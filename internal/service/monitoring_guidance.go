package service

import (
	"math"

	"standard-setting/backend/internal/domain"
)

type MonitoringItemGuidance struct {
	ItemID string   `json:"itemId"`
	Range  *float64 `json:"range"`
	State  string   `json:"state"`
}

func monitoringItemGuidance(round domain.Round, items []domain.Item) []MonitoringItemGuidance {
	rows := make([]MonitoringItemGuidance, 0, len(items))
	for _, item := range items {
		row := MonitoringItemGuidance{ItemID: item.ID, State: "waiting"}
		if round.Status == "finalized" {
			row.State = "unavailable"
			if round.FinalSnapshot != nil {
				stats, ok := round.FinalSnapshot.ItemStatistics[item.ID]
				if ok && stats.PanelistCount > 0 &&
					!math.IsNaN(stats.Minimum) && !math.IsNaN(stats.Maximum) &&
					!math.IsInf(stats.Minimum, 0) && !math.IsInf(stats.Maximum, 0) &&
					stats.Minimum >= 0 && stats.Maximum <= 100 && stats.Maximum >= stats.Minimum {
					value := stats.Maximum - stats.Minimum
					row.Range = &value
					row.State = "agreement"
					priority := value > 20
					switch item.QuestionType {
					case domain.QuestionTypeEssay:
						priority = value >= 20 // Preserve the legacy essay guidance.
					case domain.QuestionTypeComplex, domain.QuestionTypeComplexMCMA, domain.QuestionTypeComplexBS:
						// Frozen maximum converts percentage range back to the original scale.
						// Tolerance suppresses floating point noise at the exact one-score boundary.
						priority = item.MaxScore > 0 && value*float64(item.MaxScore)/100 > 1+1e-12
					}
					if stats.PanelistCount >= 2 && priority {
						row.State = "priority"
					}
				}
			}
		}
		rows = append(rows, row)
	}
	return rows
}
