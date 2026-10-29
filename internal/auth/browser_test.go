package auth

import (
	"context"
	"encoding/json"
	"errors"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/session"
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

func TestBrowserLoginUsesIndependentIdleAndAbsolutePolicy(t *testing.T) {
	s, _ := securedService(t)
	s.sessions = &browserTestSessions{}
	before := time.Now().UnixMilli()
	result, err := s.LoginBrowser(context.Background(), "u", "correct", "ip")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	var metadata map[string]any
	json.Unmarshal(raw, &metadata)
	absolute, _ := metadata["absoluteExpiresAt"].(float64)
	created, _ := metadata["createdAt"].(float64)
	if result.ExpiresAt < before+(30*time.Minute).Milliseconds() || result.ExpiresAt > time.Now().Add(30*time.Minute).UnixMilli() || int64(absolute) < before+(8*time.Hour).Milliseconds() || int64(created) < before {
		t.Fatalf("browser policy still bound to JWT TTL: %s", raw)
	}
}

func TestBrowserActivityRenewsOnlyLiveUserAndPreservesIdentity(t *testing.T) {
	s, u := securedService(t)
	sessions := &browserTestSessions{}
	s.sessions = sessions
	if err := s.SetBrowserSessionTTL(100*time.Millisecond, time.Second); err != nil {
		t.Fatal(err)
	}
	first, err := s.LoginBrowser(context.Background(), "u", "correct", "ip")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	read, err := s.BrowserState(context.Background(), first.CookieToken)
	if err != nil || read.ExpiresAt != first.ExpiresAt {
		t.Fatalf("GET extended expiry: %+v %v", read, err)
	}
	renewed, err := s.BrowserActivity(context.Background(), first.CookieToken)
	if err != nil || renewed.ExpiresAt <= first.ExpiresAt || renewed.AbsoluteExpiresAt != first.AbsoluteExpiresAt || renewed.SessionID != first.SessionID || renewed.CSRFToken != first.CSRFToken || renewed.CookieToken != "" {
		t.Fatalf("activity=%+v %v", renewed, err)
	}
	u.user.AuthVersion++
	if _, err = s.BrowserActivity(context.Background(), first.CookieToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("changed auth version renewed: %v", err)
	}
	u.user.AuthVersion--
	u.user.Active = false
	if _, err = s.BrowserActivity(context.Background(), first.CookieToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("disabled user renewed: %v", err)
	}
	u.user.Active = true
	sessions.Err = errors.New("redis down")
	if _, err = s.BrowserActivity(context.Background(), first.CookieToken); !errors.Is(err, ErrSecurityUnavailable) {
		t.Fatalf("outage renewed: %v", err)
	}
}

func TestBrowserActivityLegacyAndExpired(t *testing.T) {
	s, _ := securedService(t)
	sessions := &browserTestSessions{}
	s.sessions = sessions
	token := strings.Repeat("l", 43)
	deadline := time.Now().Add(80 * time.Millisecond).UnixMilli()
	rec := session.BrowserSession{ID: browserID(token), UserID: "u", CSRFToken: "csrf", ExpiresAt: deadline}
	if err := sessions.CreateBrowserSession(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	result, err := s.BrowserActivity(context.Background(), token)
	if err != nil || result.ExpiresAt != deadline || result.AbsoluteExpiresAt != deadline {
		t.Fatalf("legacy lifetime changed: %+v %v", result, err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err = s.BrowserActivity(context.Background(), token); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expired revived: %v", err)
	}
}

func TestBrowserTTLRejectsInvalidPolicy(t *testing.T) {
	s, _ := securedService(t)
	for _, pair := range [][2]time.Duration{{0, time.Hour}, {time.Hour, 0}, {time.Hour, time.Minute}, {time.Nanosecond, time.Hour}, {time.Minute, -time.Hour}} {
		if err := s.SetBrowserSessionTTL(pair[0], pair[1]); err == nil {
			t.Fatalf("invalid policy accepted: %v", pair)
		}
	}
}

type activityUnavailableUsers struct {
	*securityUsers
	findErr    error
	contactErr error
}

func (u *activityUnavailableUsers) FindByID(ctx context.Context, id string) (domain.User, error) {
	if u.findErr != nil {
		return domain.User{}, u.findErr
	}
	return u.securityUsers.FindByID(ctx, id)
}
func (u *activityUnavailableUsers) RequiresContact(ctx context.Context, id string) (bool, error) {
	if u.contactErr != nil {
		return false, u.contactErr
	}
	return u.securityUsers.RequiresContact(ctx, id)
}

func TestBrowserActivityMongoOutagesDoNotRenew(t *testing.T) {
	s, base := securedService(t)
	users := &activityUnavailableUsers{securityUsers: base}
	s.users = users
	store := &browserTestSessions{}
	s.sessions = store
	first, err := s.LoginBrowser(context.Background(), "u", "correct", "ip")
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"live-user", "public-metadata"} {
		users.findErr = nil
		users.contactErr = nil
		if stage == "live-user" {
			users.findErr = errors.New("mongo offline")
		} else {
			users.contactErr = errors.New("mongo offline")
		}
		if _, err = s.BrowserActivity(context.Background(), first.CookieToken); !errors.Is(err, ErrSecurityUnavailable) {
			t.Fatalf("%s outage: %v", stage, err)
		}
		rec, err := store.GetBrowserSession(context.Background(), first.SessionID)
		if err != nil || rec.ExpiresAt != first.ExpiresAt {
			t.Fatalf("%s renewed during outage: %+v %v", stage, rec, err)
		}
	}
}

type interleavedActivityStore struct {
	browserTestSessions
	beforeRenew func()
}

func (s *interleavedActivityStore) RenewBrowserSession(ctx context.Context, id string, version int, idle time.Duration) (session.BrowserSession, error) {
	if s.beforeRenew != nil {
		s.beforeRenew()
	}
	return s.BrowserSessions.RenewBrowserSession(ctx, id, version, idle)
}
func TestBrowserActivityInterleavedLogoutOrLoginNeverSucceeds(t *testing.T) {
	for _, operation := range []string{"logout", "login"} {
		t.Run(operation, func(t *testing.T) {
			s, _ := securedService(t)
			store := &interleavedActivityStore{}
			s.sessions = store
			first, err := s.LoginBrowser(context.Background(), "u", "correct", "ip")
			if err != nil {
				t.Fatal(err)
			}
			want := ErrInvalidSession
			store.beforeRenew = func() {
				if operation == "logout" {
					if err := s.LogoutBrowser(context.Background(), first.CookieToken); err != nil {
						t.Fatal(err)
					}
				} else {
					want = ErrSessionReplaced
					if _, err := s.LoginBrowser(context.Background(), "u", "correct", "ip"); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err = s.BrowserActivity(context.Background(), first.CookieToken); !errors.Is(err, want) {
				t.Fatalf("interleaved %s succeeded: %v", operation, err)
			}
		})
	}
}
