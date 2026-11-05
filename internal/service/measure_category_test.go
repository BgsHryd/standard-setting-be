package service

import (
	"bytes"
	"context"
	"testing"

	"github.com/xuri/excelize/v2"
	"standard-setting/backend/internal/domain"
)

func TestMeasureCategoryCatalogAndWorkbookAreIndependentOfScore(t *testing.T) {
	for _, category := range []string{"Sukar", "Sedang", "Mudah"} {
		st := &complexCatalogMemory{catalogImportMemory{subjects: []domain.Subject{{Code: "MAT", Name: "Matematika"}}, items: map[string]domain.Item{}}}
		s := New(st, nil, "")
		zero := 0.0
		item := domain.Item{ID: "MANUAL", SubjectCode: "MAT", SubjectName: "Matematika", QuestionType: domain.QuestionTypePG, MaxScore: 1, KJ: "A", Measure: &zero, MeasureCategory: category}
		if err := s.UpsertItems(context.Background(), []domain.Item{item}); err != nil || st.items["MANUAL"].MeasureCategory != category || *st.items["MANUAL"].Measure != 0 {
			t.Fatalf("manual: %+v %v", st.items, err)
		}
		file, err := excelize.OpenReader(bytes.NewReader(catalogWorkbook(t, "items", []any{"EXCEL", "MAT", "Pilihan Ganda", 1, "A", 0, ""})))
		if err != nil {
			t.Fatal(err)
		}
		file.SetCellStr("Data", "H1", "kategori_measure")
		file.SetCellStr("Data", "H2", category)
		buffer, err := file.WriteToBuffer()
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		result, err := s.ImportCatalogWorkbook(context.Background(), "items", buffer.Bytes(), true)
		if err != nil || !result.Applied || st.items["EXCEL"].MeasureCategory != category || result.Rows[0].MeasureCategory != category {
			t.Fatalf("Excel: %+v %v", result, err)
		}
		item.MeasureCategory = "sangat sukar"
		if s.UpsertItems(context.Background(), []domain.Item{item}) == nil {
			t.Fatal("invalid category accepted")
		}
	}
}
