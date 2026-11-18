package service

import (
	"context"
	"standard-setting/backend/internal/domain"
	"testing"
)

func TestExistingOptionCountCannotBeClearedOrBypassed(t *testing.T) {
	for _, kind := range []domain.QuestionType{domain.QuestionTypePG, domain.QuestionTypeComplexMCMA, domain.QuestionTypeComplexBS} {
		t.Run(string(kind), func(t *testing.T) {
			key := "A"
			if kind == domain.QuestionTypeComplexBS {
				key = "BSBS"
			}
			item := domain.Item{ID: "CHOICE", SubjectCode: "MAT", SubjectName: "Matematika", QuestionType: kind, MaxScore: 1, KJ: key, OptionCount: intValue(4), UsageStatus: domain.ItemAvailable}
			st := &complexCatalogMemory{catalogImportMemory{subjects: []domain.Subject{{Code: "MAT", Name: "Matematika"}}, items: map[string]domain.Item{"CHOICE": item}}}
			s := New(st, nil, "")
			for _, count := range []*int{nil, intValue(0), intValue(2)} {
				updated := item
				updated.OptionCount = count
				updated.KJ = "not-a-key"
				if err := s.UpsertItems(context.Background(), []domain.Item{updated}); err == nil {
					t.Fatal("invalid update accepted", count)
				}
				saved := st.items[item.ID]
				if saved.OptionCount == nil || *saved.OptionCount != 4 || saved.KJ != key {
					t.Fatal("invalid update changed stored item", saved)
				}
			}
		})
	}
}

func TestOptionCountMongoRoundTripWithoutLegacyBackfill(t *testing.T) {
	ctx, s, st, _ := fixture(t, false)
	legacy, err := st.GetItem(ctx, "MAT-001")
	if err != nil || legacy.OptionCount != nil {
		t.Fatal("legacy acquired count", legacy, err)
	}
	for _, tc := range []struct {
		id   string
		kind domain.QuestionType
		key  string
	}{
		{"OPT-PG", domain.QuestionTypePG, "D"},
		{"OPT-MCMA", domain.QuestionTypeComplexMCMA, "15"},
		{"OPT-BS", domain.QuestionTypeComplexBS, "SBSBS"},
	} {
		count := 5
		item := domain.Item{ID: tc.id, SubjectCode: "MAT", SubjectName: "Matematika", QuestionType: tc.kind, MaxScore: 1, KJ: tc.key, OptionCount: &count}
		if err := s.UpsertItems(ctx, []domain.Item{item}); err != nil {
			t.Fatal(err)
		}
		saved, err := st.GetItem(ctx, item.ID)
		if err != nil || saved.OptionCount == nil || *saved.OptionCount != 5 || saved.MaxScore != 1 {
			t.Fatal("count/score changed on persistence", saved, err)
		}
	}
}
