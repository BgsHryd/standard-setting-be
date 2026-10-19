package store

import (
	"context"
	"errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"standard-setting/backend/internal/domain"
)

func (m *Mongo) GetEvaluation(ctx context.Context, id string) (domain.Evaluation, error) {
	var row domain.Evaluation
	err := m.database.Collection("evaluations").FindOne(ctx, bson.M{"_id": id}).Decode(&row)
	return row, err
}
func (m *Mongo) SaveEvaluation(ctx context.Context, row domain.Evaluation, expected int) error {
	c := m.database.Collection("evaluations")
	if expected == 0 {
		_, err := c.InsertOne(ctx, row)
		return err
	}
	result, err := c.ReplaceOne(ctx, bson.M{"_id": row.ID, "version": expected}, row)
	if err == nil && result.MatchedCount != 1 {
		return errors.New("versi evaluasi berubah; muat ulang")
	}
	return err
}
func (m *Mongo) ListEvaluations(ctx context.Context, projectID string) ([]domain.Evaluation, error) {
	cur, err := m.database.Collection("evaluations").Find(ctx, bson.M{"projectId": projectID, "instrumentVersion": 1}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	rows := []domain.Evaluation{}
	err = cur.All(ctx, &rows)
	return rows, err
}
