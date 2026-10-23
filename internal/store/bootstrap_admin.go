package store

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"standard-setting/backend/internal/domain"
)

// BootstrapAdmin creates missing operator accounts without resetting existing access.
func (m *Mongo) BootstrapAdmin(ctx context.Context, initialPasswordHash string) error {
	now := time.Now().UTC()
	for _, account := range []struct{ id, username, name string }{
		{"user-superadmin", "superadmin", "Admin Super"},
		{"user-admin-bp3", "admin_bp3", "Admin BP3"},
	} {
		admin := domain.User{ID: account.id, Username: account.username, Name: account.name, PasswordHash: initialPasswordHash, Role: domain.RoleAdminSuper, MustChangePassword: true, Active: true, CreatedAt: now, UpdatedAt: now}
		_, err := m.users.UpdateOne(ctx, bson.M{"username": admin.Username}, bson.M{"$setOnInsert": admin},
			options.Update().SetUpsert(true).SetCollation(&options.Collation{Locale: "en", Strength: 2}))
		if err != nil {
			return fmt.Errorf("bootstrap admin %s: %w", admin.Username, err)
		}
	}
	return nil
}
