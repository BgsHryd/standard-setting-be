package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
)

type CatalogWorkbookRow struct {
	Row          int                 `json:"row"`
	Code         string              `json:"code"`
	Name         string              `json:"name"`
	ItemID       string              `json:"itemId,omitempty"`
	QuestionType domain.QuestionType `json:"questionType,omitempty"`
	MaxScore     int                 `json:"maxScore,omitempty"`
}

var ErrInvalidCatalogWorkbook = errors.New("file Excel tidak valid")

type CatalogWorkbookPreview struct {
	Valid   bool                 `json:"valid"`
	Applied bool                 `json:"applied"`
	Count   int                  `json:"count"`
	Rows    []CatalogWorkbookRow `json:"rows"`
	Errors  []WorkbookError      `json:"errors"`
}

func catalogHeaders(kind string) ([]string, error) {
	switch kind {
	case "subjects":
		return []string{"kode_mapel", "mata_pelajaran"}, nil
	case "items":
		return []string{"id_butir", "kode_mapel", "bentuk_soal", "skor_maksimum", "rubrik"}, nil
	default:
		return nil, errors.New("jenis impor harus items atau subjects")
	}
}

func (s *Service) CatalogWorkbookTemplate(ctx context.Context, kind string) ([]byte, error) {
	headers, err := catalogHeaders(kind)
	if err != nil {
		return nil, err
	}
	f := excelize.NewFile()
	defer f.Close()
	f.SetSheetName("Sheet1", "Data")
	f.SetSheetRow("Data", "A1", &headers)
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "FFFFFF", Size: 12},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"267775"}, Pattern: 1},
	})
	textStyle, _ := f.NewStyle(&excelize.Style{NumFmt: 49, Font: &excelize.Font{Size: 12}})
	last, _ := excelize.ColumnNumberToName(len(headers))
	f.SetCellStyle("Data", "A1", last+"1", headerStyle)
	f.SetCellStyle("Data", "A2", last+"5001", textStyle)
	f.SetColWidth("Data", "A", last, 30)
	f.SetRowHeight("Data", 1, 26)
	f.SetPanes("Data", &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	f.NewSheet("Panduan")
	guide := []string{
		"Isi sheet Data mulai baris 2. Jangan ubah nama sheet atau header. Maksimal 5000 baris dan 5 MB (.xlsx).",
		"Impor hanya menambahkan data baru. Duplikat dalam file/database ditolak; satu baris bermasalah menolak seluruh file.",
		"Kode mapel: 2–20 huruf/angka, boleh - atau _, diawali huruf. Kode disimpan dalam huruf besar.",
		"Jangan gunakan formula, macro, atau tautan eksternal. Format sel teks menjaga ID seperti 001 tetap utuh.",
	}
	if kind == "subjects" {
		guide = append(guide, "Mata pelajaran: nama 2–100 karakter, unik tanpa membedakan huruf besar/kecil atau spasi berulang.")
	} else {
		guide = append(guide,
			"kode_mapel harus sudah terdaftar; lihat sheet Referensi mapel. ID butir wajib dan maksimal 200 karakter.",
			"bentuk_soal: Pilihan Ganda, PG Kompleks, Essay; atau pilihan_ganda, pilihan_ganda_kompleks, essay.",
			"skor_maksimum: bilangan bulat 1–100. Pilihan Ganda wajib 1. rubrik boleh kosong, maksimal 8000 karakter; satu catatan per baris dalam sel.",
			"Butir baru berstatus Tersedia. Alokasi ke proyek dilakukan melalui konfigurasi proyek.",
		)
		catalog, err := s.store.ListSubjects(ctx)
		if err != nil {
			return nil, err
		}
		f.NewSheet("Referensi mapel")
		row := []string{"kode_mapel", "mata_pelajaran"}
		f.SetSheetRow("Referensi mapel", "A1", &row)
		f.SetCellStyle("Referensi mapel", "A1", "B1", headerStyle)
		f.SetColWidth("Referensi mapel", "A", "B", 32)
		for n, sub := range catalog {
			// Explicit strings prevent formula-like names from becoming formulas.
			f.SetCellStr("Referensi mapel", fmt.Sprintf("A%d", n+2), sub.Code)
			f.SetCellStr("Referensi mapel", fmt.Sprintf("B%d", n+2), sub.Name)
		}
	}
	for n, line := range guide {
		f.SetCellStr("Panduan", fmt.Sprintf("A%d", n+1), line)
	}
	f.SetColWidth("Panduan", "A", "A", 120)
	buffer, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// ImportCatalogWorkbook never upserts existing records. Validation and insertion
// share one transaction so any later write failure rolls back the entire file.
func (s *Service) ImportCatalogWorkbook(ctx context.Context, kind string, data []byte, apply bool) (CatalogWorkbookPreview, error) {
	result := CatalogWorkbookPreview{}
	headers, err := catalogHeaders(kind)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrInvalidCatalogWorkbook, err)
	}
	f, err := openProjectWorkbook(data)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrInvalidCatalogWorkbook, err)
	}
	defer f.Close()
	rows, err := f.GetRows("Data", excelize.Options{RawCellValue: true})
	if err != nil || len(rows) == 0 {
		return result, fmt.Errorf("%w: sheet Data dan header wajib ada", ErrInvalidCatalogWorkbook)
	}
	if len(rows) > workbookRows+1 {
		return result, fmt.Errorf("%w: maksimal 5000 baris data", ErrInvalidCatalogWorkbook)
	}
	err = s.store.WithTransaction(ctx, func(tx context.Context) error {
		result = CatalogWorkbookPreview{Rows: []CatalogWorkbookRow{}, Errors: []WorkbookError{}}
		problem := func(row int, column, value, message string) {
			result.Errors = append(result.Errors, WorkbookError{row, column, value, message})
		}
		if !reflect.DeepEqual(rows[0], headers) {
			problem(1, "header", strings.Join(rows[0], ", "), "kolom wajib tepat: "+strings.Join(headers, ", "))
		}
		catalog, err := s.store.ListSubjects(tx)
		if err != nil {
			return err
		}
		codes, names := map[string]domain.Subject{}, map[string]bool{}
		for _, sub := range catalog {
			codes[strings.ToUpper(sub.Code)] = sub
			names[domain.NormalizeSubjectName(sub.Name)] = true
		}
		seenCodes, seenNames, seenIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
		subjects, items := []domain.Subject{}, []domain.Item{}
		cell := func(row []string, col int) string {
			if col < len(row) {
				return strings.TrimSpace(row[col])
			}
			return ""
		}
		for n, row := range rows {
			for col := range row {
				address, _ := excelize.CoordinatesToCellName(col+1, n+1)
				formula, err := f.GetCellFormula("Data", address)
				if err != nil {
					return err
				}
				if formula != "" {
					problem(n+1, headers[min(col, len(headers)-1)], formula, "formula tidak diperbolehkan")
				}
			}
			if len(row) > len(headers) {
				problem(n+1, "kolom", "", "kolom tambahan tidak diperbolehkan")
			}
			if n == 0 || strings.TrimSpace(strings.Join(row, "")) == "" {
				continue
			}
			result.Count++
			if kind == "subjects" {
				code := strings.ToUpper(cell(row, 0))
				name := strings.Join(strings.Fields(cell(row, 1)), " ")
				normalized := domain.NormalizeSubjectName(name)
				if !subjectCodePattern.MatchString(code) {
					problem(n+1, "kode_mapel", code, "kode wajib 2–20 huruf/angka, boleh - atau _, diawali huruf")
				}
				if len([]rune(name)) < 2 || len([]rune(name)) > 100 {
					problem(n+1, "mata_pelajaran", name, "nama wajib 2–100 karakter")
				}
				if _, exists := codes[code]; exists || seenCodes[code] {
					problem(n+1, "kode_mapel", code, "kode mapel sudah ada di database atau ganda dalam file")
				}
				if names[normalized] || seenNames[normalized] {
					problem(n+1, "mata_pelajaran", name, "nama mata pelajaran sudah ada di database atau ganda dalam file")
				}
				seenCodes[code], seenNames[normalized] = true, true
				subjects = append(subjects, domain.Subject{Code: code, Name: name, NormalizedName: normalized})
				result.Rows = append(result.Rows, CatalogWorkbookRow{Row: n + 1, Code: code, Name: name})
				continue
			}
			id, code := cell(row, 0), strings.ToUpper(cell(row, 1))
			if id == "" || len([]rune(id)) > 200 || strings.ContainsAny(id, "\r\n\t") {
				problem(n+1, "id_butir", id, "ID wajib 1–200 karakter tanpa baris baru atau tab")
			}
			if seenIDs[id] {
				problem(n+1, "id_butir", id, "ID butir ganda dalam file")
			}
			seenIDs[id] = true
			_, findErr := s.store.GetItem(tx, id)
			if findErr == nil {
				problem(n+1, "id_butir", id, "ID butir sudah ada di database")
			} else if !errors.Is(findErr, mongo.ErrNoDocuments) {
				return findErr
			}
			sub, found := codes[code]
			if !found {
				problem(n+1, "kode_mapel", code, "kode mapel tidak ditemukan di database")
			}
			questionType := domain.QuestionType(strings.ToLower(cell(row, 2)))
			switch questionType {
			case "pilihan ganda":
				questionType = domain.QuestionTypePG
			case "pg kompleks", "pilihan ganda kompleks":
				questionType = domain.QuestionTypeComplex
			}
			if questionType != domain.QuestionTypePG && questionType != domain.QuestionTypeComplex && questionType != domain.QuestionTypeEssay {
				problem(n+1, "bentuk_soal", cell(row, 2), "pilih Pilihan Ganda, PG Kompleks, atau Essay")
			}
			score, scoreErr := strconv.Atoi(cell(row, 3))
			if scoreErr != nil || score < 1 || score > 100 || (questionType == domain.QuestionTypePG && score != 1) {
				problem(n+1, "skor_maksimum", cell(row, 3), "skor wajib bilangan bulat 1–100; Pilihan Ganda wajib 1")
			}
			rubricText := cell(row, 4)
			if len([]rune(rubricText)) > 8000 {
				problem(n+1, "rubrik", "", "rubrik maksimal 8000 karakter")
			}
			rubric := []string{}
			for _, line := range strings.Split(rubricText, "\n") {
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					rubric = append(rubric, trimmed)
				}
			}
			now := time.Now().UTC()
			items = append(items, domain.Item{ID: id, SubjectCode: sub.Code, SubjectName: sub.Name,
				QuestionType: questionType, MaxScore: score, Rubric: rubric, UsageStatus: domain.ItemAvailable,
				ProjectHistory: []domain.ItemProjectUsage{}, CreatedAt: now, UpdatedAt: now})
			result.Rows = append(result.Rows, CatalogWorkbookRow{Row: n + 1, Code: sub.Code, Name: sub.Name, ItemID: id, QuestionType: questionType, MaxScore: score})
		}
		if result.Count == 0 {
			problem(2, "baris", "", "isi minimal satu baris data")
		}
		result.Valid = len(result.Errors) == 0
		if !result.Valid || !apply {
			return nil
		}
		if kind == "subjects" {
			for i, sub := range subjects {
				if err := s.store.CreateSubject(tx, sub); err != nil {
					return fmt.Errorf("baris %d belum tersimpan: %w", result.Rows[i].Row, err)
				}
			}
		} else {
			inserter, ok := s.store.(interface {
				InsertCatalogItems(context.Context, []domain.Item) error
			})
			if !ok {
				return errors.New("penyimpanan impor butir tidak tersedia")
			}
			if err := inserter.InsertCatalogItems(tx, items); err != nil {
				if mongo.IsDuplicateKeyError(err) {
					return errors.New("ID butir baru saja ditambahkan oleh pengguna lain; periksa file kembali")
				}
				return err
			}
		}
		return nil
	})
	// Only mark applied after a successful commit, including any transaction retry.
	result.Applied = err == nil && apply && result.Valid
	return result, err
}
