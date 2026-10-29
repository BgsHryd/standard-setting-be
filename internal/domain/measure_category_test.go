package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestMeasureCategoryIsIndependentMetadata(t *testing.T) {
	for _, category := range []string{"Sukar", "Sedang", "Mudah"} {
		for _, number := range []float64{-2, 0, 2} {
			item := Item{ID: "test", Measure: &number, MeasureCategory: category}
			encoded, err := bson.Marshal(item)
			if err != nil {
				t.Fatal(err)
			}
			var restored Item
			if err := bson.Unmarshal(encoded, &restored); err != nil || restored.MeasureCategory != category || *restored.Measure != number {
				t.Fatalf("metadata changed: %+v %v", restored, err)
			}
			shared, _ := json.Marshal(item)
			if strings.Contains(string(shared), "measureCategory") {
				t.Fatal("shared JSON exposed category")
			}
		}
	}
}

func TestMeasureCategoryAllowsOnlyThreeLabelsAndLegacyBlank(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"Sukar", "Sukar"}, {" sedang ", "Sedang"}, {"MUDAH", "Mudah"}, {"", ""}} {
		value, err := NormalizeMeasureCategory(tc.input)
		if err != nil || value != tc.want {
			t.Fatalf("%q: %q %v", tc.input, value, err)
		}
	}
	if _, err := NormalizeMeasureCategory("otomatis"); err == nil {
		t.Fatal("invalid category accepted")
	}
}
