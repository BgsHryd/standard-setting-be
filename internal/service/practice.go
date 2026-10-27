package service

import (
	"context"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
	"time"
)

const practiceDuration = 10 * time.Minute

type practiceStore interface {
	GetPracticeSession(context.Context, string) (domain.PracticeSession, error)
	SavePracticeSession(context.Context, domain.PracticeSession, int) error
}
type PracticeView struct {
	Message   string                 `json:"message,omitempty"`
	Enabled   bool                   `json:"enabled"`
	Open      bool                   `json:"open"`
	ReadOnly  bool                   `json:"readOnly"`
	ExpiresAt *time.Time             `json:"expiresAt"`
	Session   domain.PracticeSession `json:"session"`
	Items     []domain.Item          `json:"items"`
}

func practiceID(project, user string) string {
	return fmt.Sprintf("practice:%d:%s:%s", len(project), project, user)
}
func practiceItems() []domain.Item {
	return []domain.Item{
		{ID: "practice-pg", QuestionType: domain.QuestionTypePG, MaxScore: 1, Rubric: []string{"Contoh latihan pilihan ganda; berikan persentase kandidat yang dapat menjawab benar."}},
		{ID: "practice-complex", QuestionType: domain.QuestionTypeComplex, MaxScore: 3, Rubric: []string{"Bagikan 100 kandidat ke skor 0–3."}},
		{ID: "practice-essay", QuestionType: domain.QuestionTypeEssay, MaxScore: 4, Rubric: []string{"Bagikan 100 kandidat ke skor 0–4."}},
	}
}
func (s *Service) practiceScope(ctx context.Context, user string) (domain.User, domain.Project, domain.ProjectSubject, practiceStore, error) {
	u, p, err := s.ownPanelist(ctx, user)
	if err != nil {
		return u, p, domain.ProjectSubject{}, nil, err
	}
	sub, err := subjectOf(p, u.SubjectCode)
	if err != nil {
		return u, p, sub, nil, err
	}
	st, ok := s.store.(practiceStore)
	if !ok {
		return u, p, sub, nil, errors.New("penyimpanan latihan tidak tersedia")
	}
	if err = s.contactPolicy(ctx, user); err != nil {
		return u, p, sub, st, err
	}
	return u, p, sub, st, nil
}
func practiceView(p domain.Project, sub domain.ProjectSubject, row domain.PracticeSession) PracticeView {
	v := PracticeView{Enabled: p.SimulationEnabled, Open: p.Status == domain.ProjectActive && p.SimulationEnabled && sub.PracticeStatus == "open", Session: row, Items: practiceItems()}
	v.ReadOnly = !v.Open || row.FinishedAt != nil || !domain.ProjectWritable(p, time.Now().UTC())
	if !domain.ProjectWritable(p, time.Now().UTC()) {
		v.Message = "Latihan tersedia selama periode proyek berlangsung."
	}
	if !row.StartedAt.IsZero() {
		end := row.StartedAt.Add(practiceDuration)
		v.ExpiresAt = &end
		v.ReadOnly = v.ReadOnly || !time.Now().Before(end)
	}
	return v
}
func (s *Service) GetPractice(ctx context.Context, user string) (PracticeView, error) {
	u, p, sub, st, err := s.practiceScope(ctx, user)
	if err != nil {
		return PracticeView{}, err
	}
	row, err := st.GetPracticeSession(ctx, practiceID(p.ID, u.ID))
	if errors.Is(err, mongo.ErrNoDocuments) {
		row = domain.PracticeSession{Answers: map[string]domain.Answer{}}
		err = nil
	}
	return practiceView(p, sub, row), err
}
func (s *Service) StartPractice(ctx context.Context, user string) (PracticeView, error) {
	var v PracticeView
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, p, sub, st, err := s.practiceScope(tx, user)
		if err != nil {
			return err
		}
		if !domain.ProjectWritable(p, time.Now().UTC()) {
			return ErrPeriodClosed
		}
		if p.Status != domain.ProjectActive || !p.SimulationEnabled || sub.PracticeStatus != "open" {
			return errors.New("sesi latihan tidak dibuka")
		}
		row, err := st.GetPracticeSession(tx, practiceID(p.ID, u.ID))
		if err == nil {
			v = practiceView(p, sub, row)
			return nil
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return err
		}
		if err = s.store.TouchProject(tx, p.ID); err != nil {
			return err
		}
		row = domain.PracticeSession{ID: practiceID(p.ID, u.ID), UserID: u.ID, ProjectID: p.ID, SubjectCode: sub.Code, Version: 1, StartedAt: time.Now().UTC(), Answers: map[string]domain.Answer{}}
		if err = st.SavePracticeSession(tx, row, 0); err != nil {
			return err
		}
		v = practiceView(p, sub, row)
		return nil
	})
	return v, err
}
func (s *Service) changePractice(ctx context.Context, user string, expected int, itemID string, answer *domain.Answer) (PracticeView, error) {
	var v PracticeView
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, p, sub, st, err := s.practiceScope(tx, user)
		if err != nil {
			return err
		}
		if !domain.ProjectWritable(p, time.Now().UTC()) {
			return ErrPeriodClosed
		}
		row, err := st.GetPracticeSession(tx, practiceID(p.ID, u.ID))
		if err != nil {
			return err
		}
		v = practiceView(p, sub, row)
		if !v.Open {
			return errors.New("sesi latihan sudah ditutup PIC")
		}
		if row.Version != expected {
			return ErrRevisionConflict
		}
		if row.FinishedAt != nil {
			if answer == nil {
				return nil
			}
			return errors.New("latihan sudah selesai")
		}
		if err = s.store.TouchProject(tx, p.ID); err != nil {
			return err
		}
		if answer != nil {
			if v.ReadOnly {
				return errors.New("waktu latihan sudah habis")
			}
			found := false
			for _, item := range v.Items {
				if item.ID == itemID {
					found = true
					if err = validateItemAnswer(item, *answer); err != nil {
						return err
					}
				}
			}
			if !found {
				return errors.New("butir latihan tidak ditemukan")
			}
			if row.Answers == nil {
				row.Answers = map[string]domain.Answer{}
			}
			row.Answers[itemID] = *answer
		} else {
			now := time.Now().UTC()
			row.FinishedAt = &now
		}
		row.Version++
		if err = st.SavePracticeSession(tx, row, expected); err != nil {
			return err
		}
		v = practiceView(p, sub, row)
		return nil
	})
	return v, err
}
func (s *Service) SavePracticeAnswer(ctx context.Context, user, item string, answer domain.Answer, version int) (PracticeView, error) {
	return s.changePractice(ctx, user, version, item, &answer)
}
func (s *Service) FinishPractice(ctx context.Context, user string, version int) (PracticeView, error) {
	return s.changePractice(ctx, user, version, "", nil)
}
func (s *Service) EndSubjectPractice(ctx context.Context, actor, project, code string, version int) error {
	return s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, err := s.store.FindByID(tx, actor)
		if err != nil {
			return err
		}
		p, err := s.store.GetProject(tx, project)
		if err != nil {
			return err
		}
		sub, err := subjectOf(p, code)
		if err != nil {
			return err
		}
		if !u.Active || u.Removed || u.Role != domain.RolePICMapel || sub.PICUserID != u.ID {
			return errors.New("hanya PIC mapel yang dapat memulai penilaian")
		}
		if p.Version != version {
			return ErrRevisionConflict
		}
		if p.Status != domain.ProjectActive || !p.SimulationEnabled || sub.PracticeStatus != "open" {
			return errors.New("latihan tidak sedang dibuka")
		}
		if err = s.store.TouchProject(tx, p.ID); err != nil {
			return err
		}
		for i := range p.Subjects {
			if p.Subjects[i].Code == code {
				p.Subjects[i].PracticeStatus = "closed"
			}
		}
		p.Version++
		p.UpdatedAt = time.Now().UTC()
		if err = s.store.SaveProject(tx, p); err != nil {
			return err
		}
		return s.recordAudit(tx, actor, code, p.ID, "practice_end")
	})
}
