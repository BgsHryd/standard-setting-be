package httpapi

import (
	"encoding/json"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
	"testing"
)

func TestExampleKeyIndependentOfKJVisibilityAndScopedToEnabledTypes(t *testing.T) {
	for _, show := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			form := service.PanelistForm{
				Subject:      domain.ProjectSubject{ShowKJ: show},
				ShowExamples: map[domain.QuestionType]bool{domain.QuestionTypeComplexMCMA: enabled, domain.QuestionTypeComplexBS: enabled, domain.QuestionTypePG: true},
				Items: []domain.Item{
					{ID: "mcma", QuestionType: domain.QuestionTypeComplexMCMA, KJ: "A,C,E"},
					{ID: "bs", QuestionType: domain.QuestionTypeComplexBS, KJ: "SBSBS"},
					{ID: "pg", QuestionType: domain.QuestionTypePG, KJ: "B"},
					{ID: "essay", QuestionType: domain.QuestionTypeEssay, KJ: "rubrik"},
				},
			}
			raw, err := json.Marshal(panelistFormView(form))
			if err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Items []map[string]any `json:"items"`
			}
			if err = json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			for i, item := range payload.Items {
				key, hasSimulationKey := item["simulationKey"]
				if hasSimulationKey != (enabled && i < 2) {
					t.Fatalf("wrong simulation key availability showKJ=%t enabled=%t item=%d payload=%s", show, enabled, i, raw)
				}
				if hasSimulationKey && key != form.Items[i].KJ {
					t.Fatal("simulation did not receive real item key", item)
				}
				if _, visible := item["kj"]; visible != show {
					t.Fatal("KJ badge visibility changed", item)
				}
			}
		}
	}
}
