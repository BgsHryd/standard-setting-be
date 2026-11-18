package domain

import "math"

type AngoffGroup struct {
	ItemCount   int      `json:"itemCount"`
	MeanSum     *float64 `json:"meanSum"`
	MeanPercent *float64 `json:"meanPercent"`
}
type AngoffReviewRow struct {
	PanelistID  string   `json:"panelistId"`
	ModifiedSum *float64 `json:"modifiedSum"`
	ExtendedSum *float64 `json:"extendedSum"`
}
type AngoffReview struct {
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
	out := AngoffReview{FormulaVersion: "urip-review-v1", ActivePanelistCount: len(panelistIDs), Rows: []AngoffReviewRow{}}
	for _, item := range items {
		if item.QuestionType == QuestionTypePG {
			out.Modified.ItemCount++
		} else {
			out.Extended.ItemCount++
		}
	}
	modified, extended := 0.0, 0.0
	for _, id := range panelistIDs {
		row := AngoffReviewRow{PanelistID: id}
		m, e := 0.0, 0.0
		complete := len(items) > 0
		for _, item := range items {
			value, ok := percentages[id][item.ID]
			if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
				complete = false
				break
			}
			if item.QuestionType == QuestionTypePG {
				m += value / 100
			} else {
				e += value / 100
			}
		}
		if complete {
			out.CompletePanelistCount++
			if out.Modified.ItemCount > 0 {
				row.ModifiedSum = &m
			}
			if out.Extended.ItemCount > 0 {
				row.ExtendedSum = &e
			}
			modified += m
			extended += e
		}
		out.Rows = append(out.Rows, row)
	}
	out.Ready = out.ActivePanelistCount > 0 && out.CompletePanelistCount == out.ActivePanelistCount
	if out.Ready {
		for _, entry := range []struct {
			group *AngoffGroup
			sum   float64
		}{{&out.Modified, modified}, {&out.Extended, extended}} {
			if entry.group.ItemCount > 0 {
				mean := entry.sum / float64(out.CompletePanelistCount)
				percent := 100 * mean / float64(entry.group.ItemCount)
				entry.group.MeanSum = &mean
				entry.group.MeanPercent = &percent
			}
		}
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
	out := StandardSettingError{Unit: "percentage_points", Assumption: "SEM memakai konfigurasi mapel saat ini dalam percentage points; SEM dan SEJ diasumsikan independen. Satuan dan gabungan ini menunggu validasi Prof. Urip."}
	if historical {
		out.Assumption += " SEM historis tidak direkam; nilai SEM ini bukan SEM saat finalisasi."
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
