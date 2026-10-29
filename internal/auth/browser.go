package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/session"
)

var ErrSessionReplaced = errors.New("Anda telah login di perangkat lain")

type BrowserResult struct {
	CookieToken       string   `json:"-"`
	SessionID         string   `json:"sessionId"`
	CSRFToken         string   `json:"csrfToken"`
	AuthVersion       int      `json:"authVersion"`
	ExpiresAt         int64    `json:"expiresAt"`
	AbsoluteExpiresAt int64    `json:"absoluteExpiresAt"`
	CreatedAt         int64    `json:"createdAt,omitempty"`
	User              UserView `json:"user"`
}

func browserID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Service) browserStore() (session.BrowserStore, error) {
	store, ok := s.sessions.(session.BrowserStore)
	if !ok {
		return nil, ErrSecurityUnavailable
	}
	return store, nil
}

func (s *Service) LoginBrowser(ctx context.Context, username, password, ip string) (BrowserResult, error) {
	user, err := s.loginUser(ctx, username, password, ip)
	if err != nil {
		return BrowserResult{}, err
	}
	return s.newBrowserSession(ctx, user, true)
}

func (s *Service) newBrowserSession(ctx context.Context, user domain.User, login bool, expectedID ...string) (BrowserResult, error) {
	store, err := s.browserStore()
	if err != nil {
		return BrowserResult{}, err
	}
	token, err := randomToken(32)
	if err != nil {
		return BrowserResult{}, ErrSecurityUnavailable
	}
	csrf, err := randomToken(32)
	if err != nil {
		return BrowserResult{}, ErrSecurityUnavailable
	}
	now := time.Now()
	rec := session.BrowserSession{ID: browserID(token), UserID: user.ID, AuthVersion: user.AuthVersion, CSRFToken: csrf, CreatedAt: now.UnixMilli(), LastActivityAt: now.UnixMilli(), ExpiresAt: now.Add(s.browserIdleTTL).UnixMilli(), AbsoluteExpiresAt: now.Add(s.browserAbsoluteTTL).UnixMilli()}
	if len(expectedID) > 0 {
		rec.ExpectedID = expectedID[0]
	}
	result, err := s.browserResult(ctx, rec, user)
	if err != nil {
		return BrowserResult{}, err
	}
	// A failed audit must not replace a session that was working before this login.
	if login {
		if err = s.audit(ctx, user, "auth.login", "success"); err != nil {
			return BrowserResult{}, err
		}
	}
	if err = store.CreateBrowserSession(ctx, rec); err != nil {
		if errors.Is(err, session.ErrBrowserSessionReplaced) {
			return BrowserResult{}, ErrSessionReplaced
		}
		return BrowserResult{}, ErrSecurityUnavailable
	}
	result.CookieToken = token
	return result, nil
}

func (s *Service) AuthenticateBrowser(ctx context.Context, token string) (session.BrowserSession, domain.User, error) {
	var rec session.BrowserSession
	var user domain.User
	if len(token) != 43 {
		return rec, user, ErrInvalidSession
	}
	store, err := s.browserStore()
	if err != nil {
		return rec, user, err
	}
	rec, err = store.GetBrowserSession(ctx, browserID(token))
	if errors.Is(err, session.ErrBrowserSessionReplaced) {
		return rec, user, ErrSessionReplaced
	}
	if errors.Is(err, session.ErrBrowserSessionInvalid) {
		return rec, user, ErrInvalidSession
	}
	if err != nil {
		return rec, user, ErrSecurityUnavailable
	}
	user, err = s.users.FindByID(ctx, rec.UserID)
	if err != nil {
		return rec, user, ErrSecurityUnavailable
	}
	if !user.Active || user.Removed || user.AuthVersion != rec.AuthVersion {
		return rec, user, ErrInvalidSession
	}
	return rec, user, nil
}

func (s *Service) BrowserState(ctx context.Context, token string) (BrowserResult, error) {
	rec, user, err := s.AuthenticateBrowser(ctx, token)
	if err != nil {
		return BrowserResult{}, err
	}
	return s.browserResult(ctx, rec, user)
}

