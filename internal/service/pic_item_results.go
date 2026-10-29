package service

import (
	"context"
	"errors"
	"sort"
	"standard-setting/backend/internal/domain"
	"time"
)

type PICResultPanelist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type PICResultItem struct {
	ItemID       string              `json:"itemId"`
	QuestionType domain.QuestionType `json:"questionType"`
	MaxScore     int                 `json:"maxScore"`
	Values       map[string]float64  `json:"values"`
}
type PICItemResults struct {
	ProjectCode string              `json:"projectCode"`
	SubjectCode string              `json:"subjectCode"`
	SubjectName string              `json:"subjectName"`
	Round       domain.Round        `json:"round"`
	Rounds      []domain.Round      `json:"rounds"`
	Panelists   []PICResultPanelist `json:"panelists"`
	Items       []PICResultItem     `json:"items"`
	GeneratedAt time.Time           `json:"generatedAt"`
}

func (s *Service) PICItemResults(ctx context.Context, id string, number int) (PICItemResults, error) {
	result := PICItemResults{}
	if number < 0 {
		return result, errors.New("nomor putaran tidak valid")
	}
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		user, err := s.store.FindByID(tx, id)
		if err != nil || !user.Active || user.Removed || user.Role != domain.RolePICMapel {
			return errors.New("akun bukan PIC mapel aktif")
		}
		project, err := s.store.GetProject(tx, user.ProjectID)
		if err != nil {
			return err
		}
		subject, err := subjectOf(project, user.SubjectCode)
		if err != nil || subject.PICUserID != id {
			return errors.New("PIC tidak ditugaskan pada proyek/mapel ini")
		}
		monitoring, err := s.MonitoringAtRound(tx, project.ID, subject.Code, number)
		if err != nil {
			return err
		}
		if monitoring.Round.Status != "active" && monitoring.Round.Status != "finalized" {
			return errors.New("putaran belum dimulai atau sudah dibatalkan")
		}
		history, err := s.store.ListRounds(tx, project.ID, subject.Code)
		if err != nil {
			return err
		}
		result = PICItemResults{ProjectCode: project.Code, SubjectCode: subject.Code, SubjectName: subject.Name, Round: publicRound(monitoring.Round), Rounds: []domain.Round{}, Panelists: []PICResultPanelist{}, Items: []PICResultItem{}, GeneratedAt: time.Now().UTC()}
		var snapshot *domain.RoundSnapshot
		for _, round := range history {
			if round.Status == "active" || round.Status == "finalized" {
				result.Rounds = append(result.Rounds, publicRound(round))
			}
			if round.Number == monitoring.Round.Number {
				snapshot = finalMonitoringSnapshot(round)
			}
		}
		sort.Slice(result.Rounds, func(i, j int) bool { return result.Rounds[i].Number < result.Rounds[j].Number })
		included := map[string]bool{}
		for _, panelist := range monitoring.Panelists {
			included[panelist.ID] = true
			result.Panelists = append(result.Panelists, PICResultPanelist{panelist.ID, panelist.Name})
		}
		rows := []domain.Submission{}
		if snapshot != nil {
			rows = snapshot.Submissions
		} else {
			rows, err = s.store.ListSubmissions(tx, project.ID, subject.Code, monitoring.Round.Number, "")
			if err != nil {
				return err
			}
		}
		for _, item := range monitoring.Items {
			out := PICResultItem{ItemID: item.ID, QuestionType: item.QuestionType, MaxScore: item.MaxScore, Values: map[string]float64{}}
			for _, row := range rows {
				if row.ItemID != item.ID || !included[row.PanelistID] || row.Status != "saved" || validateItemAnswer(item, row.Answer) != nil || !domain.SubmissionReviewed(row, monitoring.Round) || (project.FeatureVersion >= 1 && row.CandidateVersion != monitoring.Subject.CandidateVersion) {
					continue
				}
				out.Values[row.PanelistID] = responseValue(row.Answer, item.MaxScore)
			}
			result.Items = append(result.Items, out)
		}
		return nil
	})
	return result, err
}
