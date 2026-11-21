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
					priority := itemDiscussionPriority(item, value)
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

// Normalize only numerical noise from converting a score to percentage points and back.
func reachesDiscussionThreshold(value, threshold float64) bool {
	tolerance := 8 * (math.Nextafter(1, 2) - 1) * threshold
	return value >= threshold-tolerance
}

func itemDiscussionPriority(item domain.Item, spreadPP float64) bool {
	switch item.QuestionType {
	case domain.QuestionTypeEssay:
		return spreadPP >= 20
	case domain.QuestionTypeComplex, domain.QuestionTypeComplexMCMA, domain.QuestionTypeComplexBS:
		return item.MaxScore > 0 && reachesDiscussionThreshold(spreadPP*float64(item.MaxScore)/100, 2)
	default:
		return reachesDiscussionThreshold(spreadPP, 20)
	}
}
