package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
)

var projectWorkbookSheets = []string{"Proyek", "Mapel", "Butir", "Personel"}
var projectWorkbookHeaders = map[string][]string{
	"Proyek":   {"nama_proyek", "deskripsi", "tanggal_mulai", "tanggal_selesai", "pembulatan", "kumpulkan_kontak"},
	"Mapel":    {"mata_pelajaran", "definisi_kandidat"},
	"Butir":    {"mata_pelajaran", "id_butir"},
	"Personel": {"mata_pelajaran", "role", "nama", "username"},
}

type ProjectWorkbookError struct {
	Sheet string `json:"sheet"`
	WorkbookError
}
type ProjectWorkbookPreview struct {
	Valid    bool                   `json:"valid"`
	Applied  bool                   `json:"applied"`
	Count    int                    `json:"count"`
	Errors   []ProjectWorkbookError `json:"errors"`
	Project  *domain.Project        `json:"project,omitempty"`
	Accounts []AccountInput         `json:"accounts,omitempty"`
}

// ProjectWorkbookTemplate creates an unbound workbook for a new draft only.
func (s *Service) ProjectWorkbookTemplate(ctx context.Context) ([]byte, error) {
	catalog, err := s.store.ListSubjects(ctx)
	if err != nil {
		return nil, err
	}
	f := excelize.NewFile()
	defer f.Close()
	f.SetSheetName("Sheet1", "Proyek")
	headerStyle, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "FFFFFF"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"267775"}, Pattern: 1}})
	inputStyle, _ := f.NewStyle(&excelize.Style{NumFmt: 49, Font: &excelize.Font{Color: "1D4ED8"}})
	for _, sheet := range projectWorkbookSheets {
		if sheet != "Proyek" {
			f.NewSheet(sheet)
		}
		headers := projectWorkbookHeaders[sheet]
		for i, h := range headers {
			cell, _ := excelize.CoordinatesToCellName(i+1, 1)
			f.SetCellValue(sheet, cell, h)
		}
		last, _ := excelize.ColumnNumberToName(len(headers))
		f.SetCellStyle(sheet, "A1", last+"1", headerStyle)
		f.SetCellStyle(sheet, "A2", last+"1001", inputStyle)
		f.SetColWidth(sheet, "A", last, 30)
		f.SetRowHeight(sheet, 1, 26)
		f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	}
	f.NewSheet("Panduan")
	instructions := []string{
		"Isi empat sheet Proyek, Mapel, Butir, Personel. Satu file membuat satu proyek Draft baru.",
		"Proyek: tepat satu baris. Tanggal teks YYYY-MM-DD, termasuk hari selesai, zona waktu Asia/Jakarta.",
		"Pembulatan: ceil_1pp, nearest_1pp, atau none. Kumpulkan kontak: true atau false.",
		"Mapel: satu baris per nama/kode dari Referensi mapel. Definisi kandidat wajib, maksimal 8000 karakter.",
		"Butir: satu baris per ID butir tersedia di bank soal. Butir dialokasikan setelah seluruh validasi lolos.",
		"Personel: tepat satu pic_mapel dan 1–1000 panelis per mapel. Isi nama setiap personel.",
		"Username opsional, unik 3–80 karakter huruf kecil/angka/titik/garis bawah/tanda hubung. Kosong = otomatis.",
		"Password awal mengikuti konfigurasi server. Pengguna wajib mengganti password saat pertama masuk.",
		"Jangan ubah header, tambahkan kolom, atau gunakan formula/macro/tautan eksternal. Maksimal total 5000 baris dan 5 MB.",
		"Tinjau preview dahulu. Perubahan bank soal/username setelah preview diperiksa kembali saat membuat Draft.",
	}
	for i, v := range instructions {
		f.SetCellValue("Panduan", fmt.Sprintf("A%d", i+1), v)
	}
	f.SetColWidth("Panduan", "A", "A", 125)
	f.NewSheet("Referensi mapel")
	f.SetCellValue("Referensi mapel", "A1", "kode")
	f.SetCellValue("Referensi mapel", "B1", "nama")
	for i, v := range catalog {
		f.SetCellValue("Referensi mapel", fmt.Sprintf("A%d", i+2), v.Code)
		f.SetCellValue("Referensi mapel", fmt.Sprintf("B%d", i+2), v.Name)
	}
	f.SetColWidth("Referensi mapel", "A", "B", 32)
	b, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func openProjectWorkbook(data []byte) (*excelize.File, error) {
	if len(data) == 0 || len(data) > workbookLimit {
		return nil, errors.New("file Excel maksimal 5 MB")
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("file harus workbook .xlsx")
	}
	total := uint64(0)
	for _, part := range archive.File {
		name := strings.ToLower(part.Name)
		if strings.Contains(name, "vbaproject") || strings.Contains(name, "externallinks/") {
			return nil, errors.New("macro dan tautan eksternal tidak diterima")
		}
		if part.UncompressedSize64 > 25*1024*1024-total {
			return nil, errors.New("isi workbook terlalu besar")
		}
		total += part.UncompressedSize64
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: 25 * 1024 * 1024, UnzipXMLSizeLimit: 10 * 1024 * 1024})
	if err != nil {
		return nil, errors.New("workbook tidak dapat dibaca")
	}
	return f, nil
}

