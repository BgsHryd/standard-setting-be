package domain

import (
	"math"
	"testing"
)

func TestComputeStatisticsSampleAndRounding(t *testing.T) {
	r := ComputeStatistics([]float64{62, 64, 66, 68, 70}, "ceil_1pp")
	if r.Mean != 66 || math.Abs(r.StandardDev-math.Sqrt(10)) > 1e-10 || r.PopulationStandardDeviation == nil || math.Abs(*r.PopulationStandardDeviation-math.Sqrt(8)) > 1e-10 || r.SampleStandardDeviation == nil || math.Abs(*r.SampleStandardDeviation-math.Sqrt(10)) > 1e-10 || r.StandardError == nil || math.Abs(*r.StandardError-math.Sqrt(2)) > 1e-10 {
		t.Fatalf("wrong sample statistics: %+v", r)
	}
	for _, tc := range []struct {
		policy string
		want   float64
	}{{"none", 64.27}, {"ceil_1pp", 65}, {"nearest_1pp", 64}} {
		if got := ComputeStatistics([]float64{64.27}, tc.policy); got.OperationalCutoff != tc.want || got.StandardError != nil {
			t.Fatalf("%s: %+v", tc.policy, got)
		}
	}
	if got := ComputeStatistics(nil, "ceil_1pp"); got.PanelistCount != 0 || got.StandardError != nil {
		t.Fatalf("empty: %+v", got)
	}
	if got := ComputeStatistics([]float64{65}, "ceil_1pp"); got.OperationalCutoff != 65 {
		t.Fatal(got)
	}
}

func TestComputeStatisticsCeilingIgnoresOnlyIntegerBoundaryNoise(t *testing.T) {
	// All five values are valid maximum-score-3 distributions for 100 candidates.
	// Their exact total is 295 pp, so the panel mean is exactly 59 pp.
	values := []float64{263.0 / 3, 172.0 / 3, 212.0 / 3, 233.0 / 3, 5.0 / 3}
	result := ComputeStatistics(values, "ceil_1pp")
	if result.Cutoff <= 59 || result.Cutoff >= 59+1e-10 || result.Mean != result.Cutoff {
		t.Fatalf("raw cutoff must retain floating precision: %.17g", result.Cutoff)
	}
	if result.OperationalCutoff != 59 {
		t.Fatalf("integer-boundary float noise raised cutoff: raw %.17g, operational %g", result.Cutoff, result.OperationalCutoff)
	}
	for _, tc := range []struct{ raw, want float64 }{{59 + 5e-11, 59}, {59 + 2e-10, 60}, {59.0000001, 60}, {59.99999999995, 60}} {
		got := ComputeStatistics([]float64{tc.raw}, "ceil_1pp")
		if got.Cutoff != tc.raw || got.OperationalCutoff != tc.want {
			t.Fatalf("raw %.17g became %+v, want %g", tc.raw, got, tc.want)
		}
	}
	unrounded := ComputeStatistics(values, "none")
	if unrounded.Cutoff != result.Cutoff || unrounded.OperationalCutoff != result.Cutoff {
		t.Fatal("none policy must preserve raw cutoff", unrounded)
	}
}
