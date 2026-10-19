package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func scratchDatabase(t *testing.T) (context.Context, *mongo.Database) {
	t.Helper()
	uri := os.Getenv("MIGRATION_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set MIGRATION_TEST_MONGO_URI untuk integration test MongoDB scratch")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		cancel()
		_ = client.Disconnect(context.Background())
		t.Fatal(err)
	}
	db := client.Database(fmt.Sprintf("standard-setting-migration-test-%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := db.Drop(cleanup); err != nil {
			t.Errorf("cleanup scratch database %s: %v", db.Name(), err)
		}
		_ = client.Disconnect(cleanup)
		cancel()
	})
	return ctx, db
}

func TestStatusDoesNotCreateCollections(t *testing.T) {
	ctx, db := scratchDatabase(t)
	entries, err := Status(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0].Applied {
		t.Fatalf("expected one pending baseline, got %+v", entries)
	}
	names, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil || len(names) != 0 {
		t.Fatalf("status mutated database: collections=%v err=%v", names, err)
	}
}

func TestUpCreatesSchemaAndEnforcesUniqueUsername(t *testing.T) {
	ctx, db := scratchDatabase(t)
	entries, err := Up(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || !entries[0].Applied || entries[0].AppliedAt == nil {
		t.Fatalf("baseline not recorded: %+v", entries)
	}
	for _, collection := range []string{"users", "projects", "items", "rounds", "submissions", "schema_migrations"} {
		names, err := db.ListCollectionNames(ctx, bson.M{"name": collection})
		if err != nil || len(names) != 1 {
			t.Fatalf("missing %s: %v", collection, err)
		}
	}
	count, err := db.Collection("users").CountDocuments(ctx, bson.M{})
	if err != nil || count != 0 {
		t.Fatalf("up must not seed users: count=%d err=%v", count, err)
	}
	_, err = db.Collection("users").InsertOne(ctx, bson.M{"_id": "existing-user", "username": "operator"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Collection("users").InsertOne(ctx, bson.M{"_id": "duplicate-user", "username": "operator"})
	if !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("expected duplicate username to be rejected, got %v", err)
	}
}

func TestRerunPreservesExistingDataAndAppliedTimestamp(t *testing.T) {
	ctx, db := scratchDatabase(t)
	_, err := db.Collection("users").InsertOne(ctx, bson.M{"_id": "existing-user", "username": "operator", "passwordHash": "do-not-replace", "mustChangePassword": false})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Collection("items").InsertOne(ctx, bson.M{"_id": "MAT-901", "usageStatus": "tested", "readOnly": true, "projectId": "existing-project"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := Up(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Up(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if !first[0].AppliedAt.Equal(*second[0].AppliedAt) {
		t.Fatal("rerun changed original appliedAt")
	}
	var user, item bson.M
	if err := db.Collection("users").FindOne(ctx, bson.M{"_id": "existing-user"}).Decode(&user); err != nil {
		t.Fatal(err)
	}
	if user["passwordHash"] != "do-not-replace" || user["mustChangePassword"] != false {
		t.Fatalf("migration changed user: %v", user)
	}
	if err := db.Collection("items").FindOne(ctx, bson.M{"_id": "MAT-901"}).Decode(&item); err != nil {
		t.Fatal(err)
	}
	if item["usageStatus"] != "tested" || item["readOnly"] != true || item["projectId"] != "existing-project" {
		t.Fatalf("migration changed item: %v", item)
	}
	count, err := db.Collection("schema_migrations").CountDocuments(ctx, bson.M{})
	if err != nil || count != 3 {
		t.Fatalf("expected three ledger entries, count=%d err=%v", count, err)
	}
}

func TestCLIStatusUpAndOptInSeed(t *testing.T) {
	ctx, db := scratchDatabase(t)
	invoke := func(command string) map[string]any {
		t.Helper()
		cmd := exec.CommandContext(ctx, "go", "run", "../../cmd/migrate", "-command", command,
			"-uri", os.Getenv("MIGRATION_TEST_MONGO_URI"), "-database", db.Name())
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("CLI %s: %v: %s", command, err, output)
		}
		var result map[string]any
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("CLI %s did not produce JSON: %v", command, err)
		}
		if result["database"] != db.Name() {
			t.Fatalf("CLI used wrong database: %v", result)
		}
		return result
	}
	invoke("status")
	names, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil || len(names) != 0 {
		t.Fatalf("CLI status mutated database: %v %v", names, err)
	}
	invoke("up")
	_, err = db.Collection("users").InsertOne(ctx, bson.M{"_id": "user-superadmin", "username": "superadmin", "passwordHash": "existing-hash", "mustChangePassword": false})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Collection("items").InsertOne(ctx, bson.M{"_id": "MAT-001", "usageStatus": "tested", "readOnly": true, "projectId": "existing-project"})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if invoke("seed")["seeded"] != true {
			t.Fatal("CLI seed did not report success")
		}
	}
	var user, item bson.M
	_ = db.Collection("users").FindOne(ctx, bson.M{"_id": "user-superadmin"}).Decode(&user)
	_ = db.Collection("items").FindOne(ctx, bson.M{"_id": "MAT-001"}).Decode(&item)
	if user["passwordHash"] != "existing-hash" || user["mustChangePassword"] != false || item["projectId"] != "existing-project" || item["readOnly"] != true {
		t.Fatalf("CLI seed replaced existing data: user=%v item=%v", user, item)
	}
	count, err := db.Collection("items").CountDocuments(ctx, bson.M{})
	if err != nil || count != 36 {
		t.Fatalf("seed rerun duplicated/omitted sample items: count=%d err=%v", count, err)
	}
}

func TestFeatureMigrationPreservesHistoricalResultsAndAddsSafeDefaults(t *testing.T) {
	ctx, db := scratchDatabase(t)
	_, err := db.Collection("projects").InsertOne(ctx, bson.M{"_id": "legacy", "code": "LEG", "subjects": bson.A{bson.M{"code": "MAT", "name": "Math"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Collection("rounds").InsertOne(ctx, bson.M{"_id": "r", "projectId": "legacy", "subjectCode": "MAT", "number": 1, "status": "finalized", "statistics": bson.M{"mean": 67.5}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Collection("submissions").InsertOne(ctx, bson.M{"_id": "s", "projectId": "legacy", "subjectCode": "MAT", "roundNumber": 1, "panelistId": "u", "itemId": "i", "answer": bson.M{"value": 70}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Up(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = Up(ctx, db); err != nil {
		t.Fatal(err)
	}
	var p, r, s bson.M
	db.Collection("projects").FindOne(ctx, bson.M{"_id": "legacy"}).Decode(&p)
	db.Collection("rounds").FindOne(ctx, bson.M{"_id": "r"}).Decode(&r)
	db.Collection("submissions").FindOne(ctx, bson.M{"_id": "s"}).Decode(&s)
	if p["featureVersion"] != int32(0) || p["roundingPolicy"] != "none" || p["collectContactPanelis"] != false {
		t.Fatalf("unsafe legacy project defaults: %v", p)
	}
	if r["revision"] != int32(1) || r["status"] != "finalized" || r["statistics"].(bson.M)["mean"] != 67.5 {
		t.Fatalf("historical round changed: %v", r)
	}
	if _, ok := r["finalSnapshot"]; ok {
		t.Fatal("invented legacy final provenance")
	}
	if s["version"] != int32(1) || s["reviewRevision"] != int32(1) || s["answer"].(bson.M)["value"] != int32(70) {
		t.Fatalf("legacy answer corrupted: %v", s)
	}
}
