package service

import (
	"context"
	"testing"

	"standard-setting/backend/internal/domain"
)

func TestCatalogWorkbookMetadataRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name, key string
		input     any
		want      *float64
	}{
		{"negative integer", " A ", -3, metadataFloat(-3)},
		{"negative decimal", "B", -1.25, metadataFloat(-1.25)},
		{"comma decimal", "A,C", "2,75", metadataFloat(2.75)},
		{"zero", "0", 0, metadataFloat(0)},
		{"missing", "Kunci essay", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &catalogImportMemory{subjects: []domain.Subject{{Code: "MAT", Name: "Matematika"}}, items: map[string]domain.Item{}}
			s := New(st, nil, "")
			data := catalogWorkbook(t, "items", []any{"META-001", "MAT", "essay", 4, tc.key, tc.input, "Catatan"})
			result, err := s.ImportCatalogWorkbook(context.Background(), "items", data, true)
			if err != nil || !result.Valid || !result.Applied || len(result.Rows) != 1 {
				t.Fatalf("import: %+v %v", result, err)
			}
			item := st.items["META-001"]
			if item.KJ == "" || item.KJ != result.Rows[0].KJ || len(item.Rubric) != 1 || item.Rubric[0] != "Catatan" {
				t.Fatalf("key/rubric lost: %+v", item)
			}
			for _, measure := range []*float64{item.Measure, result.Rows[0].Measure} {
				if (measure == nil) != (tc.want == nil) || (measure != nil && *measure != *tc.want) {
					t.Fatalf("Measure changed: %+v", item)
				}
			}
		})
	}
}

func metadataFloat(value float64) *float64 { return &value }

func TestCatalogWorkbookRejectsMissingKeyAndInvalidMeasureAtomically(t *testing.T) {
	for _, tc := range []struct{ key, measure, column string }{
		{"  ", "-2", "kj"}, {"A", "bukan angka", "measure"}, {"A", "NaN", "measure"},
		{"A", "Infinity", "measure"}, {"A", "1e9999", "measure"},
	} {
		t.Run(tc.column+tc.measure, func(t *testing.T) {
			st := &catalogImportMemory{subjects: []domain.Subject{{Code: "MAT", Name: "Matematika"}}, items: map[string]domain.Item{}}
			s := New(st, nil, "")
			data := catalogWorkbook(t, "items",
				[]any{"GOOD", "MAT", "essay", 4, "A", 1, ""},
				[]any{"BAD", "MAT", "essay", 4, tc.key, tc.measure, ""},
			)
			result, err := s.ImportCatalogWorkbook(context.Background(), "items", data, true)
			if err != nil || result.Valid || result.Applied || len(st.items) != 0 {
				t.Fatalf("invalid batch stored: %+v %v", result, err)
			}
			for _, issue := range result.Errors {
				if issue.Row == 3 && issue.Column == tc.column {
					return
				}
			}
			t.Fatalf("row/column missing: %+v", result.Errors)
		})
	}
}
