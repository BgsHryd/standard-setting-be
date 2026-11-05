package store

import (
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
)

func TestQuestionInstructionsMongoPersistenceAndCAS(t *testing.T) {
	ctx, m := securityMongo(t)
	value := domain.QuestionInstructionConfig{AnswerInstructions: domain.DefaultAnswerInstructions(), Instructions: domain.DefaultQuestionInstructions(), ShowExamples: map[domain.QuestionType]bool{domain.QuestionTypeComplexMCMA: true, domain.QuestionTypeComplexBS: false}, UpdatedAt: time.Now().UTC()}
	saved, err := m.SaveQuestionInstructions(ctx, 0, value)
	if err != nil || saved.Version != 1 {
		t.Fatal(saved, err)
	}
	reloaded, err := m.GetQuestionInstructions(ctx)
	if err != nil || reloaded.AnswerInstructions[domain.QuestionTypePG] != value.AnswerInstructions[domain.QuestionTypePG] || reloaded.Instructions[domain.QuestionTypePG] != value.Instructions[domain.QuestionTypePG] || !reloaded.ShowExamples[domain.QuestionTypeComplexMCMA] || reloaded.ShowExamples[domain.QuestionTypeComplexBS] {
		t.Fatal(reloaded, err)
	}
	if _, err = m.SaveQuestionInstructions(ctx, 0, value); !errors.Is(err, domain.ErrInstructionConflict) {
		t.Fatal("stale create accepted", err)
	}
	value.Instructions[domain.QuestionTypePG] = "<p>Diubah</p>"
	saved, err = m.SaveQuestionInstructions(ctx, 1, value)
	if err != nil || saved.Version != 2 || saved.Instructions[domain.QuestionTypePG] != "<p>Diubah</p>" {
		t.Fatal(saved, err)
	}
	if _, err = m.SaveQuestionInstructions(ctx, 1, value); !errors.Is(err, domain.ErrInstructionConflict) {
		t.Fatal("stale update accepted", err)
	}
}

func TestQuestionInstructionsMongoServiceSaveAuditsAtomically(t *testing.T) {
	ctx, m := securityMongo(t)
	_, err := m.users.InsertOne(ctx, domain.User{ID: "qa-admin", Role: domain.RoleAdminSuper, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	s := service.New(m, nil, "")
	saved, err := s.SaveQuestionInstructions(ctx, "qa-admin", 0, domain.DefaultQuestionInstructions())
	if err != nil || saved.Version != 1 {
		t.Fatal(saved, err)
	}
	if _, err = s.SaveQuestionInstructions(ctx, "qa-admin", 0, domain.DefaultQuestionInstructions()); !errors.Is(err, domain.ErrInstructionConflict) {
		t.Fatal("stale write accepted", err)
	}
	count, err := m.database.Collection("audit_events").CountDocuments(ctx, bson.M{"action": "question_instructions_update"})
	if err != nil || count != 1 {
		t.Fatal("audit must match successful writes only", count, err)
	}
	reloaded, err := s.QuestionInstructions(ctx)
	if err != nil || reloaded.Version != 1 {
		t.Fatal(reloaded, err)
	}
}

func TestReviewThresholdMongoSaveAndLegacyPreservation(t *testing.T) {
	ctx, m := securityMongo(t)
	if _, err := m.users.InsertOne(ctx, domain.User{ID: "threshold-admin", Role: domain.RoleAdminSuper, Active: true}); err != nil {
		t.Fatal(err)
	}
	s := service.New(m, nil, "")
	threshold := 17
	saved, err := s.SaveQuestionInstructionSettings(ctx, "threshold-admin", 0, domain.DefaultQuestionInstructions(), nil, &threshold)
	if err != nil || saved.ReviewRangeThreshold != 17 {
		t.Fatal(saved, err)
	}
	read, err := m.GetQuestionInstructions(ctx)
	if err != nil || read.ReviewRangeThreshold != 17 {
		t.Fatal("threshold not persisted in Mongo", read, err)
	}
	if _, err = s.SaveQuestionInstructions(ctx, "threshold-admin", saved.Version, saved.Instructions); err != nil {
		t.Fatal(err)
	}
	read, err = m.GetQuestionInstructions(ctx)
	if err != nil || read.ReviewRangeThreshold != 17 {
		t.Fatal("legacy HTML save overwrote Mongo threshold", read, err)
	}
}
