package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
)

type Store interface {
	WithTransaction(context.Context, func(context.Context) error) error
	ListSubjects(context.Context) ([]domain.Subject, error)
	CreateSubject(context.Context, domain.Subject) error
	TouchProject(context.Context, string) error
	SaveUser(context.Context, domain.User) error
	FindByUsername(context.Context, string) (domain.User, error)
	ListProjects(context.Context) ([]domain.Project, error)
	GetProject(context.Context, string) (domain.Project, error)
	SaveProject(context.Context, domain.Project) error
	DeleteProject(context.Context, string) error
	SyncProjectItems(context.Context, string, string, []string) error
	ListItems(context.Context, string, string, bool) ([]domain.Item, error)
	GetItem(context.Context, string) (domain.Item, error)
	UpsertItems(context.Context, []domain.Item) error
	ListUsers(context.Context, string) ([]domain.User, error)
	FindByID(context.Context, string) (domain.User, error)
	ReplaceProjectUsers(context.Context, string, []domain.User) error
	CurrentRound(context.Context, string, string) (domain.Round, error)
	SaveRound(context.Context, domain.Round) error
	TouchRound(context.Context, domain.Round) error
	ListRounds(context.Context, string, string) ([]domain.Round, error)
	ListSubmissions(context.Context, string, string, int, string) ([]domain.Submission, error)
	UpsertSubmission(context.Context, domain.Submission) (domain.Submission, error)
}
type Cache interface {
	GetCache(context.Context, string) ([]byte, error)
	SetCache(context.Context, string, []byte, time.Duration) error
	DeleteCache(context.Context, string) error
}
type Service struct {
	store           Store
	cache           Cache
	initialPassword string
}

func New(st Store, cache Cache, password string) *Service { return &Service{st, cache, password} }
func (s *Service) ListProjects(ctx context.Context) ([]domain.Project, error) {
	return s.store.ListProjects(ctx)
}
func (s *Service) GetProject(ctx context.Context, id string) (domain.Project, error) {
	p, err := s.store.GetProject(ctx, id)
	if err != nil {
		return p, err
	}
	ensureSlots(&p)
	rows, err := s.store.ListSubmissions(ctx, id, "", 0, "")
	p.ItemsEditable = p.Status != domain.ProjectCompleted && len(rows) == 0
	return p, err
}
func (s *Service) ListUsers(ctx context.Context, id string) ([]domain.User, error) {
	return s.store.ListUsers(ctx, id)
}
func (s *Service) ListItems(ctx context.Context, subject, project string, available bool) ([]domain.Item, error) {
	return s.store.ListItems(ctx, subject, project, available)
}
func (s *Service) GetItem(ctx context.Context, id string) (domain.Item, error) {
	return s.store.GetItem(ctx, id)
}

func (s *Service) ActivateProject(ctx context.Context, id string) (domain.Project, []string, error) {
	var result domain.Project
	var validation []string
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		p, err := s.store.GetProject(tx, id)
		if err != nil {
			return err
		}
		if p.Status != domain.ProjectDraft {
			return errors.New("hanya proyek Draft yang dapat diaktifkan")
		}
		validation = domain.ValidateActivation(p)
		if len(validation) > 0 {
			result = p
			return nil
		}
		users, err := s.store.ListUsers(tx, id)
		if err != nil {
			return err
		}
		for i := range p.Subjects {
			subject := p.Subjects[i]
			if p.SimulationEnabled {
				p.Subjects[i].PracticeStatus = "open"
			}
			if _, err := assignedPanelists(p, subject, users); err != nil {
				return err
			}
			pic, err := s.store.FindByID(tx, subject.PICUserID)
			if err != nil || !pic.Active || pic.Role != domain.RolePICMapel || pic.ProjectID != p.ID || pic.SubjectCode != subject.Code {
				return errors.New("akun PIC tidak sesuai proyek dan mapel")
			}
			for _, itemID := range subject.ItemIDs {
				item, err := s.store.GetItem(tx, itemID)
				if err != nil || item.ProjectID != id || item.SubjectCode != subject.Code || item.UsageStatus != domain.ItemAllocated {
					return errors.New("alokasi ID butir tidak valid")
				}
			}
			if _, err := s.openRound(tx, id, subject.Code, 1); err != nil {
				return err
			}
		}
		p.Status = domain.ProjectActive
		p.WizardStep = 5
		p.UpdatedAt = time.Now().UTC()
		if err := s.store.SaveProject(tx, p); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, validation, err
}

