package store

import (
	"context"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"standard-setting/backend/internal/domain"
)

func (m *Mongo) SaveImpactBatch(ctx context.Context, batch domain.ImpactBatch) error {
	_, err := m.database.Collection("impact_batches").InsertOne(ctx, batch)
	return err
}
func (m *Mongo) ListImpactBatches(ctx context.Context, projectID string) ([]domain.ImpactBatch, error) {
	cur, err := m.database.Collection("impact_batches").Find(ctx, bson.M{"projectId": projectID}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	rows := []domain.ImpactBatch{}
	err = cur.All(ctx, &rows)
	return rows, err
}
