package domain

import (
	"math"
	"testing"
)

func TestNBKScoringMethodReconciles2024And2026(t *testing.T) {
	// Anonymized 30-item means/maxima from the reconciled five-panelist fixture.
	values := []float64{17.2, 58.32, 43, 83, 61, 66, 68, 48, 46, 55, 78.5, 63, 49, 56.28, 57.5, 70, 54.08, 79, 52.84, 57, 60.36, 53.76, 76.5, 54.4, 61.2, 55.88, 78.5, 63.04, 49, 72}
	maximums := []int{1, 5, 1, 1, 1, 1, 1, 1, 1, 1, 2, 1, 1, 5, 4, 1, 5, 1, 5, 5, 5, 5, 2, 5, 5, 5, 2, 5, 1, 1}
	for _, tc := range []struct {
		method string
		want   float64
	}{{"weighted", 58.775}, {"", 58.775}, {"average", 59.57866666666667}} {
		got, ok := TestScore(values, maximums, tc.method)
		if !ok || math.Abs(got-tc.want) > 1e-10 {
			t.Fatalf("%q got %.12f want %.12f", tc.method, got, tc.want)
		}
	}
}

func TestNBKScoringMethodRejectsInvalidScores(t *testing.T) {
	for _, tc := range []struct {
		values   []float64
		maximums []int
		method   string
	}{
		{nil, nil, "average"}, {[]float64{50}, nil, "weighted"}, {[]float64{50}, []int{0}, "average"},
		{[]float64{101}, []int{1}, "weighted"}, {[]float64{math.NaN()}, []int{1}, "average"}, {[]float64{50}, []int{1}, "unknown"},
	} {
		if _, ok := TestScore(tc.values, tc.maximums, tc.method); ok {
			t.Fatal("invalid scoring input accepted", tc)
		}
	}
}
