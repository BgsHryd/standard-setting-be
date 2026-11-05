package service

import (
	"context"
	"math"
	"testing"

	"standard-setting/backend/internal/domain"
)

func TestComplexSubtypesUseSameValidationAndCalculation(t *testing.T) {
	for _, kind := range []domain.QuestionType{"pilihan_ganda_kompleks", "pilihan_ganda_kompleks_mcma", "pilihan_ganda_kompleks_bs"} {
		t.Run(string(kind), func(t *testing.T) {
			item := domain.Item{QuestionType: kind, MaxScore: 3}
			answer := domain.Answer{QuestionType: kind, Distribution: []int{10, 20, 30, 40}}
			if err := validateItemAnswer(item, answer); err != nil {
				t.Fatal(err)
			}
			if value := responseValue(answer, 3); math.Abs(value-200.0/3) > 1e-9 {
				t.Fatalf("value=%v", value)
			}
			for _, counts := range [][]int{{10, 20, 30, 39}, {50, 50}, {-1, 21, 30, 50}} {
				invalid := domain.Answer{QuestionType: kind, Distribution: counts}
				if validateItemAnswer(item, invalid) == nil {
					t.Fatalf("accepted invalid %v", counts)
				}
			}
			answer.QuestionType = "pilihan_ganda"
			if validateItemAnswer(item, answer) == nil {
				t.Fatal("accepted wrong answer type")
			}
		})
	}
}

func TestCatalogWorkbookImportsBothComplexSubtypes(t *testing.T) {
	for _, tc := range []struct {
		label string
		kind  domain.QuestionType
	}{
		{"Pilihan Ganda Kompleks MCMA", "pilihan_ganda_kompleks_mcma"},
		{"Pilihan Ganda Kompleks BS", "pilihan_ganda_kompleks_bs"},
		{"PG Kompleks MCMA", "pilihan_ganda_kompleks_mcma"},
		{"PG Kompleks BS", "pilihan_ganda_kompleks_bs"},
		{"pilihan_ganda_kompleks_mcma", "pilihan_ganda_kompleks_mcma"},
		{"pilihan_ganda_kompleks_bs", "pilihan_ganda_kompleks_bs"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			st := &catalogImportMemory{subjects: []domain.Subject{{Code: "MAT", Name: "Matematika"}}, items: map[string]domain.Item{}}
			data := catalogWorkbook(t, "items", []any{"TEST", "MAT", tc.label, "3", "A,C", "", "Rubrik skor"})
			result, err := New(st, nil, "").ImportCatalogWorkbook(context.Background(), "items", data, true)
			if err != nil || !result.Applied || st.items["TEST"].QuestionType != tc.kind || st.items["TEST"].MaxScore != 3 {
				t.Fatalf("%+v %v %+v", result, err, st.items)
			}
		})
	}
}

func TestComplexSubtypeFrozenDistributionComparison(t *testing.T) {
	for _, kind := range []domain.QuestionType{"pilihan_ganda_kompleks_mcma", "pilihan_ganda_kompleks_bs"} {
		snapshot := distributionSnapshot()
		for i := range snapshot.Submissions {
			snapshot.Submissions[i].Answer.QuestionType = kind
		}
		result := snapshotReviewItem(snapshot, "item-x", "panel-a")
		if len(result.OwnDistribution) != 4 || len(result.PanelDistribution) != 4 || result.OwnDistribution[3] != 40 {
			t.Fatalf("missing distribution for %s: %+v", kind, result)
		}
	}
}

type complexCatalogMemory struct{ catalogImportMemory }

func (st *complexCatalogMemory) UpsertItems(_ context.Context, items []domain.Item) error {
	for _, item := range items {
		st.items[item.ID] = item
	}
	return nil
}

func TestComplexSubtypeCatalogSetupKeepsSameMaxScoreAndRubric(t *testing.T) {
	for _, kind := range []domain.QuestionType{"pilihan_ganda_kompleks_mcma", "pilihan_ganda_kompleks_bs"} {
		st := &complexCatalogMemory{catalogImportMemory{subjects: []domain.Subject{{Code: "MAT", Name: "Matematika"}}, items: map[string]domain.Item{}}}
		s := New(st, nil, "")
		item := domain.Item{ID: "TEST", SubjectCode: "MAT", SubjectName: "Matematika", QuestionType: kind, MaxScore: 3, KJ: "A,C", Rubric: []string{"0", "1", "2", "3"}}
		if err := s.UpsertItems(context.Background(), []domain.Item{item}); err != nil {
			t.Fatal(kind, err)
		}
		saved := st.items["TEST"]
		if saved.QuestionType != kind || saved.MaxScore != 3 || len(saved.Rubric) != 4 {
			t.Fatalf("setup changed: %+v", saved)
		}
		item.MaxScore = 0
		if s.UpsertItems(context.Background(), []domain.Item{item}) == nil {
			t.Fatal("accepted zero maxScore")
		}
	}
}
