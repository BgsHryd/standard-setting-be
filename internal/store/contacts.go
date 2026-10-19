package store

import (
	"context"
	"errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
	"time"
)

func (m *Mongo) GetContact(ctx context.Context, id string) (domain.PanelistContact, error) {
	var c domain.PanelistContact
	err := m.database.Collection("panelist_contacts").FindOne(ctx, bson.M{"_id": id}).Decode(&c)
	return c, err
}
func (m *Mongo) SaveContact(ctx context.Context, c domain.PanelistContact, expected int) error {
	collection := m.database.Collection("panelist_contacts")
	if expected == 0 {
		_, err := collection.InsertOne(ctx, c)
		if mongo.IsDuplicateKeyError(err) {
			return domain.ErrRevisionConflict
		}
		return err
	}
	r, err := collection.ReplaceOne(ctx, bson.M{"_id": c.UserID, "version": expected}, c)
	if err == nil && r.MatchedCount != 1 {
		return domain.ErrRevisionConflict
	}
	return err
}
func (m *Mongo) RequiresContact(ctx context.Context, id string) (bool, error) {
	u, err := m.FindByID(ctx, id)
	if err != nil {
		return false, err
	}
	if u.Role != domain.RolePanelist {
		return false, nil
	}
	p, err := m.GetProject(ctx, u.ProjectID)
	if err != nil {
		return false, err
	}
	if !p.CollectContactPanelis {
		return false, nil
	}
	c, err := m.GetContact(ctx, id)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	_, _, err = domain.NormalizeContact(c.WhatsApp, c.Email)
	return err != nil, nil
}

// Change logging shares the caller's Mongo session when invoked in a transaction.
func (m *Mongo) TouchProjectVersion(ctx context.Context, id string, version int) error {
	r, err := m.projects.UpdateOne(ctx, bson.M{"_id": id, "version": version}, bson.M{"$set": bson.M{"operationAt": time.Now().UTC()}})
	if err == nil && r.MatchedCount != 1 {
		return domain.ErrRevisionConflict
	}
	return err
}
