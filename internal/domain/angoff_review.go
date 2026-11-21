package domain

import "math"

type AngoffGroup struct {
	MaxPoints   int      `json:"maxPoints"`
	MeanRaw     *float64 `json:"meanRaw"`
	ItemCount   int      `json:"itemCount"`
	MeanSum     *float64 `json:"meanSum"`
	MeanPercent *float64 `json:"meanPercent"`
}
type MixAngoff struct {
	ModifiedMean  *float64 `json:"modifiedMean"`
	ExtendedMean  *float64 `json:"extendedMean"`
	CutoffPoints  *float64 `json:"cutoffPoints"`
	CutoffPercent *float64 `json:"cutoffPercent"`
	SDPoints      *float64 `json:"sdPoints"`
	SEJPoints     *float64 `json:"sejPoints"`
}

type AngoffReviewRow struct {
	ModifiedMean *float64 `json:"modifiedMean"`
	ExtendedMean *float64 `json:"extendedMean"`
	RawPoints    *float64 `json:"rawPoints"`
	ModifiedRaw  *float64 `json:"modifiedRaw"`
	ExtendedRaw  *float64 `json:"extendedRaw"`
	PanelistID   string   `json:"panelistId"`
	ModifiedSum  *float64 `json:"modifiedSum"`
	ExtendedSum  *float64 `json:"extendedSum"`
}
type AngoffReview struct {
	Mix                   MixAngoff         `json:"mix"`
	MaxPoints             int               `json:"maxPoints"`
	MeanRaw               *float64          `json:"meanRaw"`
	WeightedPercent       *float64          `json:"weightedPercent"`
	AveragePercent        *float64          `json:"averagePercent"`
	WeightedSEJ           *float64          `json:"weightedSEJ"`
	AverageSEJ            *float64          `json:"averageSEJ"`
	FormulaVersion        string            `json:"formulaVersion"`
	ActivePanelistCount   int               `json:"activePanelistCount"`
	CompletePanelistCount int               `json:"completePanelistCount"`
	Ready                 bool              `json:"ready"`
	Modified              AngoffGroup       `json:"modified"`
	Extended              AngoffGroup       `json:"extended"`
	Rows                  []AngoffReviewRow `json:"rows"`
}

// ComputeAngoffReview accepts only eligible, reviewed item percentages. Whole-test
// completeness defines the population; group means are withheld until all are complete.
func ComputeAngoffReview(items []Item, panelistIDs []string, percentages map[string]map[string]float64) AngoffReview {
	out := AngoffReview{FormulaVersion: "angoff-mix-2026", ActivePanelistCount: len(panelistIDs), Rows: []AngoffReviewRow{}}
	for _, item := range items {
		// A missing rubric cannot establish a denominator for this test.
		if item.MaxScore <= 0 || item.QuestionType == "" {
			return out
		}
	}
	for _, item := range items {
		group := &out.Extended
		if item.QuestionType == QuestionTypePG {
			group = &out.Modified
		}
		group.ItemCount++
		group.MaxPoints += item.MaxScore
		out.MaxPoints += item.MaxScore
	}
	type totals struct{ proportion, points float64 }
	var pgTotal, extendedTotal totals
	var rawScores, weightedScores, averageScores []float64
	for _, id := range panelistIDs {
		row := AngoffReviewRow{PanelistID: id}
		var pg, extended totals
		complete := len(items) > 0
		for _, item := range items {
			value, ok := percentages[id][item.ID]
			if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
				complete = false
				break
			}
			group := &extended
			if item.QuestionType == QuestionTypePG {
				group = &pg
			}
			group.proportion += value / 100
			group.points += value * float64(item.MaxScore) / 100
		}
		if complete {
			out.CompletePanelistCount++
			if out.Modified.ItemCount > 0 {
				mean := pg.proportion / float64(out.Modified.ItemCount)
				row.ModifiedMean = &mean
				row.ModifiedSum, row.ModifiedRaw = &pg.proportion, &pg.points
			}
			if out.Extended.ItemCount > 0 {
				mean := extended.points / float64(out.Extended.ItemCount)
				row.ExtendedMean = &mean
				row.ExtendedSum, row.ExtendedRaw = &extended.proportion, &extended.points
			}
			pgTotal.proportion += pg.proportion
			pgTotal.points += pg.points
			extendedTotal.proportion += extended.proportion
			extendedTotal.points += extended.points
			raw := pg.points + extended.points
			row.RawPoints = &raw
			rawScores = append(rawScores, raw)
			weightedScores = append(weightedScores, 100*raw/float64(out.MaxPoints))
			averageScores = append(averageScores, 100*(pg.proportion+extended.proportion)/float64(len(items)))
		}
		out.Rows = append(out.Rows, row)
	}
	out.Ready = out.ActivePanelistCount > 0 && out.CompletePanelistCount == out.ActivePanelistCount
	if out.Ready {
		n := float64(out.CompletePanelistCount)
		for _, entry := range []struct {
			group *AngoffGroup
			total totals
		}{{&out.Modified, pgTotal}, {&out.Extended, extendedTotal}} {
			if entry.group.ItemCount > 0 {
				mean, raw := entry.total.proportion/n, entry.total.points/n
				percent := 100 * mean / float64(entry.group.ItemCount)
				entry.group.MeanSum, entry.group.MeanPercent, entry.group.MeanRaw = &mean, &percent, &raw
			}
		}
		weighted := ComputeStatistics(weightedScores, "none")
		average := ComputeStatistics(averageScores, "none")
		raw := (pgTotal.points + extendedTotal.points) / n
		out.MeanRaw = &raw
		rawStats := ComputeStatistics(rawScores, "none")
		cut := RoundRawPoints(raw)
		pct := 100 * cut / float64(out.MaxPoints)
		out.Mix = MixAngoff{CutoffPoints: &cut, CutoffPercent: &pct, SDPoints: rawStats.SampleStandardDeviation, SEJPoints: rawStats.StandardError}
		if out.Modified.ItemCount > 0 {
			mean := *out.Modified.MeanSum / float64(out.Modified.ItemCount)
			out.Mix.ModifiedMean = &mean
		}
		if out.Extended.ItemCount > 0 {
			mean := *out.Extended.MeanRaw / float64(out.Extended.ItemCount)
			out.Mix.ExtendedMean = &mean
		}
		out.WeightedPercent, out.WeightedSEJ = &weighted.Mean, weighted.StandardError
		out.AveragePercent, out.AverageSEJ = &average.Mean, average.StandardError
	}
	return out
}

