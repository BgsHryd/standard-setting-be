package store

import (
	"context"
	"errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"standard-setting/backend/internal/domain"
	"strings"
	"time"
)

func (m *Mongo) ListSubjects(ctx context.Context) ([]domain.Subject, error) {
	c, err := m.database.Collection("subjects").Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer c.Close(ctx)
	result := []domain.Subject{}
	err = c.All(ctx, &result)
	return result, err
}
func (m *Mongo) CreateSubject(ctx context.Context, s domain.Subject) error {
	_, err := m.database.Collection("subjects").InsertOne(ctx, s)
	if mongo.IsDuplicateKeyError(err) {
		return errors.New("mata pelajaran sudah ada; nama harus unik")
	}
	return err
}
func (m *Mongo) SaveUser(ctx context.Context, u domain.User) error {
	r, err := m.users.ReplaceOne(ctx, bson.M{"_id": u.ID}, u)
	if err == nil && r.MatchedCount != 1 {
		return mongo.ErrNoDocuments
	}
	return err
}

// All roster/configuration and response writes touch this document in their transaction.
func (m *Mongo) TouchProject(ctx context.Context, id string) error {
	r, err := m.projects.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"operationAt": time.Now().UTC()}})
	if err == nil && r.MatchedCount != 1 {
		return mongo.ErrNoDocuments
	}
	return err
}

// EnsureCatalog backfills only knowledge present in existing items/projects, and preserves identifiers.
func EnsureCatalog(ctx context.Context, db *mongo.Database) error {
	if _, err := db.Collection("items").UpdateMany(ctx, bson.M{"projectHistory": nil}, bson.M{"$set": bson.M{"projectHistory": bson.A{}}}); err != nil {
		return err
	}
	entries := map[string]string{"MAT": "Matematika", "BIN": "Bahasa Indonesia", "IPA": "Ilmu Pengetahuan Alam"}
	c, err := db.Collection("items").Find(ctx, bson.M{})
	if err != nil {
		return err
	}
	for c.Next(ctx) {
		var v domain.Item
		if err := c.Decode(&v); err != nil {
			c.Close(ctx)
			return err
		}
		if v.SubjectCode != "" {
			entries[v.SubjectCode] = v.SubjectName
		}
	}
	if err := c.Err(); err != nil {
		c.Close(ctx)
		return err
	}
	c.Close(ctx)
	c, err = db.Collection("projects").Find(ctx, bson.M{})
	if err != nil {
		return err
	}
	for c.Next(ctx) {
		var p domain.Project
		if err := c.Decode(&p); err != nil {
			c.Close(ctx)
			return err
		}
		for _, s := range p.Subjects {
			if s.Code != "" {
				entries[s.Code] = s.Name
			}
		}
	}
	if err := c.Err(); err != nil {
		c.Close(ctx)
		return err
	}
	c.Close(ctx)
	for code, name := range entries {
		name = strings.Join(strings.Fields(name), " ")
		if name == "" {
			name = code
		}
		_, err := db.Collection("subjects").UpdateOne(ctx, bson.M{"_id": code}, bson.M{"$setOnInsert": domain.Subject{Code: code, Name: name, NormalizedName: domain.NormalizeSubjectName(name)}}, options.Update().SetUpsert(true))
		if err != nil {
			return err
		}
	}
	// Retain current ownership as the earliest knowable usage record.
	c, err = db.Collection("items").Find(ctx, bson.M{"projectId": bson.M{"$nin": bson.A{"", nil}}})
	if err != nil {
		return err
	}
	defer c.Close(ctx)
	for c.Next(ctx) {
		var item domain.Item
		if err := c.Decode(&item); err != nil {
			return err
		}
		if item.ProjectID != "" {
			if _, err := db.Collection("items").UpdateOne(ctx, bson.M{"_id": item.ID}, bson.M{"$addToSet": bson.M{"projectHistory": domain.ItemProjectUsage{ProjectID: item.ProjectID, ProjectName: item.ProjectName}}}); err != nil {
				return err
			}
		}
	}
	return c.Err()
}

func (m *Mongo) UpdatePasswordIfVersion(ctx context.Context, id string, version int, hash string, mustChange bool) error {
	filter := bson.M{"_id": id, "active": true, "removed": bson.M{"$ne": true}}
	if version == 0 {
		filter["$or"] = bson.A{bson.M{"authVersion": 0}, bson.M{"authVersion": bson.M{"$exists": false}}}
	} else {
		filter["authVersion"] = version
	}
	r, err := m.users.UpdateOne(ctx, filter, bson.M{"$set": bson.M{"passwordHash": hash, "mustChangePassword": mustChange, "updatedAt": time.Now().UTC()}, "$inc": bson.M{"authVersion": 1}})
	if err == nil && r.MatchedCount != 1 {
		return errors.New("sesi atau password sudah berubah; login kembali")
	}
	return err
}
