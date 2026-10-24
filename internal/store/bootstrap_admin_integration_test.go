package store

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/domain"
)

func TestBootstrapCreatesBP3SuperadminWithInitialPassword(t *testing.T) {
	ctx, m := securityMongo(t)
	hash, err := auth.HashPassword("test-private-initial-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.BootstrapAdmin(ctx, hash); err != nil {
		t.Fatal(err)
	}
	user, err := m.FindByUsername(ctx, "admin_bp3")
	if err != nil {
		t.Fatalf("additional superadmin was not created: %v", err)
	}
	if user.ID != "user-admin-bp3" || user.Name != "Admin BP3" || user.Role != domain.RoleAdminSuper || !user.Active || user.Removed || !user.MustChangePassword || user.ProjectID != "" || user.SubjectCode != "" || user.CreatedAt.IsZero() || user.UpdatedAt.IsZero() {
		t.Fatal("additional account does not have the required superadmin access and first-login settings")
	}
	s := auth.NewService(m, unusedAuthSessions{}, "test-jwt-secret", time.Minute, time.Hour)
	result, err := s.Login(ctx, "admin_bp3", "test-private-initial-password")
	if err != nil || result.User.Role != domain.RoleAdminSuper || !result.User.MustChangePassword {
		t.Fatalf("additional superadmin cannot log in with the initial password: %v", err)
	}
	count, err := m.items.CountDocuments(ctx, bson.M{})
	if err != nil || count != 0 {
		t.Fatalf("bootstrap unexpectedly seeded demo items: count=%d err=%v", count, err)
	}
}

func TestBootstrapPreservesExistingBP3Account(t *testing.T) {
	for _, username := range []string{"admin_bp3", "ADMIN_BP3"} {
		t.Run(username, func(t *testing.T) {
			ctx, m := securityMongo(t)
			created := time.Date(2026, time.October, 16, 1, 0, 0, 0, time.UTC)
			existing := domain.User{ID: "existing-bp3-id", Username: username, Name: "Existing BP3", PasswordHash: "existing-private-hash", Role: domain.RoleAdminSuper, Active: false, Removed: true, MustChangePassword: false, AuthVersion: 7, CreatedAt: created, UpdatedAt: created}
			if _, err := m.users.InsertOne(ctx, existing); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := m.BootstrapAdmin(ctx, "replacement-hash"); err != nil {
					t.Fatalf("repeated bootstrap failed: %v", err)
				}
			}
			user, err := m.FindByUsername(ctx, "admin_bp3")
			if err != nil || user.ID != existing.ID || user.Username != existing.Username || user.Name != existing.Name || user.PasswordHash != existing.PasswordHash || user.Role != existing.Role || user.Active || !user.Removed || user.MustChangePassword || user.AuthVersion != 7 || !user.CreatedAt.Equal(created) || !user.UpdatedAt.Equal(created) {
				t.Fatalf("bootstrap changed the existing account: %v", err)
			}
			count, err := m.users.CountDocuments(ctx, bson.M{})
			if err != nil || count != 2 {
				t.Fatalf("bootstrap duplicated accounts: count=%d err=%v", count, err)
			}
		})
	}
}
