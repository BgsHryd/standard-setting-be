package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/domain"
)

func renewalApp(t *testing.T, idle, absolute time.Duration) (*App, *cookieTestStore) {
	t.Helper()
	store := &cookieTestStore{}
	hash, _ := bcrypt.GenerateFromPassword([]byte("cookie-password"), bcrypt.MinCost)
	users := &testUsers{user: domain.User{ID: "cookie-user", Username: "cookie-user", PasswordHash: string(hash), Active: true, Role: domain.RoleAdminSuper}}
	svc := auth.NewService(users, store, "secret", time.Minute, time.Hour)
	if err := svc.SetBrowserSessionTTL(idle, absolute); err != nil {
		t.Fatal(err)
	}
	return New(Config{Auth: svc, AllowedOrigins: "https://app.example.test", SecureCookies: true}), store
}
func activityRequest(t *testing.T, app *App, cookie *http.Cookie, csrf, origin, id string) *http.Response {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/auth/activity", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("X-Session-ID", id)
	req.AddCookie(cookie)
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestBrowserActivityHTTPProtectionsMetadataAndCookie(t *testing.T) {
	app, store := renewalApp(t, 400*time.Millisecond, 2*time.Second)
	cookie, state := cookieLogin(t, app)
	if cookie.MaxAge != 2 {
		t.Fatalf("cookie not absolute: %+v %+v", cookie, state)
	}
	for _, tc := range []struct {
		name, csrf, origin, id string
		status                 int
	}{
		{"missing-id", state.CSRFToken, "https://app.example.test", "", 409},
		{"stale-id", state.CSRFToken, "https://app.example.test", "stale", 409},
		{"csrf", "bad", "https://app.example.test", state.SessionID, 403},
		{"origin", state.CSRFToken, "https://evil.example", state.SessionID, 403},
		{"missing-origin", state.CSRFToken, "", state.SessionID, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := activityRequest(t, app, cookie, tc.csrf, tc.origin, tc.id)
			defer res.Body.Close()
			if res.StatusCode != tc.status {
				t.Fatalf("unsafe activity status=%d want=%d", res.StatusCode, tc.status)
			}
		})
	}
	time.Sleep(20 * time.Millisecond)
	res := activityRequest(t, app, cookie, state.CSRFToken, "https://app.example.test", state.SessionID)
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	var payload struct {
		Data auth.BrowserResult `json:"data"`
	}
	json.Unmarshal(raw, &payload)
	if res.StatusCode != 200 || payload.Data.ExpiresAt <= state.ExpiresAt || payload.Data.AbsoluteExpiresAt != state.AbsoluteExpiresAt || payload.Data.SessionID != state.SessionID || payload.Data.User.ID != state.User.ID || len(res.Cookies()) != 0 || strings.Contains(string(raw), cookie.Value) {
		t.Fatalf("activity contract: %d %s cookies=%v", res.StatusCode, raw, res.Cookies())
	}
	store.Err = errors.New("redis unavailable")
	res = activityRequest(t, app, cookie, state.CSRFToken, "https://app.example.test", state.SessionID)
	res.Body.Close()
	if res.StatusCode != 503 || len(res.Cookies()) != 0 {
		t.Fatalf("outage status=%d", res.StatusCode)
	}
}

func TestBrowserSSERechecksRenewedDeadlineAndExpiresWithoutHeartbeatRenewal(t *testing.T) {
	// The new idle deadline is less than the 15s heartbeat; SSE must reset its
	// timer after observing a renewal at its original idle deadline.
	app, _ := renewalApp(t, 250*time.Millisecond, 2*time.Second)
	cookie, state := cookieLogin(t, app)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(listener) }()
	t.Cleanup(func() { _ = app.ShutdownWithTimeout(time.Second) })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+listener.Addr().String()+"/api/v1/auth/events", nil)
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	time.Sleep(120 * time.Millisecond)
	activity := activityRequest(t, app, cookie, state.CSRFToken, "https://app.example.test", state.SessionID)
	raw, _ := io.ReadAll(activity.Body)
	activity.Body.Close()
	var payload struct {
		Data auth.BrowserResult `json:"data"`
	}
	json.Unmarshal(raw, &payload)
	if activity.StatusCode != 200 {
		t.Fatalf("renewal: %d %s", activity.StatusCode, raw)
	}
	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), `"code":"SESSION_EXPIRED"`) {
			if time.Now().UnixMilli() < payload.Data.ExpiresAt {
				t.Fatal("SSE closed at original deadline")
			}
			return
		}
	}
	t.Fatalf("renewed idle expiry event missing: %v", scanner.Err())
}
