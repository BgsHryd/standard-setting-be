package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/session"
)

func qaHTTPRedis(t *testing.T) *session.Redis {
	t.Helper()
	bin, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server unavailable")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	_, port, _ := net.SplitHostPort(address)
	l.Close()
	process := exec.Command(bin, "--bind", "127.0.0.1", "--port", port, "--save", "", "--appendonly", "no", "--dir", t.TempDir())
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { process.Process.Kill(); process.Wait() })
	r := session.NewRedis(address, "", 0)
	t.Cleanup(func() { r.Close() })
	for i := 0; i < 50; i++ {
		if r.Ping(context.Background()) == nil {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("isolated Redis unavailable")
	return nil
}

func qaReadState(t *testing.T, response *http.Response) auth.BrowserResult {
	t.Helper()
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	var envelope struct {
		Data auth.BrowserResult `json:"data"`
	}
	if response.StatusCode != 200 || json.Unmarshal(raw, &envelope) != nil {
		t.Fatalf("invalid public response: %d %s", response.StatusCode, raw)
	}
	if len(response.Header.Values("Set-Cookie")) != 0 {
		t.Fatal("activity/session emitted Set-Cookie")
	}
	return envelope.Data
}

func TestQARealRedisHTTPActivityAndReadOnlySession(t *testing.T) {
	r := qaHTTPRedis(t)
	app := cookieAppStore(t, true, r)
	cookie, state := cookieLogin(t, app)
	// fasthttp serializes Max-Age instead of Expires when both are configured.
	// Max-Age is therefore the browser's absolute-lifetime bound on the wire.
	if cookie.MaxAge < 28799 || cookie.MaxAge > 28800 || state.AbsoluteExpiresAt-time.Now().UnixMilli() < 28799000 || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie absolute/security flags wrong: %+v", cookie)
	}
	time.Sleep(15 * time.Millisecond)
	read := qaReadState(t, cookieRequest(t, app, "GET", "/api/v1/auth/session", "", cookie, "", ""))
	if read.ExpiresAt != state.ExpiresAt || read.AbsoluteExpiresAt != state.AbsoluteExpiresAt {
		t.Fatal("GET renewed session")
	}
	renewed := qaReadState(t, activityRequest(t, app, cookie, state.CSRFToken, "https://app.example.test", state.SessionID))
	if renewed.ExpiresAt <= state.ExpiresAt || renewed.AbsoluteExpiresAt != state.AbsoluteExpiresAt || renewed.CSRFToken != state.CSRFToken || renewed.SessionID != state.SessionID {
		t.Fatalf("incorrect renewal %+v", renewed)
	}
	// A newer login has the same user but different binding and credential.
	newCookie, next := cookieLogin(t, app)
	response := activityRequest(t, app, newCookie, state.CSRFToken, "https://app.example.test", state.SessionID)
	response.Body.Close()
	if response.StatusCode != 409 && response.StatusCode != 403 {
		t.Fatalf("stale tab renewed new login: %d", response.StatusCode)
	}
	response = activityRequest(t, app, cookie, state.CSRFToken, "https://app.example.test", state.SessionID)
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 401 || !strings.Contains(string(raw), `"code":"SESSION_REPLACED"`) || !strings.Contains(string(raw), "Anda telah login di perangkat lain") {
		t.Fatalf("replacement lost: %d %s", response.StatusCode, raw)
	}
	if current := qaReadState(t, cookieRequest(t, app, "GET", "/api/v1/auth/session", "", newCookie, "", "")); current.SessionID != next.SessionID {
		t.Fatal("old activity disturbed new login")
	}
}

func TestQARealRedisLegacyPublicMetadataHasFixedAbsoluteDeadline(t *testing.T) {
	r := qaHTTPRedis(t)
	app := cookieAppStore(t, true, r)
	cookie, state := cookieLogin(t, app)
	digest := sha256.Sum256([]byte(cookie.Value))
	now := time.Now()
	rec := session.BrowserSession{ID: hex.EncodeToString(digest[:]), UserID: state.User.ID, AuthVersion: state.AuthVersion, CSRFToken: state.CSRFToken, ExpiresAt: now.Add(time.Minute).UnixMilli()}
	if err := r.CreateBrowserSession(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	public := qaReadState(t, cookieRequest(t, app, "GET", "/api/v1/auth/session", "", cookie, "", ""))
	renewed := qaReadState(t, activityRequest(t, app, cookie, state.CSRFToken, "https://app.example.test", state.SessionID))
	if public.AbsoluteExpiresAt != rec.ExpiresAt || renewed.AbsoluteExpiresAt != rec.ExpiresAt || renewed.ExpiresAt != rec.ExpiresAt {
		t.Fatalf("legacy lifetime extended: %+v %+v", public, renewed)
	}
}
