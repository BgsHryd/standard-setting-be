package store

import (
	"context"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"os"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/domain"
	"testing"
	"time"
)

func securityMongo(t *testing.T) (context.Context, *Mongo) {
	t.Helper()
	uri := os.Getenv("MIGRATION_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set MIGRATION_TEST_MONGO_URI to test a scratch database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	m, err := NewMongo(ctx, uri, fmt.Sprintf("security-test-%d", time.Now().UnixNano()))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { m.database.Drop(context.Background()); m.Close(context.Background()); cancel() })
	return ctx, m
}
func TestBootstrapDoesNotSeedDemoOrReplacePassword(t *testing.T) {
	ctx, m := securityMongo(t)
	if err := m.BootstrapAdmin(ctx, "initial-hash"); err != nil {
		t.Fatal(err)
	}
	if err := m.BootstrapAdmin(ctx, "replacement-hash"); err != nil {
		t.Fatal(err)
	}
	u, err := m.FindByUsername(ctx, "superadmin")
	if err != nil || u.PasswordHash != "initial-hash" {
		t.Fatal("bootstrap replaced existing credentials", err)
	}
	n, err := m.items.CountDocuments(ctx, bson.M{})
	if err != nil || n != 0 {
		t.Fatal("bootstrap created demo items", n, err)
	}
	for i := 0; i < 2; i++ {
		if err := m.SeedDemoItems(ctx); err != nil {
			t.Fatal(err)
		}
	}
	n, err = m.items.CountDocuments(ctx, bson.M{})
	if err != nil || n != 36 {
		t.Fatal("explicit demo not idempotent", n, err)
	}
}
func TestAuditAppendOnlyAndTransactionRollback(t *testing.T) {
	ctx, m := securityMongo(t)
	event := domain.AuditEvent{ID: "a", ActorID: "admin", TargetID: "u", ProjectID: "p", Action: "auth.reset", Outcome: "success"}
	if err := m.RecordAudit(ctx, event); err != nil {
		t.Fatal(err)
	}
	event.Outcome = "changed"
	if err := m.RecordAudit(ctx, event); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("existing event overwritten: %v", err)
	}
	rows, err := m.ListAudit(ctx, "p", "admin", "auth.reset")
	if err != nil || len(rows) != 1 || rows[0].Outcome != "success" || rows[0].RequestID == "" || rows[0].CreatedAt.IsZero() {
		t.Fatalf("bad audit rows: %+v %v", rows, err)
	}
	err = m.WithTransaction(ctx, func(tx context.Context) error {
		event.ID = "rollback"
		if err := m.RecordAudit(tx, event); err != nil {
			return err
		}
		return fmt.Errorf("abort")
	})
	if err == nil {
		t.Fatal("transaction did not abort")
	}
	rows, err = m.ListAudit(ctx, "p", "", "auth.reset")
	if err != nil || len(rows) != 1 {
		t.Fatalf("aborted audit persisted %+v %v", rows, err)
	}
}

type unusedAuthSessions struct{}

func (unusedAuthSessions) Save(context.Context, string, string, time.Duration) error { return nil }
func (unusedAuthSessions) Consume(context.Context, string) (string, error) {
	return "", fmt.Errorf("unused")
}
func TestPasswordChangeRollsBackWhenAuditInsertFails(t *testing.T) {
	ctx, m := securityMongo(t)
	hash, _ := auth.HashPassword("old-password")
	if err := m.BootstrapAdmin(ctx, hash); err != nil {
		t.Fatal(err)
	}
	err := m.database.RunCommand(ctx, bson.D{{Key: "collMod", Value: "audit_events"}, {Key: "validator", Value: bson.M{"action": bson.M{"$ne": "auth.password_change"}}}}).Err()
	if err != nil {
		t.Fatal(err)
	}
	s := auth.NewService(m, unusedAuthSessions{}, "secret", time.Minute, time.Hour)
	err = s.ChangePassword(ctx, "user-superadmin", "old-password", "new-password")
	if !errors.Is(err, auth.ErrSecurityUnavailable) {
		t.Fatalf("expected audit failure got %v", err)
	}
	u, err := m.FindByID(ctx, "user-superadmin")
	if err != nil || u.PasswordHash != hash || u.AuthVersion != 0 {
		t.Fatal("password changed without audit", err)
	}
}

func TestContactIndexAllowsDistinctUserIDsStoredAsMongoIDs(t *testing.T) {
	ctx, m := securityMongo(t)
	_, err := m.database.Collection("panelist_contacts").InsertMany(ctx, []any{domain.PanelistContact{UserID: "u1", ProjectID: "p", Email: "one@example.org"}, domain.PanelistContact{UserID: "u2", ProjectID: "p", Email: "two@example.org"}})
	if err != nil {
		t.Fatal("index blocks second user's contact", err)
	}
}

func TestAuditSearchMatchesNamesLiterallyAndKeepsScope(t *testing.T) {
	ctx, m := securityMongo(t)
	_, err := m.users.InsertOne(ctx, domain.User{ID: "named", Username: "operator.a", Name: "Alice*"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []domain.AuditEvent{{ID: "scope", ActorID: "named", TargetID: "named", ProjectID: "p", Action: "auth.reset", Outcome: "success"}, {ID: "other", ActorID: "named", ProjectID: "elsewhere", Action: "auth.reset", Outcome: "success"}} {
		if err = m.RecordAudit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{"Alice*", "operator.a"} {
		rows, err := m.ListAudit(ctx, "p", q, "auth.reset")
		if err != nil || len(rows) != 1 || rows[0].ID != "scope" {
			t.Fatalf("name search failed %q %+v %v", q, rows, err)
		}
	}
	rows, err := m.ListAudit(ctx, "p", "Alice.*", "auth.reset")
	if err != nil || len(rows) != 0 {
		t.Fatalf("name query interpreted as regex %+v %v", rows, err)
	}
}
