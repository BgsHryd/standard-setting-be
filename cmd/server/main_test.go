package main

import "testing"

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
