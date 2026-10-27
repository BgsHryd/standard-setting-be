package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/domain"
)

var ErrInvalidParticipantAccount = errors.New("data akun peserta tidak valid")

type ParticipantAccountInput struct {
	ProjectID      string      `json:"projectId"`
	ProjectVersion int         `json:"projectVersion"`
	SubjectCode    string      `json:"subjectCode"`
	Role           domain.Role `json:"role"`
	SlotID         string      `json:"slotId,omitempty"`
	Name           string      `json:"name"`
	Username       string      `json:"username"`
}
type ParticipantAccountEdit struct {
	ProjectVersion int    `json:"projectVersion"`
	Name           string `json:"name"`
	Username       string `json:"username"`
}

func normalizeParticipantIdentity(name, username string) (string, string, error) {
	name, username = strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(username))
	if name == "" || len([]rune(name)) > 200 || !usernamePattern.MatchString(username) {
		return "", "", ErrInvalidParticipantAccount
	}
	return name, username, nil
}
func (s *Service) availableParticipantUsername(ctx context.Context, username, userID string) error {
	u, err := s.store.FindByUsername(ctx, username)
	if err == nil {
		if u.ID == userID {
			return nil
		}
		return errors.New("username sudah digunakan")
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return err
	}
	return nil
}
func (s *Service) participantProject(ctx context.Context, id string, version int) (domain.Project, error) {
	p, err := s.store.GetProject(ctx, id)
	if err != nil {
		return p, err
	}
	if p.Status == domain.ProjectCompleted {
		return p, errors.New("akun proyek selesai tidak dapat diubah strukturnya")
	}
	if p.Version != version {
		return p, errors.New("konfigurasi sudah berubah; muat ulang sebelum mengubah akun")
	}
	ensureSlots(&p)
	return p, nil
}

// Rebuild the legacy roster projections from its authoritative slots.
func syncParticipantRoster(p *domain.Project) {
	ready := len(p.Subjects) > 0
	for i := range p.Subjects {
		sub := &p.Subjects[i]
		sub.PICUserID = ""
		sub.PICName = ""
		sub.PanelistUserIDs = []string{}
		sub.PanelistNames = []string{}
		sub.PanelistCount = 0
		hasPIC := false
		for _, slot := range sub.Slots {
			if slot.UserID == "" {
				ready = false
			}
			if slot.Role == domain.RolePICMapel {
				hasPIC = true
				sub.PICUserID = slot.UserID
				sub.PICName = slot.Name
			} else if slot.Role == domain.RolePanelist {
				sub.PanelistCount++
				sub.PanelistNames = append(sub.PanelistNames, slot.Name)
				if slot.UserID != "" {
					sub.PanelistUserIDs = append(sub.PanelistUserIDs, slot.UserID)
				}
			}
		}
		if !hasPIC || sub.PanelistCount == 0 {
			ready = false
		}
	}
	p.AccountsGenerated = ready
}
func (s *Service) saveParticipantMutation(ctx context.Context, p domain.Project, u *domain.User, actor, action string) error {
	if err := s.store.TouchProject(ctx, p.ID); err != nil {
		return err
	}
	syncParticipantRoster(&p)
	p.Version++
	p.UpdatedAt = time.Now().UTC()
	u.UpdatedAt = p.UpdatedAt
	if action == "user_create" {
		creator, ok := s.store.(interface {
			InsertUsers(context.Context, []domain.User) error
		})
		if !ok {
			return errors.New("penyimpanan akun baru tidak tersedia")
		}
		if err := creator.InsertUsers(ctx, []domain.User{*u}); err != nil {
			return err
		}
	} else if err := s.store.SaveUser(ctx, *u); err != nil {
		return err
	}
	if err := s.store.SaveProject(ctx, p); err != nil {
		return err
	}
	return s.recordAudit(ctx, actor, u.ID, p.ID, action)
}

