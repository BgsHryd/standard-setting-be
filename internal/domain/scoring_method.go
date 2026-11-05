package domain

import "math"

const (
	ScoringWeighted = "weighted"
	ScoringAverage  = "average"
)

func NormalizeScoringMethod(method string) string {
	if method == ScoringAverage {
		return ScoringAverage
	}
	return ScoringWeighted
}

// TestScore aggregates normalized item percentages. Average gives each item one
// vote; Weighted weights each item by its original maximum score.
func TestScore(values []float64, maximums []int, method string) (float64, bool) {
	if len(values) == 0 || len(values) != len(maximums) || method != "" && method != ScoringWeighted && method != ScoringAverage {
		return 0, false
	}
	numerator, denominator := 0.0, 0.0
	for i, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 || maximums[i] < 1 {
			return 0, false
		}
		weight := float64(maximums[i])
		if method == ScoringAverage {
			weight = 1
		}
		numerator += value * weight
		denominator += weight
	}
	return numerator / denominator, true
}
