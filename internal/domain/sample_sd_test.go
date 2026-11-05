package domain

import (
	"encoding/json"
	"go.mongodb.org/mongo-driver/bson"
	"math"
	"testing"
)

func TestPrimarySDMatchesExcelSample(t *testing.T) {
	r := ComputeStatistics([]float64{15, 20, 16, 20, 15}, "none")
	if math.Abs(r.StandardDev-2.588435821108957) > 1e-12 {
		t.Fatalf("primary SD still population: %v", r.StandardDev)
	}
	if r.Mean != 17.2 || r.Median != 16 || r.Minimum != 15 || r.Maximum != 20 {
		t.Fatal("unrelated statistics changed", r)
	}
}

func TestHistoricalSDPresentationIsSampleWithoutChangingSnapshot(t *testing.T) {
	old := ItemResult{PanelistCount: 5, StandardDev: math.Sqrt(26.8 / 5)}
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if math.Abs(data["standardDeviation"].(float64)-math.Sqrt(26.8/4)) > 1e-12 {
		t.Fatal("historical SD not converted", string(raw))
	}
	if old.StandardDev != math.Sqrt(26.8/5) {
		t.Fatal("source snapshot mutated")
	}
	current := ComputeStatistics([]float64{15, 20, 16, 20, 15}, "none")
	raw, _ = json.Marshal(current)
	json.Unmarshal(raw, &data)
	if math.Abs(data["standardDeviation"].(float64)-math.Sqrt(26.8/4)) > 1e-12 {
		t.Fatal("sample converted twice", string(raw))
	}
}

func TestSampleSDInsufficientAndConstantObservations(t *testing.T) {
	for _, input := range [][]float64{nil, {15}} {
		raw, _ := json.Marshal(ComputeStatistics(input, "none"))
		var data map[string]any
		json.Unmarshal(raw, &data)
		if data["standardDeviation"] != nil {
			t.Fatalf("undefined sample SD must be null: %s", raw)
		}
	}
	if r := ComputeStatistics([]float64{15, 15}, "none"); r.StandardDev != 0 || r.SampleStandardDeviation == nil || *r.SampleStandardDeviation != 0 {
		t.Fatal(r)
	}
}

func TestSampleSDBSONRoundTripAndLegacySamplePreference(t *testing.T) {
	r := ComputeStatistics([]float64{15, 20, 16, 20, 15}, "none")
	raw, err := bson.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var stored ItemResult
	if err = bson.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	shown := SampleStatistics(stored)
	if shown.StandardDev != r.StandardDev || shown.StandardDeviationMethod != "sample" {
		t.Fatal("BSON marker lost/double correction", shown)
	}
	existing := 2.5
	legacy := SampleStatistics(ItemResult{PanelistCount: 5, StandardDev: 99, SampleStandardDeviation: &existing})
	if legacy.StandardDev != existing {
		t.Fatal("stored sample ignored", legacy)
	}
}
