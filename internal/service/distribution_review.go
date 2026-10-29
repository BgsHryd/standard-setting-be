package service

import "standard-setting/backend/internal/domain"

// RoundReviewItem adds safe distribution aggregates only to the overview DTO.
// The ordinary grading feedback and other panelists' raw answers stay unchanged.
type RoundReviewItem struct {
	ItemFeedback
	OwnDistribution   []int     `json:"ownDistribution,omitempty"`
	PanelDistribution []float64 `json:"panelDistribution,omitempty"`
}

func snapshotReviewItem(snap domain.RoundSnapshot, itemID, userID string) RoundReviewItem {
	result := RoundReviewItem{ItemFeedback: snapshotFeedback(snap, itemID, userID)}
	members := map[string]bool{}
	for _, id := range snap.PanelistIDs {
		members[id] = true
	}
	seen := map[string]bool{}
	valid := true
	var sum []float64
	var questionType domain.QuestionType
	ownRows := 0
	for _, row := range snap.Submissions {
		if row.ItemID != itemID || !members[row.PanelistID] {
			continue
		}
		if row.PanelistID == userID {
			ownRows++
		}
		if seen[row.PanelistID] {
			valid = false
			continue
		}
		seen[row.PanelistID] = true
		answer := row.Answer
		if row.Status != "saved" || !answer.QuestionType.UsesDistribution() || answer.Value != nil || len(answer.Distribution) < 2 || domain.ValidateAnswer(answer) != nil {
			valid = false
			continue
		}
		if row.PanelistID == userID {
			result.OwnDistribution = append([]int(nil), answer.Distribution...)
		}
		if sum == nil {
			sum = make([]float64, len(answer.Distribution))
			questionType = answer.QuestionType
		}
		if len(answer.Distribution) != len(sum) || answer.QuestionType != questionType {
			valid = false
			continue
		}
		for score, count := range answer.Distribution {
			sum[score] += float64(count)
		}
	}
	if ownRows != 1 {
		result.OwnDistribution = nil
	}
	// Match the existing small-panel policy and never average an incomplete roster.
	if result.N < 3 || !valid || len(seen) != result.N || len(members) != result.N || len(sum) == 0 {
		return result
	}
	for score := range sum {
		sum[score] /= float64(result.N)
	}
	result.PanelDistribution = sum
	return result
}
