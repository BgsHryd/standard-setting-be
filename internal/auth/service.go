package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"standard-setting/backend/internal/domain"
)

var (
	ErrRateLimited         = errors.New("terlalu banyak percobaan login")
	ErrSecurityUnavailable = errors.New("layanan keamanan sementara tidak tersedia")
	ErrInvalidCredentials  = errors.New("username atau password tidak sesuai")
	ErrInvalidSession      = errors.New("sesi tidak valid")
	ErrWeakPassword        = errors.New("password baru minimal 6 karakter")
)

type UserRepository interface {
	FindByUsername(context.Context, string) (domain.User, error)
	FindByID(context.Context, string) (domain.User, error)
	UpdatePassword(context.Context, string, string, bool) error
}

type SessionRepository interface {
	Save(context.Context, string, string, time.Duration) error
	Consume(context.Context, string) (string, error)
}

type LoginLimiter interface {
	ReserveLogin(context.Context, string, string) (time.Duration, error)
	ResetLogin(context.Context, string) error
}
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string { return ErrRateLimited.Error() }
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

type Service struct {
	limiter    LoginLimiter
	users      UserRepository
	sessions   SessionRepository
	jwtSecret  []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

type UserView struct {
	ID                 string      `json:"id"`
	Username           string      `json:"username"`
	Name               string      `json:"name"`
	Role               domain.Role `json:"role"`
	ProjectID          string      `json:"projectId,omitempty"`
	ProjectName        string      `json:"projectName,omitempty"`
	SubjectCode        string      `json:"subjectCode,omitempty"`
	MustChangePassword bool        `json:"mustChangePassword"`
	ContactRequired    bool        `json:"contactRequired"`
}

type Result struct {
	AccessToken  string   `json:"accessToken"`
	RefreshToken string   `json:"refreshToken"`
	ExpiresIn    int64    `json:"expiresIn"`
	User         UserView `json:"user"`
}

func NewService(users UserRepository, sessions SessionRepository, jwtSecret string, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{users: users, sessions: sessions, jwtSecret: []byte(jwtSecret), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

// SetLoginLimiter configures shared rate limiting before the service begins serving requests.
func (s *Service) SetLoginLimiter(l LoginLimiter) { s.limiter = l }
func (s *Service) Login(ctx context.Context, username, password string) (Result, error) {
	return s.LoginFrom(ctx, username, password, "")
}
func (s *Service) LoginFrom(ctx context.Context, username, password, ip string) (Result, error) {
	user, err := s.loginUser(ctx, username, password, ip)
	if err != nil {
		return Result{}, err
	}
	result, err := s.createSession(ctx, user)
	if err != nil {
		if auditErr := s.audit(ctx, user, "auth.login", "failure"); auditErr != nil {
			return Result{}, auditErr
		}
		return Result{}, err
	}
	if err := s.audit(ctx, user, "auth.login", "success"); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (s *Service) loginUser(ctx context.Context, username, password, ip string) (domain.User, error) {
	username = strings.TrimSpace(username)
	if s.limiter != nil {
		retry, err := s.limiter.ReserveLogin(ctx, strings.ToLower(username), ip)
		if err != nil {
			return domain.User{}, ErrSecurityUnavailable
		}
		if retry > 0 {
			if err := s.audit(ctx, domain.User{}, "auth.login", "rate_limited"); err != nil {
				return domain.User{}, err
			}
			return domain.User{}, &RateLimitError{RetryAfter: retry}
		}
	}
	user, err := s.users.FindByUsername(ctx, username)
	if err != nil || !user.Active || user.Removed || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		if err := s.audit(ctx, user, "auth.login", "failure"); err != nil {
			return domain.User{}, err
		}
		return domain.User{}, ErrInvalidCredentials
	}
	if s.limiter != nil {
		if err := s.limiter.ResetLogin(ctx, strings.ToLower(username)); err != nil {
			return domain.User{}, ErrSecurityUnavailable
		}
	}
	if err := s.checkLoginPeriod(ctx, user, time.Now().UTC()); err != nil {
		if auditErr := s.audit(ctx, user, "auth.login", "failure"); auditErr != nil {
			return domain.User{}, auditErr
		}
		return domain.User{}, err
	}
	if tracker, ok := s.users.(interface {
		UpdateLastLogin(context.Context, string, time.Time) error
	}); ok {
		if err := tracker.UpdateLastLogin(ctx, user.ID, time.Now().UTC()); err != nil {
			return domain.User{}, err
		}
	}
	return user, nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (Result, error) {
	encoded, err := s.sessions.Consume(ctx, refreshToken)
	if err != nil {
		if s.limiter != nil && !errors.Is(err, redis.Nil) {
			return Result{}, ErrSecurityUnavailable
		}
		return Result{}, ErrInvalidSession
	}
	var binding struct {
		ID      string
		Version int
	}
	if json.Unmarshal([]byte(encoded), &binding) != nil || binding.ID == "" {
		return Result{}, ErrInvalidSession
	}
	user, err := s.users.FindByID(ctx, binding.ID)
	if err != nil || !user.Active || user.Removed || user.AuthVersion != binding.Version {
		return Result{}, ErrInvalidSession
	}
	return s.createSession(ctx, user)
}

func (s *Service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	_, err := s.changePassword(ctx, userID, nil, currentPassword, newPassword)
	return err
}
func (s *Service) ChangePasswordSession(ctx context.Context, userID string, expected int, currentPassword, newPassword string) (Result, error) {
	version, err := s.changePassword(ctx, userID, &expected, currentPassword, newPassword)
	if err != nil {
		return Result{}, err
	}
	user, err := s.CurrentUser(ctx, userID)
	if err != nil || user.AuthVersion != version {
		return Result{}, ErrInvalidSession
	}
	return s.createSession(ctx, user)
}
func (s *Service) changePassword(ctx context.Context, userID string, expected *int, currentPassword, newPassword string) (int, error) {
	if tx, ok := s.users.(interface {
		WithTransaction(context.Context, func(context.Context) error) error
	}); ok {
		var version int
		var operationErr error
		err := tx.WithTransaction(ctx, func(sc context.Context) error {
			version, operationErr = s.changePasswordAndAudit(sc, userID, expected, currentPassword, newPassword)
			if errors.Is(operationErr, ErrSecurityUnavailable) {
				return operationErr
			}
			return nil // Persist failed-attempt audits too; no password write occurred in those branches.
		})
		if err != nil {
			return 0, err
		}
		return version, operationErr
	}
	return s.changePasswordAndAudit(ctx, userID, expected, currentPassword, newPassword)
}
func (s *Service) changePasswordAndAudit(ctx context.Context, userID string, expected *int, currentPassword, newPassword string) (version int, resultErr error) {
	auditUser := domain.User{ID: userID}
	defer func() {
		outcome := "success"
		if resultErr != nil {
			outcome = "failure"
		}
		if err := s.audit(ctx, auditUser, "auth.password_change", outcome); err != nil {
			resultErr = err
		}
	}()
	if len(newPassword) < 6 {
		return 0, ErrWeakPassword
	}
	user, err := s.users.FindByID(ctx, userID)
	if err != nil || !user.Active || user.Removed {
		return 0, ErrInvalidSession
	}
	auditUser = user
	if expected != nil && user.AuthVersion != *expected {
		return 0, ErrInvalidSession
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)) != nil {
		return 0, ErrInvalidCredentials
	}
	if currentPassword == newPassword {
		return 0, fmt.Errorf("password baru harus berbeda")
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return 0, err
	}
	if atomic, ok := s.users.(interface {
		UpdatePasswordIfVersion(context.Context, string, int, string, bool) error
	}); ok {
		if err := atomic.UpdatePasswordIfVersion(ctx, userID, user.AuthVersion, hash, false); err != nil {
			return 0, ErrInvalidSession
		}
		return user.AuthVersion + 1, nil
	}
	// Compatibility for in-memory repositories; production Mongo uses the conditional update above.
	return user.AuthVersion, s.users.UpdatePassword(ctx, userID, hash, false)
}

func (s *Service) CurrentUser(ctx context.Context, userID string) (domain.User, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil || !user.Active || user.Removed {
		return domain.User{}, ErrInvalidSession
	}
	return user, nil
}

func (s *Service) SessionForUser(ctx context.Context, userID string) (Result, error) {
	user, err := s.CurrentUser(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	return s.createSession(ctx, user)
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

func (s *Service) createSession(ctx context.Context, user domain.User) (Result, error) {
	view := userView(user)
	if contacts, ok := s.users.(interface {
		RequiresContact(context.Context, string) (bool, error)
	}); ok {
		required, err := contacts.RequiresContact(ctx, user.ID)
		if err != nil {
			return Result{}, err
		}
		view.ContactRequired = required
	}
	access, err := s.issueAccessToken(user)
	if err != nil {
		return Result{}, err
	}
	refresh, err := randomToken(32)
	if err != nil {
		return Result{}, err
	}
	if err := s.sessions.Save(ctx, refresh, sessionBinding(user), s.refreshTTL); err != nil {
		if s.limiter != nil {
			return Result{}, ErrSecurityUnavailable
		}
		return Result{}, err
	}
	return Result{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(s.accessTTL.Seconds()),
		User:         view,
	}, nil
}

func userView(user domain.User) UserView {
	return UserView{
		ID: user.ID, Username: user.Username, Name: user.Name, Role: user.Role,
		ProjectID: user.ProjectID, ProjectName: user.ProjectName, SubjectCode: user.SubjectCode,
		MustChangePassword: user.MustChangePassword,
	}
}

func sessionBinding(user domain.User) string {
	b, _ := json.Marshal(struct {
		ID      string
		Version int
	}{user.ID, user.AuthVersion})
	return string(b)
}

func (s *Service) audit(ctx context.Context, user domain.User, action, outcome string) error {
	if recorder, ok := s.users.(interface {
		RecordAudit(context.Context, domain.AuditEvent) error
	}); ok {
		id, err := randomToken(16)
		if err != nil {
			return ErrSecurityUnavailable
		}
		requestID := domain.AuditRequestID(ctx)
		if requestID == "" {
			requestID = id
		}
		if err := recorder.RecordAudit(ctx, domain.AuditEvent{ID: id, ActorID: user.ID, TargetID: user.ID, ProjectID: user.ProjectID, Action: action, Outcome: outcome, RequestID: requestID, CreatedAt: time.Now().UTC()}); err != nil {
			return ErrSecurityUnavailable
		}
	}
	return nil
}
func (s *Service) ListAudit(ctx context.Context, projectID, search, action string) ([]domain.AuditEvent, error) {
	if reader, ok := s.users.(interface {
		ListAudit(context.Context, string, string, string) ([]domain.AuditEvent, error)
	}); ok {
		return reader.ListAudit(ctx, projectID, search, action)
	}
	return nil, ErrSecurityUnavailable
}

// AuditUsers resolves current names for the authorized audit response. Event history retains immutable identifiers.
func (s *Service) AuditUsers(ctx context.Context, ids []string) (map[string]UserView, error) {
	result := map[string]UserView{}
	if reader, ok := s.users.(interface {
		FindAuditUsers(context.Context, []string) ([]domain.User, error)
	}); ok {
		rows, err := reader.FindAuditUsers(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, u := range rows {
			result[u.ID] = userView(u)
		}
		return result, nil
	}
	for _, id := range ids {
		if u, err := s.users.FindByID(ctx, id); err == nil {
			result[id] = userView(u)
		}
	}
	return result, nil
}
