package store

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// EnsureIndexes is shared by application startup and the initial migration.
func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	definitions := []struct {
		collection string
		models     []mongo.IndexModel
	}{
		{"users", []mongo.IndexModel{{Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetUnique(true)}, {Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetName("username_casefold").SetUnique(true).SetCollation(&options.Collation{Locale: "en", Strength: 2})}, {Keys: bson.D{{Key: "projectId", Value: 1}, {Key: "role", Value: 1}}}}},
		{"subjects", []mongo.IndexModel{{Keys: bson.D{{Key: "normalizedName", Value: 1}}, Options: options.Index().SetUnique(true)}}},
		{"projects", []mongo.IndexModel{{Keys: bson.D{{Key: "subjects.itemIds", Value: 1}}}, {Keys: bson.D{{Key: "code", Value: 1}}, Options: options.Index().SetUnique(true)}, {Keys: bson.D{{Key: "status", Value: 1}}}}},
		{"items", []mongo.IndexModel{{Keys: bson.D{{Key: "subjectCode", Value: 1}, {Key: "usageStatus", Value: 1}}}, {Keys: bson.D{{Key: "projectId", Value: 1}}}}},
		{"rounds", []mongo.IndexModel{{Keys: bson.D{{Key: "projectId", Value: 1}, {Key: "subjectCode", Value: 1}, {Key: "number", Value: 1}}, Options: options.Index().SetUnique(true)}}},
		{"submissions", []mongo.IndexModel{{Keys: bson.D{{Key: "projectId", Value: 1}, {Key: "subjectCode", Value: 1}, {Key: "roundNumber", Value: 1}, {Key: "panelistId", Value: 1}, {Key: "itemId", Value: 1}}, Options: options.Index().SetUnique(true)}}},
		{"audit_events", []mongo.IndexModel{
			{Keys: bson.D{{Key: "actorId", Value: 1}, {Key: "createdAt", Value: -1}}},
			{Keys: bson.D{{Key: "targetId", Value: 1}, {Key: "createdAt", Value: -1}}},
			{Keys: bson.D{{Key: "projectId", Value: 1}, {Key: "createdAt", Value: -1}}},
			{Keys: bson.D{{Key: "createdAt", Value: -1}}},
		}},
		// PanelistContact.UserID is BSON _id, which MongoDB already indexes uniquely.
		{"panelist_contacts", []mongo.IndexModel{{Keys: bson.D{{Key: "projectId", Value: 1}, {Key: "subjectCode", Value: 1}}}}},
		{"evaluations", []mongo.IndexModel{{Keys: bson.D{{Key: "projectId", Value: 1}, {Key: "userId", Value: 1}, {Key: "instrumentVersion", Value: 1}}, Options: options.Index().SetUnique(true)}}},
		{"panelist_telemetry", []mongo.IndexModel{{Keys: bson.D{{Key: "projectId", Value: 1}, {Key: "subjectCode", Value: 1}, {Key: "roundNumber", Value: 1}, {Key: "roundRevision", Value: 1}}}}},
		{"practice_sessions", []mongo.IndexModel{{Keys: bson.D{{Key: "projectId", Value: 1}, {Key: "subjectCode", Value: 1}}}}},
		{"impact_batches", []mongo.IndexModel{{Keys: bson.D{{Key: "projectId", Value: 1}, {Key: "createdAt", Value: -1}}}}},
		{"impact_scores", []mongo.IndexModel{{Keys: bson.D{{Key: "batchId", Value: 1}, {Key: "subjectCode", Value: 1}, {Key: "participantId", Value: 1}}, Options: options.Index().SetUnique(true)}}},
	}
	for _, definition := range definitions {
		if _, err := db.Collection(definition.collection).Indexes().CreateMany(ctx, definition.models); err != nil {
			return fmt.Errorf("index %s: %w", definition.collection, err)
		}
	}
	return nil
}
