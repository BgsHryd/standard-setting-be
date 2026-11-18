package domain

import (
	"math"
	"testing"
)

func TestAngoffReviewCompletePopulation(t *testing.T) {
	items := []Item{{ID: "pg", QuestionType: QuestionTypePG}, {ID: "pgk", QuestionType: QuestionTypeComplex}, {ID: "essay", QuestionType: QuestionTypeEssay}}
	values := map[string]map[string]float64{"a": {"pg": 60, "pgk": 50, "essay": 100}, "b": {"pg": 80, "pgk": 100, "essay": 50}}
	got := ComputeAngoffReview(items, []string{"a", "b"}, values)
	if !got.Ready || got.CompletePanelistCount != 2 || *got.Modified.MeanSum != .7 || *got.Extended.MeanSum != 1.5 || *got.Extended.MeanPercent != 75 {
		t.Fatalf("%+v", got)
	}
	delete(values["b"], "essay")
	got = ComputeAngoffReview(items, []string{"a", "b"}, values)
	if got.Ready || got.CompletePanelistCount != 1 || got.Modified.MeanSum != nil || got.Rows[1].ModifiedSum != nil {
		t.Fatal("partial population used")
	}
	values["b"]["essay"] = math.NaN()
	if ComputeAngoffReview(items, []string{"a", "b"}, values).Ready {
		t.Fatal("NaN included")
	}
	empty := ComputeAngoffReview(items[:1], []string{"a"}, values)
	if empty.Extended.MeanSum != nil {
		t.Fatal("empty group zero")
	}
}

func TestStandardSettingErrorUnitsAndAvailability(t *testing.T) {
	zero := 0.0
	stats := ComputeStatistics([]float64{60, 80}, "none")
	got := ComputeStandardSettingError(&zero, &stats, true, false)
	if got.SEM == nil || *got.SEM != 0 || got.SEJ == nil || math.Abs(*got.SEJ-10) > 1e-10 || got.SeSS == nil || *got.SeSS != *got.SEJ {
		t.Fatalf("%+v", got)
	}
	if ComputeStandardSettingError(nil, &stats, true, false).SeSS != nil {
		t.Fatal("missing SEM treated as zero")
	}
	if ComputeStandardSettingError(&zero, &stats, false, false).SEJ != nil {
		t.Fatal("incomplete population has SEJ")
	}
	one := ComputeStatistics([]float64{60}, "none")
	if ComputeStandardSettingError(&zero, &one, true, false).SEJ != nil {
		t.Fatal("n=1 SEJ")
	}
	huge := 2000.0
	if ComputeStandardSettingError(&huge, &stats, true, false).SEM == nil {
		t.Fatal("upper bound imposed")
	}
	for _, bad := range []float64{-1, math.NaN(), math.Inf(1)} {
		if ComputeStandardSettingError(&bad, &stats, true, false).SEM != nil {
			t.Fatal("non-finite or negative SEM")
		}
	}
}
