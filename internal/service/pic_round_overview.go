package service

import (
	"context"
	"errors"

	"standard-setting/backend/internal/domain"
)

// PICRoundOverview contains frozen anonymous aggregates for one selected round.
// It intentionally omits roster, contacts, submissions and editable note state.
type PICRoundOverview struct {
	Project struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"project"`
	Subject struct {
		Code string `json:"code"`
		Name string `json:"name"`
	} `json:"subject"`
	Round         domain.Round   `json:"round"`
	Items         []domain.Item  `json:"items"`
	RoundReview   *RoundReview   `json:"roundReview"`
	RoundOutcomes []RoundOutcome `json:"roundOutcomes"`
	ReadOnly      bool           `json:"readOnly"`
}

func (s *Service) PICRoundOverview(ctx context.Context, userID string, number int) (PICRoundOverview, error) {
	var result PICRoundOverview
	if number < 1 {
		return result, errors.New("nomor putaran harus lebih dari nol")
	}
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		user, err := s.store.FindByID(tx, userID)
		if err != nil || !user.Active || user.Removed || user.Role != domain.RolePICMapel {
			return errors.New("akun bukan PIC mapel aktif")
		}
		project, err := s.store.GetProject(tx, user.ProjectID)
		if err != nil {
			return err
		}
		subject, err := subjectOf(project, user.SubjectCode)
		if err != nil || project.ID != user.ProjectID || subject.PICUserID != userID {
			return errors.New("PIC tidak ditugaskan pada proyek/mapel ini")
		}
		if project.Status == domain.ProjectDraft {
			return errors.New("proyek masih Draft")
		}
		history, err := s.store.ListRounds(tx, project.ID, subject.Code)
		if err != nil {
			return err
		}
		var selected domain.Round
		for _, round := range history {
			if round.Number == number {
				selected = round
				break
			}
		}
		if selected.Status != "finalized" {
			return errors.New("hasil hanya tersedia untuk putaran yang sudah dikunci")
		}
		snap := selected.FinalSnapshot
		if snap == nil || snap.Revision != domain.RoundRevision(selected) || snap.RoundNumber != number || snap.Submissions == nil {
			return errors.New("hasil rinci putaran ini belum tersedia; snapshot final belum tersimpan atau sudah dibatalkan")
		}
		review, outcomes, err := s.roundResults(tx, "", project, subject, selected, history)
		if err != nil {
			return err
		}
		if review == nil || review.RoundNumber != number {
			return errors.New("hasil putaran yang dipilih tidak tersedia")
		}
		items, err := s.store.ListItems(tx, subject.Code, project.ID, false)
		if err != nil {
			return err
		}
		byID := map[string]domain.Item{}
		for _, item := range items {
			byID[item.ID] = item
		}
		result.Items = []domain.Item{}
		for _, id := range subject.ItemIDs {
			if _, exists := snap.ItemStatistics[id]; !exists {
				continue
			}
			item, exists := byID[id]
			if !exists {
				return errors.New("metadata butir pada hasil putaran tidak tersedia")
			}
			result.Items = append(result.Items, domain.Item{ID: item.ID, QuestionType: item.QuestionType, MaxScore: item.MaxScore})
		}
		result.Project.ID, result.Project.Name = project.ID, project.Name
		result.Subject.Code, result.Subject.Name = subject.Code, subject.Name
		result.Round = publicRound(selected)
		result.RoundReview, result.RoundOutcomes = review, outcomes
		result.ReadOnly = true
		return nil
	})
	return result, err
}
