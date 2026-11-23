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

var (
	ErrAccountAccessDenied   = errors.New("Anda tidak memiliki izin untuk mengelola akun ini")
	ErrInvalidManagedAccount = errors.New("data akun tidak valid")
)

type ManagedAccountIdentity struct {
	Name     string `json:"name"`
	Username string `json:"username"`
}

type ManagedAccountInput struct {
	Name     string      `json:"name"`
	Username string      `json:"username"`
	Role     domain.Role `json:"role"`
}

func isParticipantRole(role domain.Role) bool {
	return role == domain.RolePICMapel || role == domain.RolePanelist
}

func isManagedAccountRole(role domain.Role) bool {
	return role == domain.RoleAdmin || role == domain.RoleUserMonitoring
}

func (s *Service) accountManagerRole(ctx context.Context, actor string) (domain.Role, error) {
	if actor == "" {
		return "", ErrAccountAccessDenied
	}
	u, err := s.store.FindByID(ctx, actor)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return "", err
	}
	if err != nil || !u.Active || u.Removed || !u.Role.IsAdministrative() {
		return "", ErrAccountAccessDenied
	}
	return u.Role, nil
}

func (s *Service) authorizeAccountRole(ctx context.Context, actor string, targetRole domain.Role) error {
	role, err := s.accountManagerRole(ctx, actor)
	if err != nil {
		return err
	}
	switch targetRole {
	case domain.RolePICMapel, domain.RolePanelist, domain.RoleUserMonitoring:
		return nil
	case domain.RoleAdmin:
		if role == domain.RoleAdminSuper {
			return nil
		}
	}
	return ErrAccountAccessDenied
}

func (s *Service) ListUsersAs(ctx context.Context, projectID, actor string) ([]domain.User, error) {
	role, err := s.accountManagerRole(ctx, actor)
	if err != nil {
		return nil, err
	}
	users, err := s.store.ListUsers(ctx, projectID)
	if err != nil || role == domain.RoleAdminSuper {
		return users, err
	}
	visible := make([]domain.User, 0, len(users))
	for _, u := range users {
		if !u.Role.IsAdministrative() {
			visible = append(visible, u)
		}
	}
	return visible, nil
}

func (s *Service) CreateManagedAccount(ctx context.Context, input ManagedAccountInput, actor string) (domain.User, error) {
	var result domain.User
	name, username, err := normalizeAccountIdentity(input.Name, input.Username)
	if err != nil {
		return result, ErrInvalidManagedAccount
	}
	if input.Role == domain.RoleAdminSuper {
		return result, ErrAccountAccessDenied
	}
	if !isManagedAccountRole(input.Role) {
		return result, ErrInvalidManagedAccount
	}
	err = s.store.WithTransaction(ctx, func(tx context.Context) error {
		if err := s.authorizeAccountRole(tx, actor, input.Role); err != nil {
			return err
		}
		if err := s.ensureUsernameAvailable(tx, username, ""); err != nil {
			return err
		}
		hash, err := auth.HashPassword(s.initialPassword)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		u := domain.User{
			ID: newID("user"), Name: name, Username: username, Role: input.Role,
			PasswordHash: hash, Active: true, MustChangePassword: true, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.insertAccount(tx, u); err != nil {
			return err
		}
		if err := s.recordAudit(tx, actor, u.ID, "", "user_create"); err != nil {
			return err
		}
		result = u
		return nil
	})
	return result, err
}

func (s *Service) managedAccount(ctx context.Context, id, actor string) (domain.User, error) {
	u, err := s.store.FindByID(ctx, id)
	if err != nil {
		return u, err
	}
	if !isManagedAccountRole(u.Role) {
		return u, ErrAccountAccessDenied
	}
	if err = s.authorizeAccountRole(ctx, actor, u.Role); err != nil {
		return u, err
	}
	if u.Removed {
		return u, errors.New("akun sudah dihapus")
	}
	return u, nil
}

func (s *Service) UpdateManagedAccount(ctx context.Context, id string, input ManagedAccountIdentity, actor string) (domain.User, error) {
	var result domain.User
	name, username, err := normalizeAccountIdentity(input.Name, input.Username)
	if err != nil {
		return result, ErrInvalidManagedAccount
	}
	err = s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, err := s.managedAccount(tx, id, actor)
		if err != nil {
			return err
		}
		if err = s.ensureUsernameAvailable(tx, username, u.ID); err != nil {
			return err
		}
		if u.Username != username {
			u.AuthVersion++
		}
		u.Username, u.Name = username, name
		u.UpdatedAt = time.Now().UTC()
		if err = s.store.SaveUser(tx, u); err != nil {
			return err
		}
		if err = s.recordAudit(tx, actor, u.ID, u.ProjectID, "user_identity_update"); err != nil {
			return err
		}
		result = u
		return nil
	})
	return result, err
}

func (s *Service) RemoveManagedAccount(ctx context.Context, id, actor string) error {
	return s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, err := s.managedAccount(tx, id, actor)
		if err != nil {
			return err
		}
		u.Active = false
		u.Removed = true
		u.AuthVersion++
		u.UpdatedAt = time.Now().UTC()
		if err = s.store.SaveUser(tx, u); err != nil {
			return err
		}
		return s.recordAudit(tx, actor, u.ID, u.ProjectID, "user_remove")
	})
}

func (s *Service) SetMonitoringProject(ctx context.Context, id, projectID, actor string) (domain.User, error) {
	var result domain.User
	projectID = strings.TrimSpace(projectID)
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, err := s.managedAccount(tx, id, actor)
		if err != nil {
			return err
		}
		if u.Role != domain.RoleUserMonitoring {
			return ErrAccountAccessDenied
		}
		if u.ProjectID == projectID {
			result = u
			return nil
		}
		auditProjectID := u.ProjectID
		u.ProjectID, u.ProjectName = "", ""
		if projectID != "" {
			project, err := s.store.GetProject(tx, projectID)
			if err != nil {
				return err
			}
			if project.Status != domain.ProjectActive {
				return errors.New("penugasan monitoring hanya untuk proyek aktif")
			}
			if err = s.store.TouchProject(tx, projectID); err != nil {
				return err
			}
			u.ProjectID, u.ProjectName = project.ID, project.Name
			auditProjectID = project.ID
		}
		u.AuthVersion++
		u.UpdatedAt = time.Now().UTC()
		if err = s.store.SaveUser(tx, u); err != nil {
			return err
		}
		if err = s.recordAudit(tx, actor, u.ID, auditProjectID, "user_monitoring_project_update"); err != nil {
			return err
		}
		result = u
		return nil
	})
	return result, err
}
