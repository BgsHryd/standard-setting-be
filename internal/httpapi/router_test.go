package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"standard-setting/backend/internal/testutil"
	"strings"
	"testing"
	"time"

	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/domain"

	"golang.org/x/crypto/bcrypt"
)

type testUsers struct{ user domain.User }

func (t *testUsers) FindByUsername(_ context.Context, username string) (domain.User, error) {
	if username != t.user.Username {
		return domain.User{}, errors.New("not found")
	}
	return t.user, nil
}
func (t *testUsers) FindByID(_ context.Context, id string) (domain.User, error) {
	if id != t.user.ID {
		return domain.User{}, errors.New("not found")
	}
	return t.user, nil
}
func (t *testUsers) UpdatePassword(_ context.Context, id string, hash string, mustChange bool) error {
	if id != t.user.ID {
		return errors.New("not found")
	}
	t.user.PasswordHash, t.user.MustChangePassword = hash, mustChange
	return nil
}

type testSessions struct {
	values map[string]string
	testutil.BrowserSessions
}

func (t *testSessions) Save(_ context.Context, token, userID string, _ time.Duration) error {
	if t.values == nil {
		t.values = map[string]string{}
	}
	t.values[token] = userID
	return nil
}
func (t *testSessions) Consume(_ context.Context, token string) (string, error) {
	userID, ok := t.values[token]
	if !ok {
		return "", errors.New("not found")
	}
	delete(t.values, token)
	return userID, nil
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	users := &testUsers{user: domain.User{ID: "admin-1", Username: "admin", Name: "Admin Super", PasswordHash: string(hash), Role: domain.RoleAdminSuper, Active: true}}
	authService := auth.NewService(users, &testSessions{}, "test-secret-at-least-32-characters", 15*time.Minute, 24*time.Hour)
	return New(Config{Auth: authService, AllowedOrigins: "http://localhost:5176"})
}

func TestHealthReportsReadyApplication(t *testing.T) {
	app := newTestApp(t)
	request := httptest.NewRequest("GET", "/api/v1/health", nil)

	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(response.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Fatalf("expected ok body, got %v", body)
	}
}

func TestLoginRejectsMalformedPayload(t *testing.T) {
	app := newTestApp(t)
	request := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":""}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://localhost:5176")

	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	if response.StatusCode != 422 {
		t.Fatalf("expected status 422, got %d", response.StatusCode)
	}
}

func TestProjectsRequireAccessToken(t *testing.T) {
	app := newTestApp(t)
	request := httptest.NewRequest("GET", "/api/v1/projects", nil)

	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("projects request failed: %v", err)
	}
	if response.StatusCode != 401 {
		t.Fatalf("expected status 401, got %d", response.StatusCode)
	}
}

func TestCORSAllowsLoopbackFrontendOrigin(t *testing.T) {
	app := newTestApp(t)
	request := httptest.NewRequest("OPTIONS", "/api/v1/auth/login", nil)
	request.Header.Set("Origin", "http://127.0.0.1:5176")
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "content-type,authorization")

	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("preflight request failed: %v", err)
	}
	if response.Header.Get("Access-Control-Allow-Origin") != "http://127.0.0.1:5176" {
		t.Fatalf("expected loopback origin to be allowed, got headers %v", response.Header)
	}
}

func TestCORSAllowsPrivateNetworkFrontendOrigin(t *testing.T) {
	app := newTestApp(t)
	request := httptest.NewRequest("OPTIONS", "/api/v1/auth/login", nil)
	request.Header.Set("Origin", "http://192.168.1.14:5176")
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "content-type,authorization")

	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("preflight request failed: %v", err)
	}
	if response.Header.Get("Access-Control-Allow-Origin") != "http://192.168.1.14:5176" {
		t.Fatalf("expected private-network origin to be allowed, got headers %v", response.Header)
	}
}

func TestCORSRejectsUnrelatedOrigin(t *testing.T) {
	app := newTestApp(t)
	request := httptest.NewRequest("OPTIONS", "/api/v1/auth/login", nil)
	request.Header.Set("Origin", "https://untrusted.example")
	request.Header.Set("Access-Control-Request-Method", "POST")

	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("preflight request failed: %v", err)
	}
	if response.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected unrelated origin to be rejected, got %q", response.Header.Get("Access-Control-Allow-Origin"))
	}
}

func attachTestSession(req *http.Request, state auth.BrowserResult) {
	req.AddCookie(&http.Cookie{Name: "ss_session", Value: state.CookieToken})
	req.Header.Set("Origin", "http://localhost:5176")
	req.Header.Set("X-CSRF-Token", state.CSRFToken)
}
