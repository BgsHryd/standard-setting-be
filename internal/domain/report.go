package domain

import (
	"math"
	"sort"
)

// cutoffBoundaryTolerancePP only removes arithmetic noise near an integer decision boundary.
const cutoffBoundaryTolerancePP = 1e-10

// ComputeStatistics uses panelist test scores in percentage points. Population
// SD is descriptive; sample SD and SE require at least two complete observations.
func ComputeStatistics(input []float64, policy string) ItemResult {
	if policy != "none" && policy != "nearest_1pp" {
		policy = "ceil_1pp"
	}
	r := ItemResult{RoundingPolicy: policy, PanelistCount: len(input)}
	if len(input) == 0 {
		return r
	}
	values := append([]float64(nil), input...)
	sort.Float64s(values)
	for _, v := range values {
		r.Mean += v
	}
	r.Mean /= float64(len(values))
	r.Cutoff = r.Mean
	r.Minimum = values[0]
	r.Maximum = values[len(values)-1]
	r.Median = values[len(values)/2]
	if len(values)%2 == 0 {
		r.Median = (values[len(values)/2-1] + r.Median) / 2
	}
	var squares float64
	for _, v := range values {
		squares += (v - r.Mean) * (v - r.Mean)
	}
	r.StandardDev = math.Sqrt(squares / float64(len(values)))
	if len(values) > 1 {
		sample := math.Sqrt(squares / float64(len(values)-1))
		se := sample / math.Sqrt(float64(len(values)))
		r.SampleStandardDeviation = &sample
		r.StandardError = &se
	}
	r.OperationalCutoff = r.Cutoff
	switch policy {
	case "ceil_1pp":
		decision := r.Cutoff
		nearest := math.Round(decision)
		if math.Abs(decision-nearest) <= cutoffBoundaryTolerancePP {
			decision = nearest
		}
		r.OperationalCutoff = math.Ceil(decision)
	case "nearest_1pp":
		r.OperationalCutoff = math.Round(r.Cutoff)
	}
	return r
}
