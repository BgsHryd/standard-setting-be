package store

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"standard-setting/backend/internal/domain"
)

func (m *Mongo) GetQuestionInstructions(ctx context.Context) (domain.QuestionInstructionConfig, error) {
	var result domain.QuestionInstructionConfig
	err := m.database.Collection("app_config").FindOne(ctx, bson.M{"_id": "question-instructions"}).Decode(&result)
	return result, err
}

func (m *Mongo) SaveQuestionInstructions(ctx context.Context, expected int, value domain.QuestionInstructionConfig) (domain.QuestionInstructionConfig, error) {
	var result domain.QuestionInstructionConfig
	err := m.database.Collection("app_config").FindOneAndUpdate(ctx,
		bson.M{"_id": "question-instructions", "version": expected},
		bson.M{"$set": bson.M{"answerInstructions": value.AnswerInstructions, "reviewRangeThreshold": value.ReviewRangeThreshold, "instructions": value.Instructions, "tampilkan_soal_contoh": value.ShowExamples, "updatedAt": value.UpdatedAt}, "$inc": bson.M{"version": 1}},
		options.FindOneAndUpdate().SetUpsert(expected == 0).SetReturnDocument(options.After)).Decode(&result)
	if errors.Is(err, mongo.ErrNoDocuments) || mongo.IsDuplicateKeyError(err) {
		return result, domain.ErrInstructionConflict
	}
	return result, err
}
