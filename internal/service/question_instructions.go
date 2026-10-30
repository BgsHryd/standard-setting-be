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
	result := domain.QuestionInstructionConfig{ReviewRangeThreshold: domain.DefaultReviewRangeThreshold, ID: "question-instructions", Instructions: domain.DefaultQuestionInstructions(), ShowExamples: domain.DefaultQuestionExamples()}
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
	if domain.ValidReviewRangeThreshold(saved.ReviewRangeThreshold) {
		result.ReviewRangeThreshold = saved.ReviewRangeThreshold
	}
	for kind := range result.ShowExamples {
		result.ShowExamples[kind] = saved.ShowExamples[kind]
	}
	for kind := range result.Instructions {
		if html, e := domain.SanitizeQuestionInstruction(saved.Instructions[kind]); e == nil {
			result.Instructions[kind] = html
		}
	}
	return result, nil
}

func (s *Service) SaveQuestionInstructions(ctx context.Context, actor string, expected int, content map[domain.QuestionType]string, examples ...map[domain.QuestionType]bool) (domain.QuestionInstructionConfig, error) {
	var flags map[domain.QuestionType]bool
	if len(examples) > 0 {
		flags = examples[0]
	}
	return s.SaveQuestionInstructionSettings(ctx, actor, expected, content, flags, nil)
}

func (s *Service) SaveQuestionInstructionSettings(ctx context.Context, actor string, expected int, content map[domain.QuestionType]string, examples map[domain.QuestionType]bool, threshold *int) (domain.QuestionInstructionConfig, error) {
	var result domain.QuestionInstructionConfig
	if threshold != nil && !domain.ValidReviewRangeThreshold(*threshold) {
		return result, errors.New("ambang penanda butir harus angka bulat 1–100 poin persentase")
	}
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
		current, readErr := s.QuestionInstructions(tx)
		if readErr != nil {
			return readErr
		}
		effectiveThreshold := current.ReviewRangeThreshold
		if threshold != nil {
			effectiveThreshold = *threshold
		}
		flags := domain.DefaultQuestionExamples()
		if examples != nil {
			for kind, enabled := range examples {
				if _, allowed := flags[kind]; !allowed {
					return errors.New("tampilkan_soal_contoh hanya tersedia untuk PGK MCMA dan BS")
				}
				flags[kind] = enabled
			}
		} else {
			// HTML-only clients must preserve the existing example configuration.
			flags = current.ShowExamples
		}
		result, err = st.SaveQuestionInstructions(tx, expected, domain.QuestionInstructionConfig{ReviewRangeThreshold: effectiveThreshold, ID: "question-instructions", Instructions: clean, ShowExamples: flags, UpdatedAt: time.Now().UTC()})
		if err != nil {
			return err
		}
		return s.recordAudit(tx, actor, "Petunjuk jenis soal", "", "question_instructions_update")
	})
	return result, err
}
