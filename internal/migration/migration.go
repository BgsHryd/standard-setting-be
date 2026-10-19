package migration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"standard-setting/backend/internal/store"
)

type Entry struct {
	Version   int        `json:"version"`
	Name      string     `json:"name"`
	Applied   bool       `json:"applied"`
	AppliedAt *time.Time `json:"appliedAt,omitempty"`
}

func Status(ctx context.Context, db *mongo.Database) ([]Entry, error) {
	entries := []Entry{{Version: 1, Name: "initial_schema"}, {Version: 2, Name: "admin_catalog_and_history"}, {Version: 3, Name: "round_revisions_contacts_audit"}}
	for i := range entries {
		var applied struct {
			Name      string    `bson:"name"`
			AppliedAt time.Time `bson:"appliedAt"`
		}
		err := db.Collection("schema_migrations").FindOne(ctx, bson.M{"_id": entries[i].Version}).Decode(&applied)
		if errors.Is(err, mongo.ErrNoDocuments) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if applied.Name != entries[i].Name || applied.AppliedAt.IsZero() {
			return nil, fmt.Errorf("ledger migration %d tidak sesuai", entries[i].Version)
		}
		entries[i].Applied = true
		entries[i].AppliedAt = &applied.AppliedAt
	}
	return entries, nil
}
func Up(ctx context.Context, db *mongo.Database) ([]Entry, error) {
	entries, err := Status(ctx, db)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Applied {
			continue
		}
		if entry.Version == 1 {
			for _, name := range []string{"users", "projects", "items", "rounds", "submissions"} {
				if err := db.CreateCollection(ctx, name); err != nil {
					var command mongo.CommandError
					if !errors.As(err, &command) || command.Code != 48 {
						return nil, err
					}
				}
			}
		}
		if err := store.EnsureIndexes(ctx, db); err != nil {
			return nil, err
		}
		if entry.Version == 2 {
			if err := store.EnsureCatalog(ctx, db); err != nil {
				return nil, err
			}
		}
		if entry.Version == 3 {
			if err := featureDefaults(ctx, db); err != nil {
				return nil, err
			}
		}
		if _, err := db.Collection("schema_migrations").UpdateOne(ctx, bson.M{"_id": entry.Version}, bson.M{"$setOnInsert": bson.M{"name": entry.Name, "appliedAt": time.Now().UTC()}}, options.Update().SetUpsert(true)); err != nil {
			return nil, err
		}
	}
	return Status(ctx, db)
}

// Missing legacy fields receive conservative defaults. Existing values, dates,
// answers and final aggregates are retained; historical snapshots are never invented.
func featureDefaults(ctx context.Context, db *mongo.Database) error {
	defaults := map[string]bson.M{
		"projects":    {"featureVersion": 0, "collectContactPanelis": false, "roundingPolicy": "none", "evaluationClosed": false, "sourceProjectId": ""},
		"rounds":      {"revision": 1, "excludedPanelists": bson.A{}, "snapshots": bson.A{}},
		"submissions": {"version": 1, "roundRevision": 1, "reviewRevision": 1, "candidateVersion": 0, "note": "", "idempotencyKey": ""},
	}
	for collection, fields := range defaults {
		for field, value := range fields {
			if _, err := db.Collection(collection).UpdateMany(ctx, bson.M{field: bson.M{"$exists": false}}, bson.M{"$set": bson.M{field: value}}); err != nil {
				return err
			}
		}
	}
	// A pipeline merges defaults before each existing subject, preserving all fields.
	_, err := db.Collection("projects").UpdateMany(ctx, bson.M{"subjects": bson.M{"$type": "array"}}, mongo.Pipeline{bson.D{{Key: "$set", Value: bson.M{"subjects": bson.M{"$map": bson.M{"input": "$subjects", "as": "subject", "in": bson.M{"$mergeObjects": bson.A{bson.M{"candidateDefinition": "", "candidateVersion": 0}, "$$subject"}}}}}}}})
	return err
}
