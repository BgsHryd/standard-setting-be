package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"net/http/httptest"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/domain"
	"strings"
	"testing"
	"time"
)

type routeLimiter struct {
	retry time.Duration
	err   error
	ip    string
}

func (l *routeLimiter) ReserveLogin(_ context.Context, _, ip string) (time.Duration, error) {
	l.ip = ip
	return l.retry, l.err
}
func (l *routeLimiter) ResetLogin(context.Context, string) error { return nil }
func TestLoginRateLimitRetryAndUnavailableContract(t *testing.T) {
	users := &testUsers{}
	s := auth.NewService(users, &testSessions{}, "secret", time.Minute, time.Hour)
	limiter := &routeLimiter{retry: 6200 * time.Millisecond}
	s.SetLoginLimiter(limiter)
	app := New(Config{Auth: s, AllowedOrigins: "http://localhost:5176"})
	for _, tc := range []struct {
		outage bool
		status int
	}{{false, 429}, {true, 503}} {
		if tc.outage {
			limiter.retry = 0
			limiter.err = errors.New("private redis address must not leak")
		}
		req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"u","password":"secret"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:5176")
		req.Header.Set("X-Forwarded-For", "203.0.113.5")
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != tc.status {
			t.Fatalf("status=%d want%d", res.StatusCode, tc.status)
		}
		if !tc.outage && res.Header.Get("Retry-After") != "7" {
			t.Fatalf("retry=%q", res.Header.Get("Retry-After"))
		}
		var body map[string]any
		json.NewDecoder(res.Body).Decode(&body)
		if strings.Contains(body["message"].(string), "private redis") {
			t.Fatal("redis details leaked")
		}
		if limiter.ip == "203.0.113.5" {
			t.Fatal("untrusted XFF changed limiter IP")
		}
	}
}

type auditRouteUsers struct{ testUsers }

func (*auditRouteUsers) ListAudit(context.Context, string, string, string) ([]domain.AuditEvent, error) {
	return []domain.AuditEvent{{ID: "event", Action: "auth.password_change", Outcome: "success", ActorID: "u"}}, nil
}
func TestAuditRouteAdminOnly(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	for _, role := range []domain.Role{domain.RoleAdminSuper, domain.RolePanelist} {
		users := &auditRouteUsers{testUsers{domain.User{ID: "u", Username: "u", Name: "Auditor", PasswordHash: string(hash), Active: true, Role: role}}}
		s := auth.NewService(users, &testSessions{}, "secret", time.Minute, time.Hour)
		login, err := s.LoginBrowser(context.Background(), "u", "secret", "")
		if err != nil {
			t.Fatal(err)
		}
		app := New(Config{Auth: s, AllowedOrigins: "http://localhost:5176"})
		req := httptest.NewRequest("GET", "/api/v1/audit-events", nil)
		attachTestSession(req, login)
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		want := 403
		if role == domain.RoleAdminSuper {
			want = 200
		}
		if res.StatusCode != want {
			t.Fatalf("role %s status=%d want%d", role, res.StatusCode, want)
		}
		if want == 200 {
			var body struct {
				Data []auditEventView `json:"data"`
			}
			json.NewDecoder(res.Body).Decode(&body)
			if len(body.Data) != 1 || body.Data[0].Action != "auth.password_change" || body.Data[0].ActorName != "Auditor" || body.Data[0].ActorUsername != "u" {
				t.Fatalf("audit not exposed %+v", body)
			}
		}
	}
}

type requestAuditUsers struct {
	testUsers
	event domain.AuditEvent
}

func (u *requestAuditUsers) RecordAudit(_ context.Context, e domain.AuditEvent) error {
	u.event = e
	return nil
}
func TestLoginAuditUsesServerGeneratedRequestID(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	users := &requestAuditUsers{testUsers: testUsers{domain.User{ID: "u", Username: "u", PasswordHash: string(hash), Active: true}}}
	s := auth.NewService(users, &testSessions{}, "secret", time.Minute, time.Hour)
	app := New(Config{Auth: s, AllowedOrigins: "http://localhost:5176"})
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"u","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:5176")
	req.Header.Set("X-Request-ID", "untrusted-input")
	res, err := app.Test(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatal("login failed", err)
	}
	id := res.Header.Get("X-Request-ID")
	if id == "" || id == "untrusted-input" || users.event.RequestID != id {
		t.Fatalf("request correlation absent or forged: %s %+v", id, users.event)
	}
}

type passwordVersionUsers struct{ testUsers }

func (u *passwordVersionUsers) UpdatePasswordIfVersion(_ context.Context, id string, version int, hash string, must bool) error {
	if id != u.user.ID || version != u.user.AuthVersion {
		return auth.ErrInvalidSession
	}
	u.user.AuthVersion++
	u.user.PasswordHash = hash
	u.user.MustChangePassword = must
	return nil
}
func TestPasswordChangeRejectsPriorCookieSession(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	users := &passwordVersionUsers{testUsers{domain.User{ID: "u", Username: "u", PasswordHash: string(hash), Active: true, Role: domain.RoleAdminSuper}}}
	s := auth.NewService(users, &testSessions{}, "secret", time.Minute, time.Hour)
	old, err := s.LoginBrowser(context.Background(), "u", "old-password", "")
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.ChangeBrowserPassword(context.Background(), old.CookieToken, "old-password", "new-password")
	if err != nil {
		t.Fatal(err)
	}
	app := New(Config{Auth: s, AllowedOrigins: "http://localhost:5176"})
	for _, tc := range []struct {
		token  auth.BrowserResult
		status int
	}{{old, 401}, {current, 200}} {
		req := httptest.NewRequest("GET", "/api/v1/me", nil)
		attachTestSession(req, tc.token)
		res, err := app.Test(req)
		if err != nil || res.StatusCode != tc.status {
			t.Fatalf("access version status=%d expected%d err=%v", res.StatusCode, tc.status, err)
		}
	}
	if _, _, err = s.AuthenticateBrowser(context.Background(), old.CookieToken); err == nil {
		t.Fatal("old refresh revived session", err)
	}
}
