package main

import (
	"testing"
	"time"
)

func TestStartupProductionRejectsKnownDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("INITIAL_PASSWORD", "")
	if _, err := loadStartupSecurity(); err == nil {
		t.Fatal("production accepted development secrets")
	}
	t.Setenv("JWT_SECRET", "a-long-private-secret-that-is-at-least-32")
	t.Setenv("INITIAL_PASSWORD", "private-bootstrap-password")
	cfg, err := loadStartupSecurity()
	if err != nil || cfg.seedDemo {
		t.Fatalf("safe defaults failed: %+v %v", cfg, err)
	}
}
func TestDemoSeedRequiresExplicitValidTrue(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("SEED_DEMO_ITEMS", "")
	cfg, err := loadStartupSecurity()
	if err != nil || cfg.seedDemo {
		t.Fatal("demo enabled by default")
	}
	t.Setenv("SEED_DEMO_ITEMS", "true")
	cfg, err = loadStartupSecurity()
	if err != nil || !cfg.seedDemo {
		t.Fatal("explicit seed ignored")
	}
	t.Setenv("SEED_DEMO_ITEMS", "tru")
	if _, err = loadStartupSecurity(); err == nil {
		t.Fatal("invalid seed flag silently accepted")
	}
}

func TestProductionRejectsExampleBootstrapPlaceholder(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "a-long-private-secret-that-is-at-least-32")
	t.Setenv("INITIAL_PASSWORD", "replace-with-private-bootstrap-password")
	if _, err := loadStartupSecurity(); err == nil {
		t.Fatal("production accepted example bootstrap password")
	}
}

func TestBrowserSessionStartupTTLPolicy(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("SESSION_IDLE_TTL", "")
	t.Setenv("SESSION_ABSOLUTE_TTL", "")
	cfg, err := loadStartupSecurity()
	if err != nil || cfg.sessionIdleTTL != 30*time.Minute || cfg.sessionAbsoluteTTL != 8*time.Hour {
		t.Fatalf("default policy: %+v %v", cfg, err)
	}
	for _, tc := range []struct{ idle, absolute string }{{"bad", "8h"}, {"0s", "8h"}, {"30m", "-1h"}, {"9h", "8h"}, {"1ns", "8h"}} {
		t.Setenv("SESSION_IDLE_TTL", tc.idle)
		t.Setenv("SESSION_ABSOLUTE_TTL", tc.absolute)
		if _, err := loadStartupSecurity(); err == nil {
			t.Fatalf("invalid policy accepted: %+v", tc)
		}
	}
	t.Setenv("SESSION_IDLE_TTL", "5m")
	t.Setenv("SESSION_ABSOLUTE_TTL", "2h")
	cfg, err = loadStartupSecurity()
	if err != nil || cfg.sessionIdleTTL != 5*time.Minute || cfg.sessionAbsoluteTTL != 2*time.Hour {
		t.Fatalf("custom policy: %+v %v", cfg, err)
	}
}
