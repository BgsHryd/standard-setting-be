package domain

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// PassConversion is an uploaded lookup table. Only upload metadata is returned to administrators.
type PassConversion struct {
	Filename     string                        `json:"filename" bson:"filename"`
	RowCount     int                           `json:"rowCount" bson:"rowCount"`
	SubjectCount int                           `json:"subjectCount" bson:"subjectCount"`
	UploadedAt   time.Time                     `json:"uploadedAt" bson:"uploadedAt"`
	Percentages  map[string]map[string]float64 `json:"-" bson:"percentages"`
}

// ProjectedPassPercentage looks up the nearest integer raw points. For weighted
// legacy results, the raw cutoff can be recovered from the frozen test maximum.
func (c *PassConversion) ProjectedPassPercentage(subject string, stats *ItemResult, raw *float64, maximum float64) *float64 {
	if c == nil || stats == nil || stats.PanelistCount == 0 {
		return nil
	}
	if raw == nil {
		if NormalizeScoringMethod(stats.ScoringMethod) != ScoringWeighted || maximum <= 0 {
			return nil
		}
		points := stats.Cutoff * maximum / 100
		raw = &points
	}
	if math.IsNaN(*raw) || math.IsInf(*raw, 0) || *raw < 0 || *raw > math.MaxInt32 {
		return nil
	}
	key := strconv.Itoa(int(RoundRawPoints(*raw)))
	result, found := c.Percentages[strings.ToUpper(subject)][key]
	if !found {
		return nil
	}
	return &result
}
