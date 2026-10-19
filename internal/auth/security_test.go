package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"standard-setting/backend/internal/domain"
	"strings"
	"testing"
	"time"
)

type securityUsers struct {
	fakeUsers
	events   []domain.AuditEvent
	contact  bool
	auditErr error
}

func (u *securityUsers) RecordAudit(_ context.Context, e domain.AuditEvent) error {
	u.events = append(u.events, e)
	return u.auditErr
}
func (u *securityUsers) RequiresContact(context.Context, string) (bool, error) { return u.contact, nil }

type securityLimiter struct {
	wait  time.Duration
	err   error
	reset bool
}

func (l *securityLimiter) ReserveLogin(context.Context, string, string) (time.Duration, error) {
	return l.wait, l.err
}
func (l *securityLimiter) ResetLogin(context.Context, string) error { l.reset = true; return l.err }
func securedService(t *testing.T) (*Service, *securityUsers) {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.MinCost)
	u := &securityUsers{fakeUsers: fakeUsers{domain.User{ID: "u", Username: "u", PasswordHash: string(hash), Active: true, Role: domain.RolePanelist}}, contact: true}
	return NewService(u, &fakeSessions{}, "secret", time.Minute, time.Hour), u
}
func TestLoginAuditsWithoutSecretsAndReadsLiveContact(t *testing.T) {
	s, u := securedService(t)
	l := &securityLimiter{}
	s.SetLoginLimiter(l)
	ctx := domain.WithAuditRequestID(context.Background(), "req-1")
	r, err := s.LoginFrom(ctx, "u", "correct", "192.0.2.1")
	if err != nil || !r.User.ContactRequired || !l.reset {
		t.Fatalf("result=%+v reset=%v err=%v", r.User, l.reset, err)
	}
	if len(u.events) != 1 || u.events[0].Action != "auth.login" || u.events[0].Outcome != "success" || u.events[0].RequestID != "req-1" || u.events[0].ActorID != "u" {
		t.Fatalf("bad audit: %+v", u.events)
	}
	b, _ := json.Marshal(u.events)
	for _, secret := range []string{"correct", u.user.PasswordHash, r.AccessToken, r.RefreshToken} {
		if strings.Contains(string(b), secret) {
			t.Fatal("audit contains secret")
		}
	}
	u.contact = false
	r, err = s.Refresh(ctx, r.RefreshToken)
	if err != nil || r.User.ContactRequired {
		t.Fatalf("refresh contact stale: %+v %v", r.User, err)
	}
}
func TestLoginLimitsBeforeCredentialsAndFailsClosed(t *testing.T) {
	s, u := securedService(t)
	l := &securityLimiter{wait: time.Minute}
	s.SetLoginLimiter(l)
	_, err := s.LoginFrom(context.Background(), "u", "correct", "ip")
	var limited *RateLimitError
	if !errors.Is(err, ErrRateLimited) || !errors.As(err, &limited) || limited.RetryAfter != time.Minute {
		t.Fatalf("expected rate limit got %v", err)
	}
	if l.reset {
		t.Fatal("blocked login reset counters")
	}
	l.wait = 0
	l.err = errors.New("redis offline")
	_, err = s.LoginFrom(context.Background(), "u", "correct", "ip")
	if !errors.Is(err, ErrSecurityUnavailable) {
		t.Fatalf("redis failure bypassed: %v", err)
	}
	u.auditErr = errors.New("audit offline")
	l.err = nil
	_, err = s.LoginFrom(context.Background(), "u", "correct", "ip")
	if !errors.Is(err, ErrSecurityUnavailable) {
		t.Fatalf("audit failure bypassed: %v", err)
	}
}
func TestFailedLoginAndPasswordChangeAreAudited(t *testing.T) {
	s, u := securedService(t)
	_, err := s.Login(context.Background(), "u", "bad")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
	if len(u.events) != 1 || u.events[0].Outcome != "failure" {
		t.Fatalf("missing failed login: %+v", u.events)
	}
	if err := s.ChangePassword(context.Background(), "u", "correct", "changed"); err != nil {
		t.Fatal(err)
	}
	if len(u.events) != 2 || u.events[1].Action != "auth.password_change" || u.events[1].Outcome != "success" {
		t.Fatalf("missing change audit %+v", u.events)
	}
}

type unavailableSessions struct{ err error }

func (s unavailableSessions) Save(context.Context, string, string, time.Duration) error { return s.err }
func (s unavailableSessions) Consume(context.Context, string) (string, error)           { return "", s.err }
func TestRefreshDistinguishesConsumedTokenFromSecurityOutage(t *testing.T) {
	s, _ := securedService(t)
	s.SetLoginLimiter(&securityLimiter{})
	s.sessions = unavailableSessions{err: fmt.Errorf("consume session: %w", redis.Nil)}
	if _, err := s.Refresh(context.Background(), "replayed"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("consumed token must be401: %v", err)
	}
	s.sessions = unavailableSessions{err: errors.New("redis unavailable")}
	if _, err := s.Refresh(context.Background(), "token"); !errors.Is(err, ErrSecurityUnavailable) {
		t.Fatalf("outage must be503: %v", err)
	}
}
