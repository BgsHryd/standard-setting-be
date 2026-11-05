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

func slidingRecord(id string) BrowserSession {
	rec := browserRecord(id, "renew-user")
	rec.CreatedAt = time.Now().Add(-time.Minute).UnixMilli()
	rec.LastActivityAt = rec.CreatedAt
	rec.ExpiresAt = time.Now().Add(2 * time.Second).UnixMilli()
	rec.AbsoluteExpiresAt = time.Now().Add(10 * time.Second).UnixMilli()
	return rec
}

func TestBrowserRenewalBoundsTTLAndReadsDoNotRenew(t *testing.T) {
	r := scratchRedis(t)
	ctx := context.Background()
	rec := slidingRecord("bounded")
	if err := r.CreateBrowserSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		got, err := r.GetBrowserSession(ctx, rec.ID)
		if err != nil || got.ExpiresAt != rec.ExpiresAt {
			t.Fatalf("read renewed: %+v %v", got, err)
		}
	}
	got, err := r.RenewBrowserSession(ctx, rec.ID, rec.AuthVersion, 5*time.Second)
	if err != nil || got.ExpiresAt <= rec.ExpiresAt || got.LastActivityAt <= rec.LastActivityAt || got.AbsoluteExpiresAt != rec.AbsoluteExpiresAt || got.CreatedAt != rec.CreatedAt {
		t.Fatalf("renew=%+v %v", got, err)
	}
	for _, key := range []string{browserPrefix + rec.ID, browserUserPrefix + rec.UserID} {
		ttl, err := r.client.PTTL(ctx, key).Result()
		if err != nil || ttl < 4*time.Second || ttl > 5*time.Second {
			t.Fatalf("unbounded ttl %s: %v %v", key, ttl, err)
		}
	}
	got, err = r.RenewBrowserSession(ctx, rec.ID, rec.AuthVersion, time.Hour)
	if err != nil || got.ExpiresAt != rec.AbsoluteExpiresAt {
		t.Fatalf("absolute cap lost: %+v %v", got, err)
	}
	// Changing configuration cannot shorten an already accepted deadline.
	next, err := r.RenewBrowserSession(ctx, rec.ID, rec.AuthVersion, time.Second)
	if err != nil || next.ExpiresAt != got.ExpiresAt {
		t.Fatalf("renew shortened: %+v %v", next, err)
	}
}

func TestBrowserRenewalLegacyKeepsFixedLifetime(t *testing.T) {
	r := scratchRedis(t)
	ctx := context.Background()
	rec := browserRecord("legacy", "renew-user")
	if err := r.CreateBrowserSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	before, _ := r.client.PTTL(ctx, browserPrefix+rec.ID).Result()
	got, err := r.RenewBrowserSession(ctx, rec.ID, rec.AuthVersion, time.Hour)
	after, _ := r.client.PTTL(ctx, browserPrefix+rec.ID).Result()
	if err != nil || got.ExpiresAt != rec.ExpiresAt || got.AbsoluteExpiresAt != 0 || after > before {
		t.Fatalf("legacy extended: %+v before=%v after=%v err=%v", got, before, after, err)
	}
}

func TestBrowserRenewalRejectsRevokedReplacedVersionAndExpired(t *testing.T) {
	for _, kind := range []string{"logout", "replaced", "floor", "version", "idle", "absolute", "missing-mapping"} {
		t.Run(kind, func(t *testing.T) {
			r := scratchRedis(t)
			ctx := context.Background()
			rec := slidingRecord(kind)
			if err := r.CreateBrowserSession(ctx, rec); err != nil {
				t.Fatal(err)
			}
			version := rec.AuthVersion
			want := ErrBrowserSessionInvalid
			switch kind {
			case "logout":
				if err := r.RevokeBrowserSession(ctx, rec.ID); err != nil {
					t.Fatal(err)
				}
			case "replaced":
				newer := slidingRecord("newer")
				if err := r.CreateBrowserSession(ctx, newer); err != nil {
					t.Fatal(err)
				}
				want = ErrBrowserSessionReplaced
			case "missing-mapping":
				r.client.Del(ctx, browserUserPrefix+rec.UserID)
				want = ErrBrowserSessionReplaced
			case "floor":
				r.client.Set(ctx, "ss:browser:version:"+rec.UserID, rec.AuthVersion+1, 0)
			case "version":
				version++
			case "idle":
				rec.ExpiresAt = time.Now().Add(-time.Second).UnixMilli()
			case "absolute":
				rec.AbsoluteExpiresAt = time.Now().Add(-time.Second).UnixMilli()
			}
			if kind == "idle" || kind == "absolute" {
				raw, _ := json.Marshal(rec)
				r.client.Set(ctx, browserPrefix+rec.ID, raw, time.Minute)
			}
			if _, err := r.RenewBrowserSession(ctx, rec.ID, version, time.Hour); !errors.Is(err, want) {
				t.Fatalf("revived %s: %v", kind, err)
			}
		})
	}
}

func TestConcurrentRenewalCannotReviveLogoutOrReplacement(t *testing.T) {
	r := scratchRedis(t)
	ctx := context.Background()
	for _, kind := range []string{"logout", "replacement"} {
		for i := 0; i < 25; i++ {
			rec := slidingRecord(fmt.Sprintf("%s-%d", kind, i))
			if err := r.CreateBrowserSession(ctx, rec); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				_, err := r.RenewBrowserSession(ctx, rec.ID, rec.AuthVersion, time.Hour)
				if err != nil && !errors.Is(err, ErrBrowserSessionInvalid) && !errors.Is(err, ErrBrowserSessionReplaced) {
					t.Error(err)
				}
			}()
			go func() {
				defer wg.Done()
				if kind == "logout" {
					if err := r.RevokeBrowserSession(ctx, rec.ID); err != nil {
						t.Error(err)
					}
				} else {
					newer := slidingRecord("new-" + rec.ID)
					if err := r.CreateBrowserSession(ctx, newer); err != nil {
						t.Error(err)
					}
				}
			}()
			wg.Wait()
			if _, err := r.GetBrowserSession(ctx, rec.ID); err == nil {
				t.Fatalf("%s revived", kind)
			}
			if _, err := r.RenewBrowserSession(ctx, rec.ID, rec.AuthVersion, time.Hour); err == nil {
				t.Fatalf("%s renewed after revocation", kind)
			}
		}
	}
}
