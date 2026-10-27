package store

import (
	"context"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"standard-setting/backend/internal/domain"
)

func (m *Mongo) SaveTelemetry(ctx context.Context, row domain.PanelistTelemetry) error {
	_, err := m.database.Collection("panelist_telemetry").ReplaceOne(ctx, bson.M{"_id": row.ID}, row, options.Replace().SetUpsert(true))
	return err
}
func (m *Mongo) ListTelemetry(ctx context.Context, project, subject string, number, revision int) ([]domain.PanelistTelemetry, error) {
	cursor, err := m.database.Collection("panelist_telemetry").Find(ctx, bson.M{"projectId": project, "subjectCode": subject, "roundNumber": number, "roundRevision": revision})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	rows := []domain.PanelistTelemetry{}
	err = cursor.All(ctx, &rows)
	return rows, err
}
