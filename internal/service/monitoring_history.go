package service

import (
	"context"
	"errors"

	"standard-setting/backend/internal/domain"
)

// MonitoringAtRound uses zero for the latest round and positive numbers for a pinned view.
func (s *Service) MonitoringAtRound(ctx context.Context, projectID, subjectCode string, number int) (Monitoring, error) {
	if number < 0 {
		return Monitoring{}, errors.New("nomor putaran tidak valid")
	}
	result, err := s.monitoringRound(ctx, projectID, subjectCode, number)
	if !errors.Is(err, errRoundUnavailable) {
		return result, err
	}
	p, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return Monitoring{}, err
	}
	subject, err := subjectOf(p, subjectCode)
	if err != nil {
		return Monitoring{}, err
	}
	history, err := s.store.ListRounds(ctx, projectID, subjectCode)
	if err != nil {
		return Monitoring{}, err
	}
	latest := 0
	for i, round := range history {
		latest = max(latest, round.Number)
		history[i] = publicRound(round)
	}
	limit := p.PlannedRounds
	if limit == 0 {
		limit = latest
		if p.Status == domain.ProjectActive {
			limit++
		}
	}
	if number <= latest || number > limit {
		return Monitoring{}, errRoundUnavailable
	}
	return Monitoring{
		Project: p, Subject: subject, History: history,
		Round:     domain.Round{ProjectID: p.ID, SubjectCode: subjectCode, Number: number, Status: "future"},
		Panelists: []MonitoringPanelist{}, Items: []domain.Item{}, Excluded: []MonitoringExclusion{}, Notes: []MonitoringNote{},
		FinalNBKChoices: []FinalNBKChoice{}, TotalItems: len(subject.ItemIDs),
		ItemGuidance: []MonitoringItemGuidance{},
	}, nil
}

func finalMonitoringSnapshot(round domain.Round) *domain.RoundSnapshot {
	if round.Status == "finalized" && round.FinalSnapshot != nil && round.FinalSnapshot.Submissions != nil {
		return round.FinalSnapshot
	}
	return nil
}
