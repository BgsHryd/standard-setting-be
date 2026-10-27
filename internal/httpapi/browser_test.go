package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"golang.org/x/crypto/bcrypt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/session"
	"strings"
	"testing"
	"time"
)

type cookieTestStore struct{ testSessions }

func cookieApp(t *testing.T, secure bool) *App {
	return cookieAppStore(t, secure, &cookieTestStore{})
}
func cookieAppStore(t *testing.T, secure bool, sessions auth.SessionRepository) *App {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("cookie-password"), bcrypt.MinCost)
	users := &testUsers{user: domain.User{ID: "cookie-user", Username: "cookie-user", PasswordHash: string(hash), Active: true, Role: domain.RoleAdminSuper}}
	service := auth.NewService(users, sessions, "qa-secret", 15*time.Minute, time.Hour)
	return New(Config{Auth: service, AllowedOrigins: "https://app.example.test", SecureCookies: secure})
}

func TestRedisSessionReplacementNotifiesAlreadyConnectedBrowser(t *testing.T) {
	bin, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server unavailable")
	}
	port, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := port.Addr().String()
	_, number, _ := net.SplitHostPort(address)
	_ = port.Close()
	process := exec.Command(bin, "--bind", "127.0.0.1", "--port", number, "--save", "", "--appendonly", "no", "--dir", t.TempDir())
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = process.Process.Kill(); _ = process.Wait() })
	store := session.NewRedis(address, "", 0)
	t.Cleanup(func() { _ = store.Close() })
	for i := 0; i < 50; i++ {
		if store.Ping(context.Background()) == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	app := cookieAppStore(t, false, store)
	first, _ := cookieLogin(t, app)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(listener) }()
	t.Cleanup(func() { _ = app.ShutdownWithTimeout(time.Second) })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+listener.Addr().String()+"/api/v1/auth/events", nil)
	req.AddCookie(first)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("SSE status=%d", res.StatusCode)
	}
	cookieLogin(t, app)
	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), `"code":"SESSION_REPLACED"`) {
			return
		}
	}
	t.Fatalf("replacement event missing: %v", scanner.Err())
}

func cookieRequest(t *testing.T, app *App, method, path, body string, cookie *http.Cookie, csrf, origin string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func cookieLogin(t *testing.T, app *App) (*http.Cookie, auth.BrowserResult) {
	t.Helper()
	res := cookieRequest(t, app, "POST", "/api/v1/auth/login", `{"username":"cookie-user","password":"cookie-password"}`, nil, "", "https://app.example.test")
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("login: %d %s", res.StatusCode, data)
	}
	cookies := res.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("session cookie absent: %v", cookies)
	}
	if strings.Contains(string(data), cookies[0].Value) || strings.Contains(string(data), "accessToken") || strings.Contains(string(data), "refreshToken") {
		t.Fatalf("credential exposed: %s", data)
	}
	var payload struct {
		Data auth.BrowserResult `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	return cookies[0], payload.Data
}

func TestCookieSessionFlagsReplacementCSRFAndLogout(t *testing.T) {
	app := cookieApp(t, true)
	first, state := cookieLogin(t, app)
	if first.Name != "__Host-ss_session" || !first.Secure || !first.HttpOnly || first.SameSite != http.SameSiteLaxMode || first.Path != "/" || first.Domain != "" || first.MaxAge < 899 || first.MaxAge > 900 {
		t.Fatalf("unsafe cookie: %+v", first)
	}
	for _, tc := range []struct{ origin, csrf string }{{"https://evil.example", state.CSRFToken}, {"", state.CSRFToken}, {"https://app.example.test", "wrong"}} {
		res := cookieRequest(t, app, "POST", "/api/v1/auth/logout", "{}", first, tc.csrf, tc.origin)
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatalf("unsafe mutation accepted: %d", res.StatusCode)
		}
	}
	second, next := cookieLogin(t, app)
	res := cookieRequest(t, app, "GET", "/api/v1/auth/session", "", first, "", "")
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 401 || !strings.Contains(string(data), `"code":"SESSION_REPLACED"`) || !strings.Contains(string(data), "Anda telah login di perangkat lain") {
		t.Fatalf("replacement not communicated: %d %s", res.StatusCode, data)
	}
	if len(res.Cookies()) != 0 {
		t.Fatal("stale response changes cookie")
	}
	res = cookieRequest(t, app, "POST", "/api/v1/auth/logout", "{}", first, state.CSRFToken, "https://app.example.test")
	res.Body.Close()
	res = cookieRequest(t, app, "GET", "/api/v1/me", "", second, "", "")
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal("old logout invalidated new session")
	}
	res = cookieRequest(t, app, "POST", "/api/v1/auth/logout", "{}", second, next.CSRFToken, "https://app.example.test")
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("logout: %d", res.StatusCode)
	}
	if len(res.Cookies()) != 0 {
		t.Fatal("logout response can overwrite a newer shared browser cookie")
	}
	res = cookieRequest(t, app, "GET", "/api/v1/me", "", second, "", "")
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("logged out cookie accepted")
	}
}

func TestCookieLoginRejectsCrossOriginAndBearerCannotBypass(t *testing.T) {
	app := cookieApp(t, false)
	res := cookieRequest(t, app, "POST", "/api/v1/auth/login", `{"username":"cookie-user","password":"cookie-password"}`, nil, "", "https://evil.example")
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("cross origin login accepted: %d", res.StatusCode)
	}
	cookie, _ := cookieLogin(t, app)
	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+cookie.Value)
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("bearer bypass")
	}
	res = cookieRequest(t, app, "POST", "/api/v1/auth/refresh", `{"refreshToken":"legacy"}`, cookie, "", "https://app.example.test")
	res.Body.Close()
	if res.StatusCode == 200 {
		t.Fatal("legacy refresh still issues sessions")
	}
}
