package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"standard-setting/backend/internal/domain"
)

type fakeUsers struct{ user domain.User }

func (f *fakeUsers) FindByUsername(_ context.Context, username string) (domain.User, error) {
	if f.user.Username != username {
		return domain.User{}, errors.New("not found")
	}
	return f.user, nil
}

func (f *fakeUsers) FindByID(_ context.Context, id string) (domain.User, error) {
	if f.user.ID != id {
		return domain.User{}, errors.New("not found")
	}
	return f.user, nil
}

func (f *fakeUsers) UpdatePassword(_ context.Context, id, passwordHash string, mustChange bool) error {
	if f.user.ID != id {
		return errors.New("not found")
	}
	f.user.PasswordHash = passwordHash
	f.user.MustChangePassword = mustChange
	return nil
}

type fakeSessions struct{ values map[string]string }

func (f *fakeSessions) Save(_ context.Context, tokenID, userID string, _ time.Duration) error {
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[tokenID] = userID
	return nil
}

func (f *fakeSessions) Consume(_ context.Context, tokenID string) (string, error) {
	userID, ok := f.values[tokenID]
	if !ok {
		return "", errors.New("session not found")
	}
	delete(f.values, tokenID)
	return userID, nil
}

func TestLoginReturnsForcedPasswordChangeState(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("12345*"), bcrypt.MinCost)
	users := &fakeUsers{user: domain.User{ID: "user-1", Username: "panelis.mat", Name: "Panelis Matematika", PasswordHash: string(hash), Role: domain.RolePanelist, Active: true, MustChangePassword: true}}
	service := NewService(users, &fakeSessions{}, "test-secret-at-least-32-characters", 15*time.Minute, 24*time.Hour)

	result, err := service.Login(context.Background(), "panelis.mat", "12345*")
	if err != nil {
		t.Fatalf("unexpected login error: %v", err)
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatal("expected access and refresh token")
	}
	if !result.User.MustChangePassword {
		t.Fatal("expected first login to require password replacement")
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.MinCost)
	users := &fakeUsers{user: domain.User{ID: "user-1", Username: "admin", PasswordHash: string(hash), Role: domain.RoleAdminSuper, Active: true}}
	service := NewService(users, &fakeSessions{}, "test-secret-at-least-32-characters", 15*time.Minute, 24*time.Hour)

	_, err := service.Login(context.Background(), "admin", "wrong")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
}

func TestChangePasswordClearsFirstLoginRequirement(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("12345*"), bcrypt.MinCost)
	users := &fakeUsers{user: domain.User{ID: "user-1", Username: "pic.mat", PasswordHash: string(hash), Role: domain.RolePICMapel, Active: true, MustChangePassword: true}}
	service := NewService(users, &fakeSessions{}, "test-secret-at-least-32-characters", 15*time.Minute, 24*time.Hour)

	err := service.ChangePassword(context.Background(), "user-1", "12345*", "Password-Baru-2026!")
	if err != nil {
		t.Fatalf("unexpected password change error: %v", err)
	}
	if users.user.MustChangePassword {
		t.Fatal("expected mustChangePassword to be cleared")
	}
	if bcrypt.CompareHashAndPassword([]byte(users.user.PasswordHash), []byte("Password-Baru-2026!")) != nil {
		t.Fatal("expected the new password hash to be stored")
	}
}

func TestChangePasswordAcceptsSixCharacters(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("12345*"), bcrypt.MinCost)
	users := &fakeUsers{user: domain.User{ID: "user-1", Username: "panelis.mat", PasswordHash: string(hash), Role: domain.RolePanelist, Active: true, MustChangePassword: true}}
	service := NewService(users, &fakeSessions{}, "test-secret-at-least-32-characters", 15*time.Minute, 24*time.Hour)

	err := service.ChangePassword(context.Background(), "user-1", "12345*", "baru12")
	if err != nil {
		t.Fatalf("expected a six-character password to be accepted, got %v", err)
	}
}

func TestChangePasswordRejectsFewerThanSixCharacters(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("12345*"), bcrypt.MinCost)
	users := &fakeUsers{user: domain.User{ID: "user-1", Username: "panelis.mat", PasswordHash: string(hash), Role: domain.RolePanelist, Active: true, MustChangePassword: true}}
	service := NewService(users, &fakeSessions{}, "test-secret-at-least-32-characters", 15*time.Minute, 24*time.Hour)

	err := service.ChangePassword(context.Background(), "user-1", "12345*", "baru1")
	if !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("expected a password shorter than six characters to be rejected, got %v", err)
	}
}

func TestRefreshConsumesOldTokenAndRotatesSession(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	users := &fakeUsers{user: domain.User{ID: "user-1", Username: "admin", PasswordHash: string(hash), Role: domain.RoleAdminSuper, Active: true}}
	sessions := &fakeSessions{}
	service := NewService(users, sessions, "test-secret-at-least-32-characters", 15*time.Minute, 24*time.Hour)
	login, err := service.Login(context.Background(), "admin", "secret")
	if err != nil {
		t.Fatalf("unexpected login error: %v", err)
	}

	refreshed, err := service.Refresh(context.Background(), login.RefreshToken)
	if err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}
	if refreshed.RefreshToken == login.RefreshToken || refreshed.AccessToken == "" {
		t.Fatal("expected rotated refresh token and a new access token")
	}
	if _, err := service.Refresh(context.Background(), login.RefreshToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expected consumed token rejection, got %v", err)
	}
}

func TestRefreshCannotReviveSessionAfterReset(t *testing.T) {
	hash, _ := HashPassword("oldpass")
	users := &fakeUsers{user: domain.User{ID: "u", Username: "u", PasswordHash: hash, Active: true}}
	s := NewService(users, &fakeSessions{}, "secret", time.Minute, time.Hour)
	login, err := s.Login(context.Background(), "u", "oldpass")
	if err != nil {
		t.Fatal(err)
	}
	users.user.AuthVersion++
	if _, err := s.Refresh(context.Background(), login.RefreshToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("old refresh survived reset")
	}
}

type racingPasswordUsers struct {
	fakeUsers
	afterWrite func()
}

func (f *racingPasswordUsers) UpdatePasswordIfVersion(_ context.Context, id string, version int, hash string, must bool) error {
	if f.user.ID != id || f.user.AuthVersion != version || !f.user.Active {
		return ErrInvalidSession
	}
	f.user.PasswordHash = hash
	f.user.MustChangePassword = must
	f.user.AuthVersion++
	if f.afterWrite != nil {
		f.afterWrite()
	}
	return nil
}
func TestPasswordChangeSessionCannotAcquireResetVersion(t *testing.T) {
	hash, _ := HashPassword("oldpass")
	users := &racingPasswordUsers{fakeUsers: fakeUsers{user: domain.User{ID: "u", Username: "u", PasswordHash: hash, Active: true}}}
	users.afterWrite = func() { users.user.AuthVersion++; users.user.MustChangePassword = true }
	s := NewService(users, &fakeSessions{}, "secret", time.Minute, time.Hour)
	result, err := s.ChangePasswordSession(context.Background(), "u", 0, "oldpass", "newpass")
	if !errors.Is(err, ErrInvalidSession) || result.AccessToken != "" {
		t.Fatal("pre-reset request received a post-reset session")
	}
}