// BrowserActivity explicitly renews user activity; normal reads never call it.
func (s *Service) BrowserActivity(ctx context.Context, token string) (BrowserResult, error) {
	rec, user, err := s.AuthenticateBrowser(ctx, token)
	if err != nil {
		return BrowserResult{}, err
	}
	// Resolve live public metadata before changing Redis, so repository outages
	// cannot be reported as a successful renewal.
	result, err := s.browserResult(ctx, rec, user)
	if err != nil {
		return BrowserResult{}, err
	}
	store, err := s.browserStore()
	if err != nil {
		return BrowserResult{}, err
	}
	renewed, err := store.RenewBrowserSession(ctx, rec.ID, user.AuthVersion, s.browserIdleTTL)
	if errors.Is(err, session.ErrBrowserSessionReplaced) {
		return BrowserResult{}, ErrSessionReplaced
	}
	if errors.Is(err, session.ErrBrowserSessionInvalid) {
		return BrowserResult{}, ErrInvalidSession
	}
	if err != nil {
		return BrowserResult{}, ErrSecurityUnavailable
	}
	result.ExpiresAt = renewed.ExpiresAt
	result.AbsoluteExpiresAt = renewed.AbsoluteExpiresAt
	if result.AbsoluteExpiresAt == 0 {
		result.AbsoluteExpiresAt = renewed.ExpiresAt
	}
	return result, nil
}

func (s *Service) browserResult(ctx context.Context, rec session.BrowserSession, user domain.User) (BrowserResult, error) {
	view := userView(user)
	if contacts, ok := s.users.(interface {
		RequiresContact(context.Context, string) (bool, error)
	}); ok {
		var err error
		view.ContactRequired, err = contacts.RequiresContact(ctx, user.ID)
		if err != nil {
			return BrowserResult{}, ErrSecurityUnavailable
		}
	}
	absolute := rec.AbsoluteExpiresAt
	if absolute == 0 {
		absolute = rec.ExpiresAt
	} // Pre-upgrade sessions keep their fixed deadline.
	return BrowserResult{SessionID: rec.ID, CSRFToken: rec.CSRFToken, AuthVersion: rec.AuthVersion, ExpiresAt: rec.ExpiresAt, AbsoluteExpiresAt: absolute, CreatedAt: rec.CreatedAt, User: view}, nil
}

func (s *Service) LogoutBrowser(ctx context.Context, token string) error {
	if len(token) != 43 {
		return nil
	}
	store, err := s.browserStore()
	if err != nil {
		return err
	}
	if err = store.RevokeBrowserSession(ctx, browserID(token)); err != nil {
		return ErrSecurityUnavailable
	}
	return nil
}

func (s *Service) ChangeBrowserPassword(ctx context.Context, token, currentPassword, newPassword string) (BrowserResult, error) {
	rec, user, err := s.AuthenticateBrowser(ctx, token)
	if err != nil {
		return BrowserResult{}, err
	}
	version, err := s.changePassword(ctx, user.ID, &rec.AuthVersion, currentPassword, newPassword)
	if err != nil {
		return BrowserResult{}, err
	}
	user, err = s.CurrentUser(ctx, user.ID)
	if err != nil || user.AuthVersion != version {
		return BrowserResult{}, ErrInvalidSession
	}
	return s.newBrowserSession(ctx, user, false, rec.ID)
}

// Subscribe before validating again: a replacement during subscription setup
// must be observed even when its Pub/Sub message predates the acknowledgement.
func (s *Service) WatchBrowserSessions(ctx context.Context, token string) (<-chan struct{}, func(), error) {
	rec, _, err := s.AuthenticateBrowser(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	store, err := s.browserStore()
	if err != nil {
		return nil, nil, err
	}
	events, closeSub, err := store.SubscribeBrowserSessions(ctx, rec.UserID)
	if err != nil {
		return nil, nil, ErrSecurityUnavailable
	}
	if _, _, err = s.AuthenticateBrowser(ctx, token); err != nil {
		closeSub()
		return nil, nil, err
	}
	return events, closeSub, nil
}
