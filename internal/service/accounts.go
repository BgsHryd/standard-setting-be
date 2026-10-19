package service

import (
	"context"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/mongo"
	"regexp"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/domain"
	"strings"
	"time"
)

type AccountInput struct {
	SlotID      string      `json:"slotId"`
	Name        string      `json:"name"`
	Role        domain.Role `json:"role"`
	SubjectCode string      `json:"subjectCode"`
	Username    string      `json:"username,omitempty"`
}
type AccountRow struct {
	UserID      string      `json:"userId"`
	SlotID      string      `json:"slotId"`
	Name        string      `json:"name"`
	Role        domain.Role `json:"role"`
	SubjectCode string      `json:"subjectCode"`
	Username    string      `json:"username"`
	Status      string      `json:"status"`
	Active      bool        `json:"active"`
}

func (s *Service) AccountRows(ctx context.Context, id string) ([]AccountRow, error) {
	p, err := s.store.GetProject(ctx, id)
	if err != nil {
		return nil, err
	}
	ensureSlots(&p)
	users, err := s.store.ListUsers(ctx, id)
	if err != nil {
		return nil, err
	}
	byID := map[string]domain.User{}
	for _, u := range users {
		byID[u.ID] = u
	}
	rows := []AccountRow{}
	retained := map[string]bool{}
	for _, sub := range p.Subjects {
		for _, slot := range sub.Slots {
			row := AccountRow{UserID: slot.UserID, SlotID: slot.ID, Name: slot.Name, Role: slot.Role, SubjectCode: sub.Code, Status: "pending"}
			if u, ok := byID[slot.UserID]; ok && !u.Removed {
				row.Username = u.Username
				row.Active = u.Active
				row.Status = "active"
				if !u.Active {
					row.Status = "inactive"
				}
				retained[u.ID] = true
			}
			rows = append(rows, row)
		}
	}
	for _, u := range users {
		if !retained[u.ID] {
			rows = append(rows, AccountRow{UserID: u.ID, SlotID: u.SlotID, Name: u.Name, Role: u.Role, SubjectCode: u.SubjectCode, Username: u.Username, Status: "removed"})
		}
	}
	return rows, nil
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,79}$`)

func (s *Service) GenerateAccounts(ctx context.Context, projectID string, inputs []AccountInput) ([]domain.User, error) {
	result := []domain.User{}
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		p, err := s.store.GetProject(tx, projectID)
		if err != nil {
			return err
		}
		if p.Status == domain.ProjectCompleted {
			return errors.New("akun proyek selesai tidak dapat dibuat ulang")
		}
		if err = s.store.TouchProject(tx, p.ID); err != nil {
			return err
		}
		ensureSlots(&p)
		expected := 0
		for _, sub := range p.Subjects {
			expected += len(sub.Slots)
		}
		if len(inputs) != expected {
			return errors.New("jumlah baris akun tidak sesuai kebutuhan PIC dan panelis")
		}
		bySlot := map[string]AccountInput{}
		legacyCount := map[string]int{}
		for _, input := range inputs {
			if input.SlotID == "" {
				key := input.SubjectCode + string(input.Role)
				n := legacyCount[key]
				legacyCount[key]++
				for _, sub := range p.Subjects {
					if sub.Code == input.SubjectCode {
						slots := []domain.PersonnelSlot{}
						for _, slot := range sub.Slots {
							if slot.Role == input.Role {
								slots = append(slots, slot)
							}
						}
						if n < len(slots) {
							input.SlotID = slots[n].ID
						}
					}
				}
			}
			if input.SlotID == "" || bySlot[input.SlotID].SlotID != "" {
				return errors.New("identitas slot akun kosong atau ganda")
			}
			bySlot[input.SlotID] = input
		}
		users := []domain.User{}
		seenUsername := map[string]bool{}
		now := time.Now().UTC()
		for i := range p.Subjects {
			sub := &p.Subjects[i]
			if sub.PanelistCount < 1 {
				return errors.New("setiap mapel membutuhkan minimal satu panelis")
			}
			sub.PICUserID = ""
			sub.PanelistUserIDs = []string{}
			for j := range sub.Slots {
				slot := &sub.Slots[j]
				input, ok := bySlot[slot.ID]
				if !ok || input.Role != slot.Role || input.SubjectCode != sub.Code || strings.TrimSpace(input.Name) != slot.Name || slot.Name == "" {
					return errors.New("baris akun tidak sesuai nama, role, atau mata pelajaran konfigurasi tersimpan")
				}
				var u domain.User
				if slot.UserID != "" {
					u, err = s.store.FindByID(tx, slot.UserID)
					if err != nil {
						return err
					}
					if u.Removed {
						return errors.New("akun dihapus tidak dapat diaktifkan ulang lewat regenerasi")
					}
					if input.Username != "" && strings.ToLower(strings.TrimSpace(input.Username)) != u.Username {
						return errors.New("username akun yang sudah dibuat tidak dapat diubah")
					}
				} else {
					username := strings.ToLower(strings.TrimSpace(input.Username))
					if username == "" {
						role := "panelis"
						if slot.Role == domain.RolePICMapel {
							role = "pic"
						}
						for n := 1; n <= 10000; n++ {
							candidate := fmt.Sprintf("%s.%s.%s%02d", strings.ToLower(p.Code), strings.ToLower(sub.Code), role, n)
							_, findErr := s.store.FindByUsername(tx, candidate)
							if errors.Is(findErr, mongo.ErrNoDocuments) && !seenUsername[candidate] {
								username = candidate
								break
							}
							if findErr != nil && !errors.Is(findErr, mongo.ErrNoDocuments) {
								return findErr
							}
						}
					}
					if !usernamePattern.MatchString(username) {
						return fmt.Errorf("username %s harus 3–80 karakter: huruf kecil, angka, titik, garis bawah, atau tanda hubung", username)
					}
					_, findErr := s.store.FindByUsername(tx, username)
					if findErr == nil || seenUsername[username] {
						return fmt.Errorf("username %s sudah digunakan", username)
					}
					if !errors.Is(findErr, mongo.ErrNoDocuments) {
						return findErr
					}
					hash, err := auth.HashPassword(s.initialPassword)
					if err != nil {
						return err
					}
					u = domain.User{ID: newID("user"), Username: username, PasswordHash: hash, MustChangePassword: true, Active: true, CreatedAt: now}
					slot.UserID = u.ID
				}
				if seenUsername[u.Username] {
					return errors.New("username ganda")
				}
				seenUsername[u.Username] = true
				u.SlotID = slot.ID
				u.Name = slot.Name
				u.Role = slot.Role
				u.SubjectCode = sub.Code
				u.ProjectID = p.ID
				u.ProjectName = p.Name
				u.UpdatedAt = now
				users = append(users, u)
				if slot.Role == domain.RolePICMapel {
					sub.PICUserID = u.ID
				} else {
					sub.PanelistUserIDs = append(sub.PanelistUserIDs, u.ID)
				}
			}
		}
		if err = s.store.ReplaceProjectUsers(tx, p.ID, users); err != nil {
			return err
		}
		p.AccountsGenerated = true
		p.WizardStep = 5
		p.Version++
		p.UpdatedAt = now
		if err = s.store.SaveProject(tx, p); err != nil {
			return err
		}
		result = users
		return nil
	})
	return result, err
}
func (s *Service) SetUserActive(ctx context.Context, id string, active bool) error {
	return s.SetUserActiveAs(ctx, id, active, "")
}
func (s *Service) SetUserActiveAs(ctx context.Context, id string, active bool, actor string) error {
	return s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, err := s.store.FindByID(tx, id)
		if err != nil {
			return err
		}
		if u.Role == domain.RoleAdminSuper || u.Removed {
			return errors.New("hanya peserta yang masih ditugaskan dapat diubah statusnya")
		}
		if err = s.store.TouchProject(tx, u.ProjectID); err != nil {
			return err
		}
		if u.Active != active {
			u.AuthVersion++
		}
		u.Active = active
		u.UpdatedAt = time.Now().UTC()
		if err = s.store.SaveUser(tx, u); err != nil {
			return err
		}
		return s.recordAudit(tx, actor, id, u.ProjectID, "user_active_update")
	})
}
func (s *Service) ResetPassword(ctx context.Context, id string) error {
	return s.ResetPasswordAs(ctx, id, "")
}
func (s *Service) ResetPasswordAs(ctx context.Context, id, actor string) error {
	hash, err := auth.HashPassword(s.initialPassword)
	if err != nil {
		return err
	}
	return s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, err := s.store.FindByID(tx, id)
		if err != nil {
			return err
		}
		if u.Role == domain.RoleAdminSuper || u.Removed {
			return errors.New("reset hanya untuk akun peserta yang masih ditugaskan")
		}
		if err = s.store.TouchProject(tx, u.ProjectID); err != nil {
			return err
		}
		u.PasswordHash = hash
		u.MustChangePassword = true
		u.AuthVersion++
		u.UpdatedAt = time.Now().UTC()
		if err = s.store.SaveUser(tx, u); err != nil {
			return err
		}
		return s.recordAudit(tx, actor, id, u.ProjectID, "password_reset")
	})
}
