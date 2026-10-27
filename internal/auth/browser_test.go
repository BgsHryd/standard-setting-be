package auth

import (
	"context"
	"encoding/json"
	"errors"
	"standard-setting/backend/internal/testutil"
	"strings"
	"testing"
	"time"
)

type browserTestSessions struct {
	fakeSessions
	testutil.BrowserSessions
}

func TestBrowserLoginReplacesPreviousWithoutExposingCredential(t *testing.T) {
	s, _ := securedService(t)
	sessions := &browserTestSessions{}
	s.sessions = sessions
	first, err := s.LoginBrowser(context.Background(), "u", "correct", "ip")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.LoginBrowser(context.Background(), "u", "correct", "ip")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AuthenticateBrowser(context.Background(), first.CookieToken); !errors.Is(err, ErrSessionReplaced) {
		t.Fatalf("old session accepted: %v", err)
	}
	if _, _, err = s.AuthenticateBrowser(context.Background(), second.CookieToken); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(second)
	if strings.Contains(string(raw), second.CookieToken) || strings.Contains(string(raw), "accessToken") || strings.Contains(string(raw), "refreshToken") {
		t.Fatalf("credential exposed in browser JSON: %s", raw)
	}
	if second.SessionID == "" || second.CSRFToken == "" || second.ExpiresAt <= time.Now().UnixMilli() {
		t.Fatalf("missing browser metadata: %s", raw)
	}
	if err = s.LogoutBrowser(context.Background(), first.CookieToken); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AuthenticateBrowser(context.Background(), second.CookieToken); err != nil {
		t.Fatal("stale logout removed new session", err)
	}
}

func TestFailedBrowserLoginKeepsCurrentAndRedisOutageFailsClosed(t *testing.T) {
	s, _ := securedService(t)
	sessions := &browserTestSessions{}
	s.sessions = sessions
	first, err := s.LoginBrowser(context.Background(), "u", "correct", "ip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.LoginBrowser(context.Background(), "u", "wrong", "ip"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
	if _, _, err = s.AuthenticateBrowser(context.Background(), first.CookieToken); err != nil {
		t.Fatal(err)
	}
	sessions.Err = errors.New("private redis address")
	if _, _, err = s.AuthenticateBrowser(context.Background(), first.CookieToken); !errors.Is(err, ErrSecurityUnavailable) {
		t.Fatal("outage not fail closed", err)
	}
}
