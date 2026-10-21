package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
)

const workbookLimit = 5 * 1024 * 1024
const workbookRows = 5000

var workbookHeaders = map[int][]string{2: {"mata_pelajaran", "id_butir"}, 3: {"mata_pelajaran", "role", "nama"}, 4: {"mata_pelajaran", "role", "nama", "username"}}

type WorkbookError struct {
	Row     int    `json:"row"`
	Column  string `json:"column"`
	Value   string `json:"value"`
	Message string `json:"message"`
}
type ImportPreview struct {
	Valid    bool            `json:"valid"`
	Count    int             `json:"count"`
	Errors   []WorkbookError `json:"errors"`
	Project  *domain.Project `json:"project,omitempty"`
	Accounts []AccountInput  `json:"accounts,omitempty"`
	Applied  bool            `json:"applied"`
}

func (s *Service) Template(ctx context.Context, id string, step int) ([]byte, error) {
	headers, ok := workbookHeaders[step]
	if !ok {
		return nil, errors.New("template hanya untuk langkah 2–4")
	}
	p, err := s.GetProject(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Status == domain.ProjectCompleted {
		return nil, errors.New("proyek selesai tidak dapat diimpor")
	}
	rows, err := s.AccountRows(ctx, id)
	if err != nil {
		return nil, err
	}
	if step >= 3 {
		if len(p.Subjects) == 0 {
			return nil, errors.New("isi kebutuhan setiap mata pelajaran terlebih dahulu")
		}
		for _, sub := range p.Subjects {
			if sub.PanelistCount < 1 {
				return nil, errors.New("kebutuhan panelis setiap mapel harus terisi")
			}
		}
	}
	f := excelize.NewFile()
	defer f.Close()
	f.SetSheetName("Sheet1", "Data")
	f.NewSheet("_binding")
	f.SetCellValue("_binding", "A1", p.ID)
	f.SetCellValue("_binding", "B1", strconv.Itoa(p.Version))
	f.SetCellValue("_binding", "C1", strconv.Itoa(step))
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue("Data", cell, h)
	}
	names := map[string]string{}
	for _, sub := range p.Subjects {
		names[sub.Code] = sub.Name
	}
	n := 2
	if step == 2 {
		for _, sub := range p.Subjects {
			for _, item := range sub.ItemIDs {
				f.SetCellValue("Data", fmt.Sprintf("A%d", n), sub.Name)
				f.SetCellValue("Data", fmt.Sprintf("B%d", n), item)
				n++
			}
		}
	}
	if step >= 3 {
		for _, row := range rows {
			if row.Status == "removed" {
				continue
			}
			f.SetCellValue("Data", fmt.Sprintf("A%d", n), names[row.SubjectCode])
			f.SetCellValue("Data", fmt.Sprintf("B%d", n), string(row.Role))
			f.SetCellValue("Data", fmt.Sprintf("C%d", n), row.Name)
			if step == 4 {
				f.SetCellValue("Data", fmt.Sprintf("D%d", n), row.Username)
			}
			f.SetCellValue("_binding", fmt.Sprintf("A%d", n), row.SlotID)
			n++
		}
	}
	headerStyle, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "FFFFFF", Size: 12}, Fill: excelize.Fill{Type: "pattern", Color: []string{"267775"}, Pattern: 1}, Alignment: &excelize.Alignment{Vertical: "center"}})
	inputStyle, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Size: 12, Color: "1D4ED8"}, NumFmt: 49, Alignment: &excelize.Alignment{Vertical: "center"}, Protection: &excelize.Protection{Locked: false}})
	last, _ := excelize.ColumnNumberToName(len(headers))
	f.SetColWidth("Data", "A", last, 30)
	f.SetRowHeight("Data", 1, 26)
	f.SetCellStyle("Data", "A1", last+"1", headerStyle)
	f.SetCellStyle("Data", "A2", fmt.Sprintf("%s%d", last, max(n, 100)), inputStyle)
	f.SetPanes("Data", &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	f.SetSheetVisible("_binding", false, true)
	b, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func (s *Service) ImportWorkbook(ctx context.Context, id string, step int, data []byte, apply bool) (ImportPreview, error) {
	result := ImportPreview{Errors: []WorkbookError{}}
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		p, err := s.GetProject(tx, id)
		if err != nil {
			return err
		}
		if p.Status == domain.ProjectCompleted {
			return errors.New("proyek selesai tidak dapat diimpor")
		}
		headers, ok := workbookHeaders[step]
		if !ok {
			return errors.New("langkah impor tidak valid")
		}
		if len(data) == 0 || len(data) > workbookLimit {
			return errors.New("file Excel maksimal 5 MB")
		}
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return errors.New("file harus workbook .xlsx")
		}
		total := uint64(0)
		for _, file := range archive.File {
			total += file.UncompressedSize64
			if strings.Contains(file.Name, "vbaProject") || strings.Contains(file.Name, "externalLinks/") {
				return errors.New("macro dan tautan eksternal tidak diterima")
			}
		}
		if total > 25*1024*1024 {
			return errors.New("isi workbook terlalu besar")
		}
		f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: 25 * 1024 * 1024, UnzipXMLSizeLimit: 10 * 1024 * 1024})
		if err != nil {
			return errors.New("workbook tidak dapat dibaca")
		}
		defer f.Close()
		bind, _ := f.GetCellValue("_binding", "A1")
		version, _ := f.GetCellValue("_binding", "B1")
		kind, _ := f.GetCellValue("_binding", "C1")
		if bind != p.ID || version != strconv.Itoa(p.Version) || kind != strconv.Itoa(step) {
			return errors.New("template bukan untuk konfigurasi ini atau sudah kedaluwarsa; unduh template terbaru")
		}
		rows, err := f.GetRows("Data", excelize.Options{RawCellValue: true})
		if err != nil || len(rows) == 0 {
			return errors.New("sheet Data dan header wajib ada")
		}
		if len(rows) > workbookRows+1 {
			return errors.New("maksimal 5000 baris data")
		}
		if !reflect.DeepEqual(rows[0], headers) {
			return errors.New("kolom wajib tepat: " + strings.Join(headers, ", "))
		}
		problem := func(row int, col, value, msg string) {
			result.Errors = append(result.Errors, WorkbookError{row, col, value, msg})
		}
		for n, row := range rows {
			if len(row) > len(headers) {
				problem(n+1, "header", "", "kolom tambahan tidak diperbolehkan")
			}
			for col := range row {
				cell, _ := excelize.CoordinatesToCellName(col+1, n+1)
				formula, _ := f.GetCellFormula("Data", cell)
				if formula != "" {
					problem(n+1, headers[min(col, len(headers)-1)], formula, "formula tidak diperbolehkan")
				}
			}
		}
		value := func(row []string, n int) string {
			if n < len(row) {
				return strings.TrimSpace(row[n])
			}
			return ""
		}
		catalog, err := s.store.ListSubjects(tx)
		if err != nil {
			return err
		}
		byName := map[string]domain.Subject{}
		byCode := map[string]domain.Subject{}
		for _, sub := range catalog {
			byName[domain.NormalizeSubjectName(sub.Name)] = sub
			byCode[strings.ToUpper(strings.TrimSpace(sub.Code))] = sub
		}
		if step == 2 {
			if !p.ItemsEditable {
				problem(0, "id_butir", "", "butir terkunci karena penilaian sudah dimulai")
			}
			seen := map[string]bool{}
			for n, row := range rows[1:] {
				rowNo := n + 2
				mapel, itemID := value(row, 0), value(row, 1)
				if mapel == "" && itemID == "" {
					continue
				}
				result.Count++
				sub, exists := byName[domain.NormalizeSubjectName(mapel)]
				coded, codeExists := byCode[strings.ToUpper(strings.TrimSpace(mapel))]
				if exists && codeExists && sub.Code != coded.Code {
					exists = false
					problem(rowNo, "mata_pelajaran", mapel, "nama dan kode mata pelajaran ambigu; gunakan pengenal yang tidak ambigu")
				} else if codeExists {
					sub = coded
					exists = true
				} else if !exists {
					problem(rowNo, "mata_pelajaran", mapel, "nama atau kode mata pelajaran tidak ada di database")
				}
				item, findErr := s.store.GetItem(tx, itemID)
				if errors.Is(findErr, mongo.ErrNoDocuments) || itemID == "" {
					problem(rowNo, "id_butir", itemID, "ID butir tidak ada di database")
				} else if findErr != nil {
					return findErr
				}
				if seen[itemID] {
					problem(rowNo, "id_butir", itemID, "ID butir ganda dalam file")
				}
				seen[itemID] = true
				if exists && findErr == nil && item.SubjectCode != sub.Code {
					problem(rowNo, "id_butir", itemID, "butir bukan milik mata pelajaran ini")
				}
				if findErr == nil && (item.UsageStatus == domain.ItemTested || item.ProjectID != "" && item.ProjectID != p.ID) {
					problem(rowNo, "id_butir", itemID, "butir sudah diuji atau dialokasikan ke proyek lain")
				}
				if !exists || findErr != nil {
					continue
				}
				idx := -1
				for i := range p.Subjects {
					if p.Subjects[i].Code == sub.Code {
						idx = i
					}
				}
				if idx < 0 {
					p.Subjects = append(p.Subjects, domain.ProjectSubject{Code: sub.Code, Name: sub.Name, PanelistCount: 3, PanelistNames: []string{"", "", ""}, ItemIDs: []string{}})
					idx = len(p.Subjects) - 1
				}
				if !contains(p.Subjects[idx].ItemIDs, itemID) {
					p.Subjects[idx].ItemIDs = append(p.Subjects[idx].ItemIDs, itemID)
				}
			}
		} else {
			expected, err := s.AccountRows(tx, id)
			if err != nil {
				return err
			}
			active := []AccountRow{}
			for _, row := range expected {
				if row.Status != "removed" {
					active = append(active, row)
				}
			}
			if len(p.Subjects) == 0 || len(active) == 0 {
				return errors.New("kebutuhan setiap mata pelajaran harus diisi terlebih dahulu")
			}
			for _, sub := range p.Subjects {
				if sub.PanelistCount < 1 {
					return errors.New("jumlah panelis harus terisi")
				}
			}
			if len(rows)-1 != len(active) {
				problem(0, "baris", "", "jumlah baris berbeda dari kebutuhan personel; baris tidak boleh ditambah atau dihapus")
			}
			seen := map[string]bool{}
			for i, expected := range active {
				n := i + 2
				row := []string{}
				if i+1 < len(rows) {
					row = rows[i+1]
				}
				result.Count++
				mapel, role, name := value(row, 0), value(row, 1), value(row, 2)
				slotID, _ := f.GetCellValue("_binding", fmt.Sprintf("A%d", n))
				var subject domain.ProjectSubject
				for _, sub := range p.Subjects {
					if sub.Code == expected.SubjectCode {
						subject = sub
					}
				}
				if slotID != expected.SlotID || mapel != subject.Name || role != string(expected.Role) {
					problem(n, "mata_pelajaran / role", mapel+" / "+role, "baris prefill atau identitas personel diubah")
				}
				if name == "" {
					problem(n, "nama", name, "nama wajib diisi")
				}
				if step == 3 {
					for si := range p.Subjects {
						for j := range p.Subjects[si].Slots {
							slot := &p.Subjects[si].Slots[j]
							if slot.ID == expected.SlotID {
								slot.Name = name
								if slot.Role == domain.RolePICMapel {
									p.Subjects[si].PICName = name
								} else {
									p.Subjects[si].PanelistNames[j-1] = name
								}
							}
						}
					}
				}
				if step == 4 {
					username := strings.ToLower(value(row, 3))
					if name != expected.Name {
						problem(n, "nama", name, "nama prefill tidak boleh diubah pada langkah akun")
					}
					if !usernamePattern.MatchString(username) {
						problem(n, "username", username, "username wajib 3–80 karakter: huruf/angka/titik/garis bawah/tanda hubung")
					}
					if expected.Username != "" && username != expected.Username {
						problem(n, "username", username, "username akun existing tidak dapat diubah")
					}
					if seen[username] {
						problem(n, "username", username, "username ganda dalam file")
					}
					seen[username] = true
					found, findErr := s.store.FindByUsername(tx, username)
					if findErr == nil && found.ID != expected.UserID {
						problem(n, "username", username, "username sudah dipakai di database")
					} else if findErr != nil && !errors.Is(findErr, mongo.ErrNoDocuments) {
						return findErr
					}
					result.Accounts = append(result.Accounts, AccountInput{SlotID: expected.SlotID, Name: name, Role: expected.Role, SubjectCode: expected.SubjectCode, Username: username})
				}
			}
		}
		if result.Count == 0 {
			problem(0, "baris", "", "file tidak memiliki data")
		}
		result.Valid = len(result.Errors) == 0
		result.Project = &p
		if !result.Valid || !apply {
			return nil
		}
		if step == 4 {
			if _, err := s.GenerateAccounts(tx, id, result.Accounts); err != nil {
				return err
			}
			saved, err := s.GetProject(tx, id)
			if err != nil {
				return err
			}
			result.Project = &saved
		} else {
			saved, err := s.SaveProject(tx, p, "workbook")
			if err != nil {
				return err
			}
			result.Project = &saved
		}
		result.Applied = true
		return nil
	})
	return result, err
}
