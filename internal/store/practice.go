package store

import (
	"context"
	"errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
)

func (m *Mongo) GetPracticeSession(ctx context.Context, id string) (domain.PracticeSession, error) {
	var row domain.PracticeSession
	err := m.database.Collection("practice_sessions").FindOne(ctx, bson.M{"_id": id}).Decode(&row)
	return row, err
}
func (m *Mongo) SavePracticeSession(ctx context.Context, row domain.PracticeSession, expected int) error {
	collection := m.database.Collection("practice_sessions")
	if expected == 0 {
		_, err := collection.InsertOne(ctx, row)
		if mongo.IsDuplicateKeyError(err) {
			return domain.ErrRevisionConflict
		}
		return err
	}
	result, err := collection.ReplaceOne(ctx, bson.M{"_id": row.ID, "version": expected}, row)
	if err == nil && result.MatchedCount != 1 {
		return domain.ErrRevisionConflict
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return domain.ErrRevisionConflict
	}
	return err
}
