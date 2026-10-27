package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func browserRecord(id, user string) BrowserSession {
	return BrowserSession{ID: id, UserID: user, AuthVersion: 3, CSRFToken: "csrf-test", ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}
}

func TestBrowserActivationRejectsStaleVersionAndRotation(t *testing.T) {
	r := scratchRedis(t)
	ctx := context.Background()
	first := browserRecord("first", "u")
	if err := r.CreateBrowserSession(ctx, first); err != nil {
		t.Fatal(err)
	}
	next := browserRecord("next", "u")
	next.AuthVersion++
	if err := r.CreateBrowserSession(ctx, next); err != nil {
		t.Fatal(err)
	}
	stale := browserRecord("stale", "u")
	if err := r.CreateBrowserSession(ctx, stale); !errors.Is(err, ErrBrowserSessionReplaced) {
		t.Fatalf("stale version accepted: %v", err)
	}
	rotation := browserRecord("rotation", "u")
	rotation.AuthVersion = next.AuthVersion
	rotation.ExpectedID = first.ID
	if err := r.CreateBrowserSession(ctx, rotation); !errors.Is(err, ErrBrowserSessionReplaced) {
		t.Fatalf("stale rotation accepted: %v", err)
	}
	if _, err := r.GetBrowserSession(ctx, next.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.RevokeBrowserSession(ctx, next.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateBrowserSession(ctx, stale); !errors.Is(err, ErrBrowserSessionReplaced) {
		t.Fatalf("version floor lost on logout: %v", err)
	}
}

func TestBrowserReplacementAndStaleLogout(t *testing.T) {
	r := scratchRedis(t)
	ctx := context.Background()
	for _, rec := range []BrowserSession{browserRecord("first", "u1"), browserRecord("other", "u2"), browserRecord("second", "u1")} {
		if err := r.CreateBrowserSession(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.GetBrowserSession(ctx, "first"); !errors.Is(err, ErrBrowserSessionReplaced) {
		t.Fatalf("old login accepted: %v", err)
	}
	if err := r.RevokeBrowserSession(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"second", "other"} {
		if _, err := r.GetBrowserSession(ctx, id); err != nil {
			t.Fatalf("current/other user revoked: %s: %v", id, err)
		}
	}
	if err := r.RevokeBrowserSession(ctx, "second"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetBrowserSession(ctx, "second"); !errors.Is(err, ErrBrowserSessionInvalid) {
		t.Fatalf("logout accepted: %v", err)
	}
	if _, err := r.GetBrowserSession(ctx, "first"); !errors.Is(err, ErrBrowserSessionReplaced) {
		t.Fatalf("replacement reason lost after logout: %v", err)
	}
}

func TestConcurrentBrowserLoginsLeaveExactlyOneValidSession(t *testing.T) {
	r := scratchRedis(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := r.CreateBrowserSession(ctx, browserRecord(fmt.Sprint(i), "u")); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	active := 0
	for i := 0; i < 20; i++ {
		_, err := r.GetBrowserSession(ctx, fmt.Sprint(i))
		if err == nil {
			active++
		} else if !errors.Is(err, ErrBrowserSessionReplaced) {
			t.Fatal(err)
		}
	}
	if active != 1 {
		t.Fatalf("active sessions=%d, want exactly one", active)
	}
}

func TestBrowserExpiryIsAbsoluteAndSubscriptionNotifies(t *testing.T) {
	r := scratchRedis(t)
	ctx := context.Background()
	changes, closeSub, err := r.SubscribeBrowserSessions(ctx, "u")
	if err != nil {
		t.Fatal(err)
	}
	defer closeSub()
	rec := browserRecord("short", "u")
	rec.ExpiresAt = time.Now().Add(120 * time.Millisecond).UnixMilli()
	if err := r.CreateBrowserSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("no session notification")
	}
	for i := 0; i < 4; i++ {
		_, _ = r.GetBrowserSession(ctx, "short")
		time.Sleep(40 * time.Millisecond)
	}
	if _, err := r.GetBrowserSession(ctx, "short"); !errors.Is(err, ErrBrowserSessionInvalid) {
		t.Fatalf("read extended expiry: %v", err)
	}
	if err := r.CreateBrowserSession(ctx, rec); err == nil {
		t.Fatal("expired session created")
	}
}
