package domain

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestParseMeasureAcceptsSignedNumbersAndPreservesMissingVersusZero(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  float64
	}{
		{"0", 0}, {"-3", -3}, {"-1.25", -1.25}, {" 2,75 ", 2.75}, {"+.5", .5}, {"-2.5e-2", -.025},
	} {
		t.Run(tc.input, func(t *testing.T) {
			value, err := ParseMeasure(tc.input)
			if err != nil || value == nil || *value != tc.want {
				t.Fatalf("measure %q: %v %v", tc.input, value, err)
			}
		})
	}
	for _, input := range []string{"", "  "} {
		if value, err := ParseMeasure(input); err != nil || value != nil {
			t.Fatalf("empty measure must be absent: %v %v", value, err)
		}
	}
	for _, input := range []string{"NaN", "Infinity", "-Inf", "1e9999", "1_000", "0x10", "1.2.3", "1,000.25", "huruf"} {
		if _, err := ParseMeasure(input); err == nil {
			t.Fatalf("invalid measure accepted: %q", input)
		}
	}
}

func TestItemMetadataRequiresKeyAndFiniteMeasure(t *testing.T) {
	zero := 0.0
	if err := ValidateItemMetadata("0", &zero); err != nil {
		t.Fatal("zero key/measure should be valid", err)
	}
	if err := ValidateItemMetadata(" A,C ", nil); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", " \t", strings.Repeat("a", 8001)} {
		if err := ValidateItemMetadata(key, nil); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
	for _, measure := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := ValidateItemMetadata("A", &measure); err == nil {
			t.Fatal("non-finite measure accepted")
		}
	}
}

func TestItemMetadataPersistsInBSONButIsNotSharedWithPanelistJSON(t *testing.T) {
	measure := -1.25
	item := Item{ID: "MAT-NEW", KJ: "A,C", Measure: &measure}
	data, err := bson.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var restored Item
	if err := bson.Unmarshal(data, &restored); err != nil || restored.KJ != "A,C" || restored.Measure == nil || *restored.Measure != -1.25 {
		t.Fatalf("BSON round trip: %+v %v", restored, err)
	}
	shared, err := json.Marshal(item)
	if err != nil || strings.Contains(string(shared), `"kj"`) || strings.Contains(string(shared), `"measure"`) {
		t.Fatalf("shared JSON leaked administrative metadata: %s %v", shared, err)
	}
}
