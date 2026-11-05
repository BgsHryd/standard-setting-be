package httpapi

import (
	"encoding/json"
	"testing"

	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
)

func TestMeasureCategoryAdminRoundTripAndPanelistVisibility(t *testing.T) {
	zero := 0.0
	item := domain.Item{ID: "one", Measure: &zero, MeasureCategory: "Sukar"}
	admin, _ := json.Marshal(adminItem(item))
	var decoded catalogItemDTO
	if err := json.Unmarshal(admin, &decoded); err != nil || decoded.MeasureCategory != "Sukar" {
		t.Fatalf("admin category missing: %s %v", admin, err)
	}
	for _, visible := range []bool{false, true} {
		form := service.PanelistForm{Subject: domain.ProjectSubject{ShowMeasure: visible}, Items: []domain.Item{item}}
		data, _ := json.Marshal(panelistFormView(form))
		var payload struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatal(err)
		}
		value, present := payload.Items[0]["measureCategory"]
		if present != visible || (present && value != "Sukar") {
			t.Fatalf("visibility=%t: %s", visible, data)
		}
	}
}