func (s *Service) CreateParticipantAccount(ctx context.Context, input ParticipantAccountInput, actor string) (domain.User, error) {
	var result domain.User
	name, username, err := normalizeParticipantIdentity(input.Name, input.Username)
	if err != nil {
		return result, err
	}
	if input.Role != domain.RolePICMapel && input.Role != domain.RolePanelist {
		return result, ErrInvalidParticipantAccount
	}
	err = s.store.WithTransaction(ctx, func(tx context.Context) error {
		p, err := s.participantProject(tx, input.ProjectID, input.ProjectVersion)
		if err != nil {
			return err
		}
		subIndex := -1
		for i, sub := range p.Subjects {
			if sub.Code == input.SubjectCode {
				subIndex = i
				break
			}
		}
		if subIndex < 0 {
			return errors.New("mata pelajaran tidak ditemukan di proyek")
		}
		sub := &p.Subjects[subIndex]
		slotIndex := -1
		picCount := 0
		for i, slot := range sub.Slots {
			if slot.Role == domain.RolePICMapel {
				picCount++
				if input.Role == domain.RolePICMapel && slot.UserID != "" {
					return errors.New("mata pelajaran sudah memiliki PIC")
				}
			}
			if input.SlotID != "" && slot.ID == input.SlotID {
				if slot.Role != input.Role || slot.UserID != "" {
					return errors.New("slot akun sudah terisi atau role tidak sesuai")
				}
				slotIndex = i
			}
			if input.SlotID == "" && slotIndex < 0 && slot.Role == input.Role && slot.UserID == "" {
				slotIndex = i
			}
		}
		if input.SlotID != "" && slotIndex < 0 {
			return errors.New("slot akun tidak ditemukan di mata pelajaran")
		}
		if input.Role == domain.RolePICMapel && picCount > 1 {
			return errors.New("penugasan PIC ganda")
		}
		if slotIndex < 0 {
			if input.Role == domain.RolePanelist && sub.PanelistCount >= 1000 {
				return errors.New("jumlah panelis maksimal 1000")
			}
			sub.Slots = append(sub.Slots, domain.PersonnelSlot{ID: newID("slot"), Role: input.Role})
			slotIndex = len(sub.Slots) - 1
		}
		if err = s.availableParticipantUsername(tx, username, ""); err != nil {
			return err
		}
		hash, err := auth.HashPassword(s.initialPassword)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		u := domain.User{ID: newID("user"), Username: username, Name: name, PasswordHash: hash, Role: input.Role, ProjectID: p.ID, ProjectName: p.Name, SubjectCode: sub.Code, SlotID: sub.Slots[slotIndex].ID, Active: true, MustChangePassword: true, CreatedAt: now}
		sub.Slots[slotIndex].UserID = u.ID
		sub.Slots[slotIndex].Name = name
		if err = s.saveParticipantMutation(tx, p, &u, actor, "user_create"); err != nil {
			return err
		}
		result = u
		return nil
	})
	return result, err
}

func (s *Service) participantAssignment(ctx context.Context, id string, version int) (domain.User, domain.Project, int, int, error) {
	u, err := s.store.FindByID(ctx, id)
	if err != nil {
		return u, domain.Project{}, 0, 0, err
	}
	if u.Role == domain.RoleAdminSuper || u.Removed || u.ProjectID == "" || (u.Role != domain.RolePICMapel && u.Role != domain.RolePanelist) {
		return u, domain.Project{}, 0, 0, errors.New("hanya akun peserta yang masih ditugaskan dapat diubah")
	}
	p, err := s.participantProject(ctx, u.ProjectID, version)
	if err != nil {
		return u, p, 0, 0, err
	}
	for i, sub := range p.Subjects {
		if sub.Code == u.SubjectCode {
			for j, slot := range sub.Slots {
				if slot.UserID == u.ID && slot.Role == u.Role && (u.SlotID == "" || slot.ID == u.SlotID) {
					return u, p, i, j, nil
				}
			}
		}
	}
	return u, p, 0, 0, errors.New("akun tidak sesuai slot penugasan proyek")
}
func (s *Service) UpdateParticipantAccount(ctx context.Context, id string, input ParticipantAccountEdit, actor string) (domain.User, error) {
	var result domain.User
	name, username, err := normalizeParticipantIdentity(input.Name, input.Username)
	if err != nil {
		return result, err
	}
	err = s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, p, i, j, err := s.participantAssignment(tx, id, input.ProjectVersion)
		if err != nil {
			return err
		}
		if err = s.availableParticipantUsername(tx, username, u.ID); err != nil {
			return err
		}
		if u.Username != username {
			u.AuthVersion++
		}
		u.Username = username
		u.Name = name
		u.SlotID = p.Subjects[i].Slots[j].ID
		p.Subjects[i].Slots[j].Name = name
		if err = s.saveParticipantMutation(tx, p, &u, actor, "user_identity_update"); err != nil {
			return err
		}
		result = u
		return nil
	})
	return result, err
}
func (s *Service) RemoveParticipantAccount(ctx context.Context, id string, version int, actor string) error {
	return s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, p, i, j, err := s.participantAssignment(tx, id, version)
		if err != nil {
			return err
		}
		if u.Role == domain.RolePICMapel {
			p.Subjects[i].Slots[j] = domain.PersonnelSlot{ID: newID("slot"), Role: domain.RolePICMapel}
		} else {
			p.Subjects[i].Slots = append(p.Subjects[i].Slots[:j], p.Subjects[i].Slots[j+1:]...)
		}
		u.Active = false
		u.Removed = true
		u.AuthVersion++
		return s.saveParticipantMutation(tx, p, &u, actor, "user_remove")
	})
}
