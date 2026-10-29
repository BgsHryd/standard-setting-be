package service

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
)

type questionInstructionStore interface {
	GetQuestionInstructions(context.Context) (domain.QuestionInstructionConfig, error)
	SaveQuestionInstructions(context.Context, int, domain.QuestionInstructionConfig) (domain.QuestionInstructionConfig, error)
}

func (s *Service) QuestionInstructions(ctx context.Context) (domain.QuestionInstructionConfig, error) {
	result := domain.QuestionInstructionConfig{ID: "question-instructions", Instructions: domain.DefaultQuestionInstructions()}
	st, ok := s.store.(questionInstructionStore)
	if !ok {
		return result, nil
	}
	saved, err := st.GetQuestionInstructions(ctx)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Version, result.UpdatedAt = saved.Version, saved.UpdatedAt
	for kind := range result.Instructions {
		if html, e := domain.SanitizeQuestionInstruction(saved.Instructions[kind]); e == nil {
			result.Instructions[kind] = html
		}
	}
	return result, nil
}

func (s *Service) SaveQuestionInstructions(ctx context.Context, actor string, expected int, content map[domain.QuestionType]string) (domain.QuestionInstructionConfig, error) {
	var result domain.QuestionInstructionConfig
	if expected < 0 {
		return result, errors.New("versi konfigurasi tidak valid")
	}
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		user, err := s.store.FindByID(tx, actor)
		if err != nil || !user.Active || user.Removed || user.Role != domain.RoleAdminSuper {
			return errors.New("hanya Admin Super aktif dapat mengubah petunjuk")
		}
		known := domain.DefaultQuestionInstructions()
		if len(content) != len(known) {
			return errors.New("petunjuk seluruh jenis soal wajib diisi")
		}
		clean := make(map[domain.QuestionType]string, len(known))
		for kind := range known {
			value, e := domain.SanitizeQuestionInstruction(content[kind])
			if e != nil {
				return e
			}
			clean[kind] = value
		}
		st, ok := s.store.(questionInstructionStore)
		if !ok {
			return errors.New("penyimpanan konfigurasi tidak tersedia")
		}
		result, err = st.SaveQuestionInstructions(tx, expected, domain.QuestionInstructionConfig{ID: "question-instructions", Instructions: clean, UpdatedAt: time.Now().UTC()})
		if err != nil {
			return err
		}
		return s.recordAudit(tx, actor, "Petunjuk jenis soal", "", "question_instructions_update")
	})
	return result, err
}