type StandardSettingError struct {
	SEM        *float64 `json:"sem"`
	SEJ        *float64 `json:"sej"`
	SeSS       *float64 `json:"seSs"`
	Unit       string   `json:"unit"`
	Assumption string   `json:"assumption"`
}

func ComputeStandardSettingError(sem *float64, stats *ItemResult, ready bool, historical bool) StandardSettingError {
	out := StandardSettingError{Unit: "percentage_points", Assumption: "Galat jarak skor peserta ke ambang menggunakan √(SEM² + SEJ²) bila SEM mewakili CSEM di sekitar ambang pada skala yang sama dan kovarians galat dapat diabaikan."}
	if historical {
		out.Assumption += " SEM historis tidak direkam."
	}
	if sem != nil && !math.IsNaN(*sem) && !math.IsInf(*sem, 0) && *sem >= 0 {
		v := *sem
		out.SEM = &v
	}
	if ready && stats != nil && stats.PanelistCount >= 2 && stats.StandardError != nil && !math.IsNaN(*stats.StandardError) && !math.IsInf(*stats.StandardError, 0) && *stats.StandardError >= 0 {
		v := *stats.StandardError
		out.SEJ = &v
	}
	if out.SEM != nil && out.SEJ != nil {
		v := math.Hypot(*out.SEM, *out.SEJ)
		if !math.IsInf(v, 0) {
			out.SeSS = &v
		}
	}
	return out
}

// RoundStandardSettingError uses whole-test judgments on the operational scale.
// A missing measurement on an old snapshot cannot be filled from current settings.
func RoundStandardSettingError(sem *float64, snapshot *RoundSnapshot, stats *ItemResult, review AngoffReview, livePolicy ...string) StandardSettingError {
	legacy := snapshot != nil && snapshot.Measurement == nil
	if snapshot != nil {
		sem = nil
		if snapshot.Measurement != nil {
			sem = snapshot.Measurement.SEM
		}
	}
	rawScale := false
	if snapshot != nil {
		rawScale = snapshot.Measurement != nil && snapshot.Measurement.Unit == "raw_points" || snapshot.Measurement == nil && snapshot.RoundingPolicy == NearestRawPoint
	} else if len(livePolicy) > 0 {
		rawScale = livePolicy[0] == NearestRawPoint
	} else if stats != nil {
		rawScale = stats.RoundingPolicy == NearestRawPoint
	}
	if rawScale {
		out := ComputeStandardSettingError(sem, nil, false, legacy)
		out.Unit = "raw_points"
		out.Assumption = "SEM dan SEJ memakai poin skor tes. Galat gabungan dibulatkan ke poin terdekat."
		if review.Ready && (stats == nil || stats.PanelistCount == review.CompletePanelistCount) {
			out.SEJ = review.Mix.SEJPoints
		}
		if out.SEM != nil && out.SEJ != nil {
			v := RoundRawPoints(math.Hypot(*out.SEM, *out.SEJ))
			if !math.IsInf(v, 0) {
				out.SeSS = &v
			}
		}
		return out
	}
	if stats == nil || !review.Ready || stats.PanelistCount != review.CompletePanelistCount {
		return ComputeStandardSettingError(sem, nil, false, legacy)
	}
	source := *stats
	method := source.ScoringMethod
	if method == "" && snapshot != nil {
		method = snapshot.ScoringMethod
	}
	source.StandardError = review.WeightedSEJ
	if NormalizeScoringMethod(method) == ScoringAverage {
		source.StandardError = review.AverageSEJ
	}
	return ComputeStandardSettingError(sem, &source, true, legacy)
}
