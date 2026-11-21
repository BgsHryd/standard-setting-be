package domain

import (
	"encoding/json"
	"math"
	"sort"
)

// cutoffBoundaryTolerancePP only removes arithmetic noise near an integer decision boundary.
const cutoffBoundaryTolerancePP = 1e-10

const NearestRawPoint = "nearest_raw_point"

func ValidRoundingPolicy(policy string) bool {
	switch policy {
	case NearestRawPoint, "none", "ceil_1pp", "nearest_1pp":
		return true
	}
	return false
}

// RoundRawPoints implements Excel ROUND on nonnegative raw points, allowing
// for floating-point noise when converting percentages back onto the rubric.
func RoundRawPoints(mean float64) float64 {
	half := math.Floor(mean) + 0.5
	if math.Abs(mean-half) <= 1e-10 {
		mean = half
	}
	return math.Round(mean)
}

// ComputeTestStatistics receives validated, complete panelist scores. Under
// nearest_raw_point the scores are weighted percentages of the same rubric.
func ComputeTestStatistics(scores []float64, policy string, items []Item) ItemResult {
	if policy != NearestRawPoint {
		return ComputeStatistics(scores, policy)
	}
	stats := ComputeStatistics(scores, "none")
	stats.RoundingPolicy = policy
	var maximum int
	for _, item := range items {
		maximum += item.MaxScore
	}
	if maximum > 0 {
		raw := stats.Mean * float64(maximum) / 100
		stats.OperationalCutoff = 100 * RoundRawPoints(raw) / float64(maximum)
	}
	return stats
}

// ComputeStatistics uses sample SD (n-1) in percentage points. SD and SE
// require at least two complete observations; population SD is retained separately.
func ComputeStatistics(input []float64, policy string) ItemResult {
	if policy != "none" && policy != "nearest_1pp" {
		policy = "ceil_1pp"
	}
	r := ItemResult{RoundingPolicy: policy, PanelistCount: len(input), StandardDeviationMethod: "sample"}
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
	population := math.Sqrt(squares / float64(len(values)))
	r.PopulationStandardDeviation = &population
	if len(values) > 1 {
		sample := math.Sqrt(squares / float64(len(values)-1))
		r.StandardDev = sample
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

// SampleStatistics normalizes a copy for presentation, never rewriting historical
// BSON snapshots. Records without the method marker stored population SD.
func SampleStatistics(r ItemResult) ItemResult {
	if r.StandardDeviationMethod == "sample" {
		return r
	}
	if r.PanelistCount > 0 {
		population := r.StandardDev
		r.PopulationStandardDeviation = &population
	}
	if r.PanelistCount > 1 {
		sample := r.StandardDev * math.Sqrt(float64(r.PanelistCount)/float64(r.PanelistCount-1))
		if r.SampleStandardDeviation != nil {
			sample = *r.SampleStandardDeviation
		}
		r.StandardDev = sample
		r.SampleStandardDeviation = &sample
	} else {
		r.StandardDev = 0
		r.SampleStandardDeviation = nil
	}
	r.StandardDeviationMethod = "sample"
	return r
}

func (r ItemResult) MarshalJSON() ([]byte, error) {
	r = SampleStatistics(r)
	type plain ItemResult
	return json.Marshal(struct {
		plain
		SD *float64 `json:"standardDeviation"`
	}{plain: plain(r), SD: r.SampleStandardDeviation})
}
