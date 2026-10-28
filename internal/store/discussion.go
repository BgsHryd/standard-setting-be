package store

import (
	"context"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"standard-setting/backend/internal/domain"
)

func (m *Mongo) ListDiscussionNotes(ctx context.Context, user, project, subject string, round, revision int) ([]domain.DiscussionNote, error) {
	filter := bson.M{"projectId": project, "subjectCode": subject, "roundNumber": round, "roundRevision": revision}
	if user != "" {
		filter["userId"] = user
	}
	cursor, err := m.database.Collection("panelist_discussion_notes").Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "itemId", Value: 1}, {Key: "userId", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	result := []domain.DiscussionNote{}
	err = cursor.All(ctx, &result)
	return result, err
}

func (m *Mongo) SaveDiscussionNote(ctx context.Context, note domain.DiscussionNote, expected int) error {
	collection := m.database.Collection("panelist_discussion_notes")
	if expected == 0 {
		_, err := collection.InsertOne(ctx, note)
		if mongo.IsDuplicateKeyError(err) {
			return domain.ErrRevisionConflict
		}
		return err
	}
	result, err := collection.ReplaceOne(ctx, bson.M{"_id": note.ID, "version": expected}, note)
	if err == nil && result.MatchedCount != 1 {
		return domain.ErrRevisionConflict
	}
	return err
}
