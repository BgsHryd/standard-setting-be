package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// Independent acceptance coverage: execute the real Lua scripts against a
// disposable Redis, including serialization with logout and replacement.
func TestQARenewalSerializationNeverRevivesRevokedSessions(t *testing.T) {
	r := scratchRedis(t)
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		for _, replacement := range []bool{false, true} {
			user := fmt.Sprintf("qa-%d-%t", i, replacement)
			now := time.Now().UnixMilli()
			rec := BrowserSession{ID: user + "-old", UserID: user, AuthVersion: 2, CSRFToken: "qa-csrf", CreatedAt: now, LastActivityAt: now, ExpiresAt: now + 60000, AbsoluteExpiresAt: now + 120000}
			if err := r.CreateBrowserSession(ctx, rec); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				_, err := r.RenewBrowserSession(ctx, rec.ID, rec.AuthVersion, time.Minute)
				if err != nil && !errors.Is(err, ErrBrowserSessionInvalid) && !errors.Is(err, ErrBrowserSessionReplaced) {
					t.Error(err)
				}
			}()
			go func() {
				defer wg.Done()
				<-start
				if replacement {
					next := rec
					next.ID = user + "-new"
					if err := r.CreateBrowserSession(ctx, next); err != nil {
						t.Error(err)
					}
				} else if err := r.RevokeBrowserSession(ctx, rec.ID); err != nil {
					t.Error(err)
				}
			}()
			close(start)
			wg.Wait()
			for attempt := 0; attempt < 3; attempt++ {
				if _, err := r.RenewBrowserSession(ctx, rec.ID, rec.AuthVersion, time.Minute); err == nil {
					t.Fatalf("revived %s replacement=%t", rec.ID, replacement)
				}
			}
			active, err := r.client.Get(ctx, browserUserPrefix+user).Result()
			if replacement {
				if err != nil || active != user+"-new" {
					t.Fatalf("new login lost: %q %v", active, err)
				}
			} else if active != "" {
				t.Fatalf("logout mapping restored: %q", active)
			}
		}
	}
}

func TestQARenewalTTLAndAuthorityBounds(t *testing.T) {
	r := scratchRedis(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	rec := BrowserSession{ID: "qa-bound", UserID: "qa-bound-user", AuthVersion: 3, CSRFToken: "qa-csrf", CreatedAt: now, LastActivityAt: now, ExpiresAt: now + 600, AbsoluteExpiresAt: now + 1200}
	if err := r.CreateBrowserSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	renewed, err := r.RenewBrowserSession(ctx, rec.ID, rec.AuthVersion, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.ExpiresAt != rec.AbsoluteExpiresAt || renewed.AbsoluteExpiresAt != rec.AbsoluteExpiresAt {
		t.Fatalf("absolute bound changed: %+v", renewed)
	}
	for _, key := range []string{browserPrefix + rec.ID, browserUserPrefix + rec.UserID} {
		ttl, err := r.client.PTTL(ctx, key).Result()
		if err != nil || ttl <= 0 || ttl > 1200*time.Millisecond {
			t.Fatalf("unbounded key %s ttl=%v err=%v", key, ttl, err)
		}
	}
	before, _ := r.client.Get(ctx, browserPrefix+rec.ID).Result()
	for i := 0; i < 8; i++ {
		if _, err := r.GetBrowserSession(ctx, rec.ID); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := r.client.Get(ctx, browserPrefix+rec.ID).Result()
	if before != after {
		t.Fatal("validation mutated session")
	}
	// An authoritative version floor rejects renewal even if the record is live.
	if err := r.client.Set(ctx, "ss:browser:version:"+rec.UserID, 4, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RenewBrowserSession(ctx, rec.ID, rec.AuthVersion, time.Minute); err == nil {
		t.Fatal("renewed below auth version floor")
	}
}

func TestQALegacyRenewalKeepsOriginalDeadlineAndExpiredCannotRevive(t *testing.T) {
	r := scratchRedis(t)
	ctx := context.Background()
	rec := browserRecord("qa-legacy", "qa-legacy-user")
	rec.ExpiresAt = time.Now().Add(250 * time.Millisecond).UnixMilli()
	if err := r.CreateBrowserSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	got, err := r.RenewBrowserSession(ctx, rec.ID, rec.AuthVersion, 8*time.Hour)
	if err != nil || got.ExpiresAt != rec.ExpiresAt {
		t.Fatalf("legacy extension: %+v %v", got, err)
	}
	time.Sleep(275 * time.Millisecond)
	if _, err := r.RenewBrowserSession(ctx, rec.ID, rec.AuthVersion, time.Hour); err == nil {
		t.Fatal("expired record revived")
	}
	// Also test a live Redis key whose embedded absolute deadline has elapsed.
	now := time.Now().UnixMilli()
	rec.ID = "qa-deadline"
	rec.CreatedAt = now - 2000
	rec.LastActivityAt = now - 1000
	rec.AbsoluteExpiresAt = now - 1
	rec.ExpiresAt = now + 60000
	data, _ := json.Marshal(rec)
	r.client.Set(ctx, browserPrefix+rec.ID, data, time.Minute)
	r.client.Set(ctx, browserUserPrefix+rec.UserID, rec.ID, time.Minute)
	if _, err := r.RenewBrowserSession(ctx, rec.ID, rec.AuthVersion, time.Hour); err == nil {
		t.Fatal("elapsed absolute deadline revived")
	}
}
