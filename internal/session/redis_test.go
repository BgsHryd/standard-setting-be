package session

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func scratchRedis(t *testing.T) *Redis {
	t.Helper()
	bin, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server unavailable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cmd := exec.Command(bin, "--bind", "127.0.0.1", "--port", fmt.Sprint(port), "--save", "", "--appendonly", "no", "--dir", filepath.Clean(t.TempDir()))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	r := NewRedis(fmt.Sprintf("127.0.0.1:%d", port), "", 0)
	t.Cleanup(func() { r.Close() })
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		if r.Ping(ctx) == nil {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("scratch Redis did not start")
	return nil
}
func TestLoginLimiterAtomicTTLAndSharedAcrossClients(t *testing.T) {
	r := scratchRedis(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	allowed := make(chan bool, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			retry, err := r.ReserveLogin(ctx, "same", "192.0.2.1")
			if err != nil {
				t.Error(err)
			}
			allowed <- retry == 0
		}()
	}
	wg.Wait()
	close(allowed)
	n := 0
	for a := range allowed {
		if a {
			n++
		}
	}
	if n != 5 {
		t.Fatalf("concurrent attempts allowed=%d want 5", n)
	}
	keys, _ := r.client.Keys(ctx, "ss:login:*").Result()
	if len(keys) != 2 {
		t.Fatalf("expected username and IP counters, got %v", keys)
	}
	for _, key := range keys {
		ttl, _ := r.client.TTL(ctx, key).Result()
		if ttl <= 0 || ttl > 15*time.Minute {
			t.Fatalf("counter has no bounded TTL: %v", ttl)
		}
	}
	other := NewRedis(r.client.Options().Addr, "", 0)
	defer other.Close()
	if retry, err := other.ReserveLogin(ctx, "same", "other-ip"); err != nil || retry <= 0 {
		t.Fatalf("limiter not shared: %v %v", retry, err)
	}
	if err := r.ResetLogin(ctx, "same"); err != nil {
		t.Fatal(err)
	}
	if retry, err := r.ReserveLogin(ctx, "same", "other-ip"); err != nil || retry != 0 {
		t.Fatalf("username not reset: %v %v", retry, err)
	}
	if retry, err := r.ReserveLogin(ctx, "fresh", "192.0.2.1"); err != nil || retry <= 0 {
		t.Fatalf("success cleared IP spray guard: %v %v", retry, err)
	}
}
func TestLoginLimiterExpiresAndSessionReplayFails(t *testing.T) {
	r := scratchRedis(t)
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		r.ReserveLogin(ctx, "u", "ip")
	}
	keys, _ := r.client.Keys(ctx, "ss:login:*").Result()
	for _, k := range keys {
		r.client.PExpire(ctx, k, time.Millisecond)
	}
	time.Sleep(10 * time.Millisecond)
	if retry, err := r.ReserveLogin(ctx, "u", "ip"); err != nil || retry != 0 {
		t.Fatalf("temporary lock did not expire: %v %v", retry, err)
	}
	r.Save(ctx, "token", "user", time.Minute)
	if value, err := r.Consume(ctx, "token"); err != nil || value != "user" {
		t.Fatal(value, err)
	}
	if _, err := r.Consume(ctx, "token"); err == nil {
		t.Fatal("token replay accepted")
	}
}
