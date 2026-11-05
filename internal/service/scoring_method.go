package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"standard-setting/backend/internal/domain"
)

func testScoreForItems(items []domain.Item, values map[string]float64, method string) (float64, error) {
	percentages := make([]float64, 0, len(items))
	maximums := make([]int, 0, len(items))
	for _, item := range items {
		value, ok := values[item.ID]
		if !ok {
			return 0, errors.New("jawaban penilaian belum lengkap")
		}
		percentages = append(percentages, value)
		maximums = append(maximums, item.MaxScore)
	}
	value, ok := domain.TestScore(percentages, maximums, method)
	if !ok {
		return 0, errors.New("nilai atau metode skoring tidak valid")
	}
	return value, nil
}

// Frozen metadata is reconstructed only from valid included answers. It is also
// used by historical monitoring, so editing the master cannot invalidate it.
func frozenScoringResponses(snap domain.RoundSnapshot) (map[string]domain.Item, map[string]map[string]float64, error) {
	included := map[string]bool{}
	for _, id := range snap.PanelistIDs {
		included[id] = true
	}
	items := map[string]domain.Item{}
	values := map[string]map[string]float64{}
	for _, row := range snap.Submissions {
		if !included[row.PanelistID] || row.Status != "saved" || row.CandidateVersion != snap.CandidateVersion || !domain.SubmissionReviewed(row, domain.Round{Revision: snap.Revision}) {
			continue
		}
		if _, ok := snap.ItemStatistics[row.ItemID]; len(snap.ItemStatistics) > 0 && !ok {
			continue
		}
		maximum := len(row.Answer.Distribution) - 1
		if row.Answer.QuestionType == domain.QuestionTypePG {
			maximum = 1
		}
		item := domain.Item{ID: row.ItemID, QuestionType: row.Answer.QuestionType, MaxScore: maximum}
		if validateItemAnswer(item, row.Answer) != nil {
			continue
		}
		if previous, ok := items[item.ID]; ok && (previous.QuestionType != item.QuestionType || previous.MaxScore != maximum) {
			return nil, nil, fmt.Errorf("bentuk respons historis butir %s tidak konsisten", item.ID)
		}
		items[item.ID] = item
		if values[row.PanelistID] == nil {
			values[row.PanelistID] = map[string]float64{}
		}
		values[row.PanelistID][row.ItemID] = responseValue(row.Answer, maximum)
	}
	return items, values, nil
}

// Only derived test statistics change. Original responses, item statistics,
// cohort, identity, rounding policy and finalization timestamps are retained.
func recalculateSnapshot(snap domain.RoundSnapshot, method, fallbackPolicy string) (domain.RoundSnapshot, error) {
	if len(snap.ItemStatistics) == 0 || len(snap.PanelistIDs) == 0 || snap.Submissions == nil {
		return snap, errors.New("snapshot tidak memiliki respons lengkap untuk menghitung ulang NBK")
	}
	items, values, err := frozenScoringResponses(snap)
	if err != nil {
		return snap, err
	}
	ids := make([]string, 0, len(snap.ItemStatistics))
	for id := range snap.ItemStatistics {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	ordered := make([]domain.Item, 0, len(ids))
	for _, id := range ids {
		item, ok := items[id]
		if !ok {
			return snap, fmt.Errorf("respons historis butir %s belum lengkap", id)
		}
		ordered = append(ordered, item)
	}
	scores := make([]float64, 0, len(snap.PanelistIDs))
	for _, id := range snap.PanelistIDs {
		score, err := testScoreForItems(ordered, values[id], method)
		if err != nil {
			return snap, fmt.Errorf("snapshot putaran %d: %w", snap.RoundNumber, err)
		}
		scores = append(scores, score)
	}
	policy := snap.RoundingPolicy
	if policy == "" {
		policy = snap.Statistics.RoundingPolicy
	}
	if policy == "" {
		policy = fallbackPolicy
	}
	stats := domain.ComputeStatistics(scores, policy)
	stats.ScoringMethod = domain.NormalizeScoringMethod(method)
	stats.RoundNumber, stats.Revision, stats.CompletedAt = snap.Statistics.RoundNumber, snap.Statistics.Revision, snap.Statistics.CompletedAt
	stats.PanelistIDs = append([]string(nil), snap.Statistics.PanelistIDs...)
	snap.Statistics = stats
	snap.ScoringMethod = stats.ScoringMethod
	return snap, nil
}

func (s *Service) recalculateProjectScoring(ctx context.Context, p *domain.Project, actor string) error {
	for i, subject := range p.Subjects {
		rounds, err := s.store.ListRounds(ctx, p.ID, subject.Code)
		if err != nil {
			return err
		}
		statsByID := map[string]domain.ItemResult{}
		for _, round := range rounds {
			if round.Status == "finalized" && round.FinalSnapshot == nil {
				return errors.New("hasil terkunci tanpa snapshot tidak dapat dihitung ulang")
			}
			changed := false
			round.Snapshots = append([]domain.RoundSnapshot(nil), round.Snapshots...)
			for j, snapshot := range round.Snapshots {
				updated, err := recalculateSnapshot(snapshot, p.ScoringMethod, projectRoundingPolicy(*p))
				if err != nil {
					return err
				}
				round.Snapshots[j] = updated
				statsByID[updated.ID] = updated.Statistics
				changed = true
			}
			if round.FinalSnapshot != nil {
				updated, err := recalculateSnapshot(*round.FinalSnapshot, p.ScoringMethod, projectRoundingPolicy(*p))
				if err != nil {
					return err
				}
				round.FinalSnapshot = &updated
				statsByID[updated.ID] = updated.Statistics
				changed = true
			}
			if changed {
				round.UpdatedAt = time.Now().UTC()
				if err := s.store.SaveRound(ctx, round); err != nil {
					return err
				}
			}
		}
		if subject.FinalNBK != nil {
			stats, ok := statsByID[subject.FinalNBK.SnapshotID]
			if !ok {
				return errors.New("snapshot NBK final tidak tersedia untuk menghitung ulang")
			}
			decision := *subject.FinalNBK
			decision.Statistics = stats
			p.Subjects[i].FinalNBK = &decision
		}
	}
	return s.recordAudit(ctx, actor, p.ID, p.ID, "project_scoring_"+p.ScoringMethod)
}
