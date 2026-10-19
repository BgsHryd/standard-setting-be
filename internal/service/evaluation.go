package service

import (
	"context"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
	"time"
	"unicode/utf8"
)

type evaluationStore interface {
	GetEvaluation(context.Context, string) (domain.Evaluation, error)
	SaveEvaluation(context.Context, domain.Evaluation, int) error
	ListEvaluations(context.Context, string) ([]domain.Evaluation, error)
}
type EvaluationView struct {
	Response          *domain.Evaluation `json:"response"`
	Closed            bool               `json:"closed"`
	InstrumentVersion int                `json:"instrumentVersion"`
	Labels            []string           `json:"labels"`
}
type EvaluationQuestion struct {
	Key          string  `json:"key"`
	Label        string  `json:"label"`
	Distribution [5]int  `json:"distribution"`
	Median       float64 `json:"median"`
}
type EvaluationAggregate struct {
	InstrumentVersion int                  `json:"instrumentVersion"`
	Invited           int                  `json:"invited"`
	Responded         int                  `json:"responded"`
	Missing           int                  `json:"missing"`
	SmallPanel        bool                 `json:"smallPanel"`
	Closed            bool                 `json:"closed"`
	Questions         []EvaluationQuestion `json:"questions"`
}

var evaluationLabels = []string{"1 — Sangat rendah / sangat tidak memadai", "2 — Rendah / tidak memadai", "3 — Cukup", "4 — Tinggi / memadai", "5 — Sangat tinggi / sangat memadai"}

func evaluationID(project, user string) string {
	return fmt.Sprintf("evaluation:%d:%s:%s:1", len(project), project, user)
}
func (s *Service) evaluationScope(ctx context.Context, userID string) (domain.User, domain.Project, error) {
	u, err := s.store.FindByID(ctx, userID)
	if err != nil {
		return u, domain.Project{}, err
	}
	if !u.Active || u.Removed || u.Role != domain.RolePanelist {
		return u, domain.Project{}, errors.New("panelis aktif wajib ditugaskan ke proyek")
	}
	p, err := s.store.GetProject(ctx, u.ProjectID)
	if err != nil {
		return u, p, err
	}
	sub, err := subjectOf(p, u.SubjectCode)
	if err != nil || !contains(sub.PanelistUserIDs, u.ID) || p.Status == domain.ProjectDraft {
		return u, p, errors.New("panelis tidak ditugaskan pada proyek aktif/selesai")
	}
	return u, p, nil
}
func (s *Service) GetEvaluation(ctx context.Context, userID string) (EvaluationView, error) {
	view := EvaluationView{InstrumentVersion: 1, Labels: evaluationLabels}
	u, p, err := s.evaluationScope(ctx, userID)
	if err != nil {
		return view, err
	}
	view.Closed = p.EvaluationClosed
	st, ok := s.store.(evaluationStore)
	if !ok {
		return view, errors.New("penyimpanan evaluasi tidak tersedia")
	}
	row, err := st.GetEvaluation(ctx, evaluationID(p.ID, u.ID))
	if errors.Is(err, mongo.ErrNoDocuments) {
		return view, nil
	}
	if err == nil {
		view.Response = &row
	}
	return view, err
}
func (s *Service) SaveEvaluation(ctx context.Context, userID string, input domain.Evaluation) (domain.Evaluation, error) {
	var result domain.Evaluation
	if input.Confidence < 1 || input.Confidence > 5 || input.Training < 1 || input.Training > 5 || input.TimeAdequacy < 1 || input.TimeAdequacy > 5 || utf8.RuneCountInString(input.Comment) > 1000 || input.Version < 0 || (input.InstrumentVersion != 0 && input.InstrumentVersion != 1) {
		return result, errors.New("semua skor harus 1–5; komentar maksimal 1000 karakter; instrumen versi 1")
	}
	st, ok := s.store.(evaluationStore)
	if !ok {
		return result, errors.New("penyimpanan evaluasi tidak tersedia")
	}
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, p, err := s.evaluationScope(tx, userID)
		if err != nil {
			return err
		}
		if p.EvaluationClosed {
			return errors.New("periode evaluasi ditutup")
		}
		if err = s.store.TouchProject(tx, p.ID); err != nil {
			return err
		}
		id := evaluationID(p.ID, u.ID)
		old, err := st.GetEvaluation(tx, id)
		if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
			return err
		}
		if input.Version != old.Version {
			return errors.New("versi evaluasi berubah; muat ulang")
		}
		now := time.Now().UTC()
		result = domain.Evaluation{ID: id, ProjectID: p.ID, UserID: u.ID, SubjectCode: u.SubjectCode, InstrumentVersion: 1, Version: old.Version + 1, Confidence: input.Confidence, Training: input.Training, TimeAdequacy: input.TimeAdequacy, Comment: input.Comment, CreatedAt: old.CreatedAt, UpdatedAt: now}
		if result.CreatedAt.IsZero() {
			result.CreatedAt = now
		}
		return st.SaveEvaluation(tx, result, old.Version)
	})
	return result, err
}
func (s *Service) EvaluationSummary(ctx context.Context, projectID string) (EvaluationAggregate, error) {
	result := EvaluationAggregate{InstrumentVersion: 1, Questions: []EvaluationQuestion{}}
	p, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return result, err
	}
	result.Closed = p.EvaluationClosed
	st, ok := s.store.(evaluationStore)
	if !ok {
		return result, errors.New("penyimpanan evaluasi tidak tersedia")
	}
	rows, err := st.ListEvaluations(ctx, projectID)
	if err != nil {
		return result, err
	}
	users, err := s.store.ListUsers(ctx, projectID)
	if err != nil {
		return result, err
	}
	invited := map[string]bool{}
	for _, u := range users {
		if u.Role == domain.RolePanelist && u.ProjectID == projectID {
			invited[u.ID] = true
		}
	}
	for _, r := range rows {
		invited[r.UserID] = true
	}
	result.Invited = len(invited)
	result.Responded = len(rows)
	result.Missing = result.Invited - result.Responded
	result.SmallPanel = result.Responded < 3
	if result.SmallPanel {
		return result, nil
	}
	keys := []string{"confidence", "training", "timeAdequacy"}
	labels := []string{"Keyakinan atas penilaian", "Kejelasan pelatihan", "Kecukupan waktu"}
	for i, key := range keys {
		q := EvaluationQuestion{Key: key, Label: labels[i]}
		values := []float64{}
		for _, r := range rows {
			score := []int{r.Confidence, r.Training, r.TimeAdequacy}[i]
			if score >= 1 && score <= 5 {
				q.Distribution[score-1]++
				values = append(values, float64(score))
			}
		}
		q.Median = domain.ComputeStatistics(values, "none").Median
		result.Questions = append(result.Questions, q)
	}
	return result, nil
}
