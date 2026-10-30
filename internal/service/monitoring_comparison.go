package service

import (
	"context"
	"math"
	"sort"
	"standard-setting/backend/internal/domain"
)

// Aggregate-only DTO: no frozen submissions or panelist identifiers are exposed.
type MonitoringItemRoundValue struct {
	ItemID string   `json:"itemId"`
	Mean   *float64 `json:"mean"`
	SD     *float64 `json:"sd"`
	Range  *float64 `json:"range"`
	N      int      `json:"n"`
}
type MonitoringItemRound struct {
	Number int                        `json:"number"`
	Status string                     `json:"status"`
	Items  []MonitoringItemRoundValue `json:"items"`
}

func comparisonValue(id string, stats domain.ItemResult) MonitoringItemRoundValue {
	result := MonitoringItemRoundValue{ItemID: id}
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	if stats.PanelistCount < 1 || !finite(stats.Mean) || stats.Mean < 0 || stats.Mean > 100 || !finite(stats.Minimum) || !finite(stats.Maximum) || stats.Minimum < 0 || stats.Maximum > 100 || stats.Maximum < stats.Minimum {
		return result
	}
	result.N = stats.PanelistCount
	result.Mean = &stats.Mean
	span := stats.Maximum - stats.Minimum
	result.Range = &span
	normalized := domain.SampleStatistics(stats)
	if normalized.SampleStandardDeviation != nil && finite(*normalized.SampleStandardDeviation) && *normalized.SampleStandardDeviation >= 0 {
		result.SD = normalized.SampleStandardDeviation
	}
	return result
}
func (s *Service) monitoringItemRounds(ctx context.Context, p domain.Project, subject domain.ProjectSubject, history []domain.Round, items []domain.Item, users []domain.User) ([]MonitoringItemRound, error) {
	result := make([]MonitoringItemRound, 0, len(history))
	currentSubject, err := subjectOf(p, subject.Code)
	if err != nil {
		return nil, err
	}
	for _, round := range history {
		out := MonitoringItemRound{Number: round.Number, Status: round.Status, Items: []MonitoringItemRoundValue{}}
		values := map[string]map[string]float64{}
		if round.Status == "active" {
			roster, err := effectivePanelists(p, currentSubject, users, round)
			if err != nil {
				return nil, err
			}
			included := map[string]bool{}
			for _, u := range roster {
				included[u.ID] = true
			}
			rows, err := s.store.ListSubmissions(ctx, p.ID, subject.Code, round.Number, "")
			if err != nil {
				return nil, err
			}
			byID := map[string]domain.Item{}
			for _, item := range items {
				byID[item.ID] = item
			}
			for _, row := range rows {
				item, ok := byID[row.ItemID]
				if !ok || !included[row.PanelistID] || row.Status != "saved" || validateItemAnswer(item, row.Answer) != nil || !domain.SubmissionReviewed(row, round) || (p.FeatureVersion >= 1 && row.CandidateVersion != currentSubject.CandidateVersion) {
					continue
				}
				if values[item.ID] == nil {
					values[item.ID] = map[string]float64{}
				}
				values[item.ID][row.PanelistID] = responseValue(row.Answer, item.MaxScore)
			}
		}
		for _, item := range items {
			stats := domain.ItemResult{}
			if round.Status == "finalized" && round.FinalSnapshot != nil {
				stats = round.FinalSnapshot.ItemStatistics[item.ID]
			}
			if round.Status == "active" {
				scores := []float64{}
				for _, value := range values[item.ID] {
					scores = append(scores, value)
				}
				sort.Float64s(scores)
				stats = domain.ComputeStatistics(scores, "none")
			}
			out.Items = append(out.Items, comparisonValue(item.ID, stats))
		}
		result = append(result, out)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Number < result[j].Number })
	return result, nil
}
