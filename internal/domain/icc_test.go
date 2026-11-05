package domain

import (
	"math"
	"testing"
)

func TestICC2KAbsoluteAgreementUsesAveragePanel(t *testing.T) {
	// MS rows = 2, MS columns = 1.5, MS error = 0, n = 3.
	// Absolute-agreement average measures: 2 / (2 + 1.5/3) = .8.
	got := ICC2K([][]float64{{1, 2}, {2, 3}, {3, 4}})
	if got == nil || math.Abs(*got-0.8) > 1e-12 {
		t.Fatalf("absolute agreement should penalize rater offset: %v", got)
	}
	got = ICC2K([][]float64{{10, 10, 10}, {20, 20, 20}, {40, 40, 40}})
	if got == nil || *got != 1 {
		t.Fatalf("identical variable ratings should agree perfectly: %v", got)
	}
}

func TestICC2KDoesNotInventResultsForInsufficientOrInvalidMatrices(t *testing.T) {
	for _, matrix := range [][][]float64{
		nil, {{1, 2}}, {{1}, {2}}, {{1, 2}, {3}},
		{{1, 1}, {1, 1}}, {{0, math.NaN()}, {2, 3}},
		{{0, 1}, {math.Inf(1), 3}}, {{0, 100}, {100, 0}},
	} {
		if got := ICC2K(matrix); got != nil {
			t.Fatalf("undefined matrix has ICC: %v -> %v", matrix, *got)
		}
	}
}

func TestICC2KShroutFleissPublishedExample(t *testing.T) {
	// psych::ICC documentation: MSR=1349/120, MSC=2339/72, MSE=367/360.
	// Exact ICC(2,k) = 736/1187, not single-rater or consistency ICC.
	matrix := [][]float64{{9, 2, 5, 8}, {6, 1, 3, 2}, {8, 4, 6, 8}, {7, 1, 2, 6}, {10, 5, 6, 9}, {6, 2, 4, 7}}
	got := ICC2K(matrix)
	if got == nil || math.Abs(*got-736.0/1187.0) > 1e-12 {
		t.Fatalf("published benchmark differs: %v", got)
	}
}