// ImportProjectWorkbook revalidates the entire workbook in the same transaction as draft creation.
// It never accepts an existing project identity or activates a project.
func (s *Service) ImportProjectWorkbook(ctx context.Context, data []byte, apply bool, actor string) (ProjectWorkbookPreview, error) {
	result := ProjectWorkbookPreview{Errors: []ProjectWorkbookError{}, Accounts: []AccountInput{}}
	f, err := openProjectWorkbook(data)
	if err != nil {
		return result, err
	}
	defer f.Close()
	err = s.store.WithTransaction(ctx, func(tx context.Context) error {
		// Mongo may retry a transaction; discard all results from a previous attempt.
		result = ProjectWorkbookPreview{Errors: []ProjectWorkbookError{}, Accounts: []AccountInput{}}
		problem := func(sheet string, row int, column, value, message string) {
			result.Errors = append(result.Errors, ProjectWorkbookError{sheet, WorkbookError{row, column, value, message}})
		}
		values := map[string][][]string{}
		total := 0
		for _, sheet := range projectWorkbookSheets {
			rows, readErr := f.GetRows(sheet, excelize.Options{RawCellValue: true})
			headers := projectWorkbookHeaders[sheet]
			if readErr != nil || len(rows) == 0 {
				problem(sheet, 1, "header", "", "sheet dan header wajib ada")
				continue
			}
			if len(rows) > workbookRows+1 {
				return errors.New("maksimal total 5000 baris data")
			}
			if !reflect.DeepEqual(rows[0], headers) {
				problem(sheet, 1, "header", strings.Join(rows[0], ", "), "kolom wajib tepat: "+strings.Join(headers, ", "))
			}
			for n, row := range rows {
				if len(row) > len(headers) {
					problem(sheet, n+1, "kolom", "", "kolom tambahan tidak diperbolehkan")
				}
				for col := range row {
					cell, _ := excelize.CoordinatesToCellName(col+1, n+1)
					formula, _ := f.GetCellFormula(sheet, cell)
					if formula != "" {
						problem(sheet, n+1, headers[min(col, len(headers)-1)], formula, "formula tidak diperbolehkan")
					}
				}
				if n > 0 && strings.TrimSpace(strings.Join(row, "")) != "" {
					total++
				}
			}
			values[sheet] = rows
		}
		if total > workbookRows {
			return errors.New("maksimal total 5000 baris data")
		}
		result.Count = total
		cell := func(row []string, col int) string {
			if col < len(row) {
				return strings.TrimSpace(row[col])
			}
			return ""
		}
		each := func(sheet string, fn func(int, []string)) {
			for n, row := range values[sheet] {
				if n > 0 && strings.TrimSpace(strings.Join(row, "")) != "" {
					fn(n+1, row)
				}
			}
		}
		p := domain.Project{Status: domain.ProjectDraft, WizardStep: 4, FeatureVersion: 1, WorkflowVersion: 1, PlannedRounds: 3, Subjects: []domain.ProjectSubject{}}
		metadataRows := 0
		each("Proyek", func(n int, row []string) {
			metadataRows++
			if metadataRows > 1 {
				problem("Proyek", n, "baris", "", "tepat satu baris proyek diperbolehkan")
				return
			}
			p.Name = cell(row, 0)
			p.Description = cell(row, 1)
			p.RoundingPolicy = cell(row, 4)
			if p.Name == "" || len([]rune(p.Name)) > 200 {
				problem("Proyek", n, "nama_proyek", p.Name, "nama proyek wajib 1–200 karakter")
			}
			if len([]rune(p.Description)) > 8000 {
				problem("Proyek", n, "deskripsi", "", "deskripsi maksimal 8000 karakter")
			}
			location := time.FixedZone("Asia/Jakarta", 7*60*60)
			for col := 2; col <= 3; col++ {
				v := cell(row, col)
				date, dateErr := time.ParseInLocation("2006-01-02", v, location)
				if dateErr != nil {
					problem("Proyek", n, projectWorkbookHeaders["Proyek"][col], v, "gunakan tanggal teks YYYY-MM-DD")
				} else if col == 2 {
					p.StartDate = date.UTC()
				} else {
					p.EndDate = date.AddDate(0, 0, 1).UTC()
				}
			}
			if !p.StartDate.IsZero() && !p.EndDate.IsZero() && !p.EndDate.After(p.StartDate) {
				problem("Proyek", n, "tanggal_selesai", cell(row, 3), "periode selesai harus pada atau sesudah periode mulai")
			}
			if p.RoundingPolicy != "none" && p.RoundingPolicy != "nearest_1pp" && p.RoundingPolicy != "ceil_1pp" {
				problem("Proyek", n, "pembulatan", p.RoundingPolicy, "pilih none, nearest_1pp, atau ceil_1pp")
			}
			switch cell(row, 5) {
			case "true":
				p.CollectContactPanelis = true
			case "false":
			default:
				problem("Proyek", n, "kumpulkan_kontak", cell(row, 5), "isi true atau false")
			}
		})
		if metadataRows == 0 {
			problem("Proyek", 2, "baris", "", "satu baris proyek wajib diisi")
		}
		catalog, err := s.store.ListSubjects(tx)
		if err != nil {
			return err
		}
		lookup := func(value string) (domain.Subject, bool) {
			var found domain.Subject
			matches := 0
			for _, sub := range catalog {
				if strings.EqualFold(sub.Code, value) || domain.NormalizeSubjectName(sub.Name) == domain.NormalizeSubjectName(value) {
					found = sub
					matches++
				}
			}
			return found, matches == 1
		}
		indexes := map[string]int{}
		sourceRows := map[string]int{}
		each("Mapel", func(n int, row []string) {
			v := cell(row, 0)
			sub, ok := lookup(v)
			if !ok {
				problem("Mapel", n, "mata_pelajaran", v, "nama/kode mapel tidak ditemukan atau ambigu")
				return
			}
			if _, exists := indexes[sub.Code]; exists {
				problem("Mapel", n, "mata_pelajaran", v, "mata pelajaran ganda")
				return
			}
			definition := domain.NormalizeCandidateDefinition(cell(row, 1))
			if definition == "" || len([]rune(definition)) > 8000 {
				problem("Mapel", n, "definisi_kandidat", "", "definisi kandidat wajib 1–8000 karakter")
			}
			indexes[sub.Code] = len(p.Subjects)
			sourceRows[sub.Code] = n
			p.Subjects = append(p.Subjects, domain.ProjectSubject{Code: sub.Code, Name: sub.Name, CandidateDefinition: definition, ItemIDs: []string{}, PanelistNames: []string{}})
		})
		if len(p.Subjects) == 0 {
			problem("Mapel", 2, "baris", "", "minimal satu mata pelajaran wajib diisi")
		}
		selectedSubject := func(sheet string, n int, value string) (int, bool) {
			sub, ok := lookup(value)
			idx, selected := indexes[sub.Code]
			if !ok || !selected {
				problem(sheet, n, "mata_pelajaran", value, "mata pelajaran harus terdaftar pada sheet Mapel")
				return 0, false
			}
			return idx, true
		}
		seenItems := map[string]bool{}
		each("Butir", func(n int, row []string) {
			idx, ok := selectedSubject("Butir", n, cell(row, 0))
			id := cell(row, 1)
			if seenItems[id] {
				problem("Butir", n, "id_butir", id, "ID butir ganda dalam file")
			}
			seenItems[id] = true
			item, findErr := s.store.GetItem(tx, id)
			if errors.Is(findErr, mongo.ErrNoDocuments) || id == "" {
				problem("Butir", n, "id_butir", id, "ID butir tidak ada di bank soal")
			} else if findErr != nil {
				err = findErr
				return
			} else {
				if ok && item.SubjectCode != p.Subjects[idx].Code {
					problem("Butir", n, "id_butir", id, "butir bukan milik mata pelajaran ini")
				}
				if (item.UsageStatus != domain.ItemAvailable && item.UsageStatus != domain.ItemAllocated) || item.ReadOnly {
					problem("Butir", n, "id_butir", id, "butir sudah diuji atau terkunci")
				}
			}
			if ok && findErr == nil {
				p.Subjects[idx].ItemIDs = append(p.Subjects[idx].ItemIDs, id)
			}
		})
		if err != nil {
			return err
		}
		pics := map[string]int{}
		seenUsernames := map[string]bool{}
		each("Personel", func(n int, row []string) {
			idx, ok := selectedSubject("Personel", n, cell(row, 0))
			role, name, username := domain.Role(cell(row, 1)), cell(row, 2), strings.ToLower(cell(row, 3))
			if role != domain.RolePICMapel && role != domain.RolePanelist {
				problem("Personel", n, "role", string(role), "role harus pic_mapel atau panelis")
			}
			if name == "" || len([]rune(name)) > 200 {
				problem("Personel", n, "nama", name, "nama wajib 1–200 karakter")
			}
			if username != "" {
				if !usernamePattern.MatchString(username) {
					problem("Personel", n, "username", username, "username wajib 3–80 karakter: huruf/angka/titik/garis bawah/tanda hubung")
				}
				if seenUsernames[username] {
					problem("Personel", n, "username", username, "username ganda dalam file")
				}
				seenUsernames[username] = true
				_, findErr := s.store.FindByUsername(tx, username)
				if findErr == nil {
					problem("Personel", n, "username", username, "username sudah dipakai di database")
				} else if !errors.Is(findErr, mongo.ErrNoDocuments) {
					err = findErr
				}
			}
			if !ok {
				return
			}
			sub := &p.Subjects[idx]
			if role == domain.RolePICMapel {
				pics[sub.Code]++
				sub.PICName = name
				if pics[sub.Code] > 1 {
					problem("Personel", n, "role", string(role), "tepat satu PIC per mata pelajaran")
				}
			} else if role == domain.RolePanelist {
				sub.PanelistNames = append(sub.PanelistNames, name)
				sub.PanelistCount++
			}
			result.Accounts = append(result.Accounts, AccountInput{Name: name, Role: role, SubjectCode: sub.Code, Username: username})
		})
		if err != nil {
			return err
		}
		for _, sub := range p.Subjects {
			n := sourceRows[sub.Code]
			if len(sub.ItemIDs) == 0 {
				problem("Mapel", n, "mata_pelajaran", sub.Code, "minimal satu ID butir wajib dipilih")
			}
			if pics[sub.Code] != 1 {
				problem("Mapel", n, "mata_pelajaran", sub.Code, "tepat satu PIC wajib diisi di sheet Personel")
			}
			if sub.PanelistCount < 1 || sub.PanelistCount > 1000 {
				problem("Mapel", n, "mata_pelajaran", sub.Code, "jumlah panelis harus 1–1000")
			}
		}
		result.Project = &p
		result.Valid = len(result.Errors) == 0
		if !result.Valid || !apply {
			return nil
		}
		saved, err := s.SaveProject(tx, p, actor)
		if err != nil {
			return err
		}
		if _, err = s.GenerateAccounts(tx, saved.ID, result.Accounts); err != nil {
			return err
		}
		saved, err = s.GetProject(tx, saved.ID)
		if err != nil {
			return err
		}
		result.Project = &saved
		result.Applied = true
		return nil
	})
	if err != nil {
		result.Applied = false
		result.Project = nil
	}
	return result, err
}