func (s *Service) DeleteProject(ctx context.Context, id, confirmation string) error {
	if confirmation != "HAPUS PROYEK" {
		return errors.New("konfirmasi penghapusan proyek tidak sesuai")
	}
	return s.store.WithTransaction(ctx, func(tx context.Context) error { return s.store.DeleteProject(tx, id) })
}

func (s *Service) UpsertItems(ctx context.Context, items []domain.Item) error {
	return s.store.WithTransaction(ctx, func(tx context.Context) error {
		seen := map[string]bool{}
		for i := range items {
			item := &items[i]
			item.ID = strings.TrimSpace(item.ID)
			item.KJ = strings.TrimSpace(item.KJ)
			if err := domain.ValidateItemMetadata(item.KJ, item.Measure); err != nil {
				return err
			}
			if item.ID == "" || item.SubjectCode == "" || item.SubjectName == "" || seen[item.ID] {
				return errors.New("ID butir/mapel wajib diisi dan ID tidak boleh ganda")
			}
			seen[item.ID] = true
			catalog, err := s.store.ListSubjects(tx)
			if err != nil {
				return err
			}
			found := false
			for _, sub := range catalog {
				if sub.Code == item.SubjectCode {
					item.SubjectName = sub.Name
					found = true
					break
				}
			}
			if !found {
				return errors.New("mata pelajaran tidak ditemukan di katalog")
			}
			if item.QuestionType != domain.QuestionTypePG && item.QuestionType != domain.QuestionTypeComplex && item.QuestionType != domain.QuestionTypeEssay {
				return errors.New("jenis soal tidak valid")
			}
			if item.MaxScore < 1 || item.MaxScore > 100 || (item.QuestionType == domain.QuestionTypePG && item.MaxScore != 1) {
				return errors.New("skor maksimum tidak valid")
			}
			old, err := s.store.GetItem(tx, item.ID)
			if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
				return err
			}
			if err == nil {
				if old.UsageStatus != domain.ItemAvailable || old.ReadOnly || old.ProjectID != "" {
					return errors.New("butir yang dialokasikan atau sudah diuji hanya dapat dilihat")
				}
				item.CreatedAt = old.CreatedAt
				item.ProjectHistory = old.ProjectHistory
			} else {
				item.CreatedAt = time.Now().UTC()
			}
			item.UsageStatus = domain.ItemAvailable
			item.ProjectID = ""
			item.ProjectName = ""
			item.ReadOnly = false
			item.FinalResult = nil
			item.UpdatedAt = time.Now().UTC()
		}
		return s.store.UpsertItems(tx, items)
	})
}

func subjectOf(p domain.Project, code string) (domain.ProjectSubject, error) {
	for _, subject := range p.Subjects {
		if subject.Code == code {
			return subject, nil
		}
	}
	return domain.ProjectSubject{}, errors.New("mata pelajaran tidak ditemukan di proyek")
}
func contains(ids []string, id string) bool {
	for _, value := range ids {
		if value == id {
			return true
		}
	}
	return false
}
func assignedPanelists(p domain.Project, subject domain.ProjectSubject, users []domain.User) ([]domain.User, error) {
	assigned := []domain.User{}
	seen := map[string]bool{}
	for _, id := range subject.PanelistUserIDs {
		if id == "" || seen[id] {
			return nil, errors.New("penugasan panelis kosong atau ganda")
		}
		seen[id] = true
		for _, u := range users {
			if u.ID == id && u.Active && !u.Removed && u.Role == domain.RolePanelist && u.ProjectID == p.ID && u.SubjectCode == subject.Code {
				assigned = append(assigned, u)
			}
		}
	}

	return assigned, nil
}
func newID(prefix string) string {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(value)
}
