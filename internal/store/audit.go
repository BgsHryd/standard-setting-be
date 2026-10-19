package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"regexp"
	"standard-setting/backend/internal/domain"
	"strings"
	"time"
)

// RecordAudit inserts a new row only; no application API updates or deletes audit rows.
// The caller's Mongo session context is retained so privileged writes can audit atomically.
func (m *Mongo) RecordAudit(ctx context.Context, event domain.AuditEvent) error {
	if event.ID == "" {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return err
		}
		event.ID = hex.EncodeToString(b[:])
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if event.RequestID == "" {
		event.RequestID = domain.AuditRequestID(ctx)
		if event.RequestID == "" {
			event.RequestID = event.ID
		}
	}
	_, err := m.database.Collection("audit_events").InsertOne(ctx, event)
	return err
}
func auditFilter(projectID, search, action string) bson.M {
	f := bson.M{}
	if projectID != "" {
		f["projectId"] = projectID
	}
	if action != "" {
		f["action"] = action
	}
	if search = strings.TrimSpace(search); search != "" {
		if len(search) > 200 {
			search = search[:200]
		}
		r := bson.M{"$regex": regexp.QuoteMeta(search), "$options": "i"}
		f["$or"] = bson.A{bson.M{"actorId": r}, bson.M{"targetId": r}, bson.M{"requestId": r}}
	}
	return f
}
func (m *Mongo) ListAudit(ctx context.Context, projectID, search, action string) ([]domain.AuditEvent, error) {
	filter := auditFilter(projectID, search, action)
	if query := strings.TrimSpace(search); query != "" {
		if len(query) > 200 {
			query = query[:200]
		}
		match := bson.M{"$regex": regexp.QuoteMeta(query), "$options": "i"}
		users, err := m.users.Find(ctx, bson.M{"$or": bson.A{bson.M{"name": match}, bson.M{"username": match}}}, options.Find().SetProjection(bson.M{"_id": 1}).SetLimit(1000))
		if err != nil {
			return nil, err
		}
		ids := []string{}
		for users.Next(ctx) {
			var u struct {
				ID string `bson:"_id"`
			}
			if err := users.Decode(&u); err != nil {
				users.Close(ctx)
				return nil, err
			}
			ids = append(ids, u.ID)
		}
		err = users.Err()
		users.Close(ctx)
		if err != nil {
			return nil, err
		}
		if len(ids) > 0 {
			filter["$or"] = append(filter["$or"].(bson.A), bson.M{"actorId": bson.M{"$in": ids}}, bson.M{"targetId": bson.M{"$in": ids}})
		}
	}
	c, err := m.database.Collection("audit_events").Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).SetLimit(1000))
	if err != nil {
		return nil, err
	}
	defer c.Close(ctx)
	rows := []domain.AuditEvent{}
	err = c.All(ctx, &rows)
	return rows, err
}

// FindAuditUsers returns a safe identity projection for response enrichment, including inactive accounts.
func (m *Mongo) FindAuditUsers(ctx context.Context, ids []string) ([]domain.User, error) {
	result := []domain.User{}
	if len(ids) == 0 {
		return result, nil
	}
	c, err := m.users.Find(ctx, bson.M{"_id": bson.M{"$in": ids}}, options.Find().SetProjection(bson.M{"_id": 1, "name": 1, "username": 1, "role": 1}))
	if err != nil {
		return nil, err
	}
	defer c.Close(ctx)
	err = c.All(ctx, &result)
	return result, err
}
