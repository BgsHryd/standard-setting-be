package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"standard-setting/backend/internal/domain"
)

var passConversionHeaders = []string{"kode_mapel", "nbk_rawskor", "persentase_peserta_lulus"}

func parsePassConversion(data []byte) (*domain.PassConversion, error) {
	if err := validateScoreWorkbookArchive(data); err != nil {
		return nil, err
	}
	book, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("workbook tidak dapat dibaca")
	}
	defer book.Close()
	sheets := book.GetSheetList()
	if len(sheets) != 1 {
		return nil, errors.New("workbook harus memiliki satu sheet tabel konversi")
	}
	sheet := sheets[0]
	rows, err := book.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil || len(rows) == 0 || !slices.Equal(rows[0], passConversionHeaders) {
		return nil, errors.New("kolom wajib: " + strings.Join(passConversionHeaders, ", "))
	}
	if len(rows) > 5001 {
		return nil, errors.New("tabel konversi maksimal 5.000 baris data")
	}
	result := &domain.PassConversion{Percentages: map[string]map[string]float64{}}
	for i, row := range rows[1:] {
		number := i + 2
		if strings.TrimSpace(strings.Join(row, "")) == "" {
			continue
		}
		if len(row) != 3 {
			return nil, fmt.Errorf("baris %d: isi ketiga kolom tabel konversi", number)
		}
		code := strings.ToUpper(strings.TrimSpace(row[0]))
		if code == "" || len(code) > 128 || strings.ContainsAny(code, ".$\x00") {
			return nil, fmt.Errorf("baris %d: kode_mapel tidak valid", number)
		}
		raw, err := strconv.ParseFloat(strings.TrimSpace(row[1]), 64)
		if err != nil || !(raw >= 0 && raw <= math.MaxInt32 && math.Trunc(raw) == raw) {
			return nil, fmt.Errorf("baris %d: nbk_rawskor harus bilangan bulat non-negatif", number)
		}
		value := strings.TrimSpace(row[2])
		percent, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSuffix(value, "%"), ",", "."), 64)
		if err != nil {
			return nil, fmt.Errorf("baris %d: persentase_peserta_lulus harus berupa angka", number)
		}
		if !strings.HasSuffix(value, "%") {
			formatted, err := book.GetCellValue(sheet, "C"+strconv.Itoa(number))
			if err != nil {
				return nil, fmt.Errorf("baris %d: format persentase tidak dapat dibaca", number)
			}
			if strings.HasSuffix(strings.TrimSpace(formatted), "%") {
				percent *= 100
			}
		}
		if !(percent >= 0 && percent <= 100) {
			return nil, fmt.Errorf("baris %d: persentase_peserta_lulus harus 0–100", number)
		}
		if result.Percentages[code] == nil {
			result.Percentages[code] = make(map[string]float64)
		}
		key := strconv.Itoa(int(raw))
		if _, duplicate := result.Percentages[code][key]; duplicate {
			return nil, fmt.Errorf("baris %d: %s dengan skor mentah %s sudah ada", number, code, key)
		}
		result.Percentages[code][key] = percent
		result.RowCount++
	}
	if result.RowCount == 0 {
		return nil, errors.New("tabel konversi belum berisi data")
	}
	result.SubjectCount = len(result.Percentages)
	return result, nil
}

func (s *Service) ProjectPassConversion(ctx context.Context, projectID string) (*domain.PassConversion, error) {
	project, err := s.store.GetProject(ctx, projectID)
	return project.PassConversion, err
}

func (s *Service) SaveProjectPassConversion(ctx context.Context, projectID, actorID, filename string, data []byte) (*domain.PassConversion, error) {
	conversion, err := parsePassConversion(data)
	if err != nil {
		return nil, err
	}
	conversion.Filename = filename
	conversion.UploadedAt = time.Now().UTC()
	err = s.store.WithTransaction(ctx, func(tx context.Context) error {
		project, err := s.store.GetProject(tx, projectID)
		if err != nil {
			return err
		}
		project.PassConversion = conversion
		project.Version++
		project.UpdatedAt = conversion.UploadedAt
		if err := s.store.SaveProject(tx, project); err != nil {
			return err
		}
		return s.recordAudit(tx, actorID, project.ID, project.ID, "pass_conversion_upload")
	})
	if err != nil {
		return nil, err
	}
	return conversion, nil
}
