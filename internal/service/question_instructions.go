package service

import (
	"context"
	"errors"
	"slices"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
)

type questionInstructionStore interface {
	GetQuestionInstructions(context.Context) (domain.QuestionInstructionConfig, error)
	SaveQuestionInstructions(context.Context, int, domain.QuestionInstructionConfig) (domain.QuestionInstructionConfig, error)
}

func (s *Service) QuestionInstructions(ctx context.Context) (domain.QuestionInstructionConfig, error) {
	result := domain.QuestionInstructionConfig{
		ID: "question-instructions", Instructions: domain.DefaultQuestionInstructions(),
		AnswerInstructions: domain.DefaultAnswerInstructions(), ShowExamples: domain.DefaultQuestionExamples(),
		ReviewRangeThreshold: domain.DefaultReviewRangeThreshold, PGKRecode: domain.DefaultPGKRecode(),
	}
	result.PGKScoreMappings = []domain.PGKScoreMapping{{OptionCount: 5, Scores: result.PGKRecode}}
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
	if saved.PGKScoreMappings != nil {
		result.PGKScoreMappings = saved.PGKScoreMappings
		result.PGKRecode = legacyPGKRecode(saved.PGKScoreMappings)
	} else if domain.ValidPGKRecode(saved.PGKRecode) {
		result.PGKRecode = saved.PGKRecode
		result.PGKScoreMappings[0].Scores = saved.PGKRecode
	}
	if domain.ValidReviewRangeThreshold(saved.ReviewRangeThreshold) {
		result.ReviewRangeThreshold = saved.ReviewRangeThreshold
	}
	// Prior settings screens saved all fields, including untouched factory copy.
	// Upgrade only the exact retired essay defaults; custom admin wording wins.
	oldEssayAnswer := domain.AnswerInstruction{Title: "Distribusi 100 kandidat berdasarkan skor", Description: "Jumlah seluruh distribusi harus tepat 100."}
	const oldEssayPrompt = `<p>Bayangkan jawaban 100 kandidat batas lulus, lalu gunakan <strong>rubrik penskoran</strong> untuk memperkirakan distribusi skornya.</p><p>Jumlah kandidat pada seluruh skor harus <strong>tepat 100</strong>.</p>`
	for kind := range result.AnswerInstructions {
		if kind == domain.QuestionTypeEssay && saved.AnswerInstructions[kind] == oldEssayAnswer {
			continue
		}
		if value := saved.AnswerInstructions[kind]; domain.ValidAnswerInstruction(value) {
			result.AnswerInstructions[kind] = value
		}
	}
	for kind := range result.ShowExamples {
		result.ShowExamples[kind] = saved.ShowExamples[kind]
	}
	for kind := range result.Instructions {
		if kind == domain.QuestionTypeEssay && saved.Instructions[kind] == oldEssayPrompt {
			continue
		}
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
	return s.SaveQuestionSettings(ctx, actor, expected, content, QuestionInstructionSettings{ShowExamples: flags})
}

type QuestionInstructionSettings struct {
	AnswerInstructions   map[domain.QuestionType]domain.AnswerInstruction
	ShowExamples         map[domain.QuestionType]bool
	ReviewRangeThreshold *int
	PGKRecode            []int
	PGKScoreMappings     []domain.PGKScoreMapping
}

func legacyPGKRecode(mappings []domain.PGKScoreMapping) []int {
	for _, mapping := range mappings {
		if mapping.OptionCount == 5 && domain.ValidPGKRecode(mapping.Scores) {
			return mapping.Scores
		}
	}
	return nil
}

func (s *Service) SaveQuestionInstructionSettings(ctx context.Context, actor string, expected int, content map[domain.QuestionType]string, examples map[domain.QuestionType]bool, threshold *int, answerSettings ...map[domain.QuestionType]domain.AnswerInstruction) (domain.QuestionInstructionConfig, error) {
	settings := QuestionInstructionSettings{ShowExamples: examples, ReviewRangeThreshold: threshold}
	if len(answerSettings) > 0 {
		settings.AnswerInstructions = answerSettings[0]
	}
	return s.SaveQuestionSettings(ctx, actor, expected, content, settings)
}

func (s *Service) SaveQuestionSettings(ctx context.Context, actor string, expected int, content map[domain.QuestionType]string, settings QuestionInstructionSettings) (domain.QuestionInstructionConfig, error) {
	var result domain.QuestionInstructionConfig
	threshold := settings.ReviewRangeThreshold
	if threshold != nil && !domain.ValidReviewRangeThreshold(*threshold) {
		return result, errors.New("ambang penanda butir harus angka bulat 1–100 poin persentase")
	}
	if expected < 0 {
		return result, errors.New("versi konfigurasi tidak valid")
	}
	if err := domain.ValidatePGKScoreMappings(settings.PGKScoreMappings); err != nil {
		return result, err
	}
	if settings.PGKScoreMappings == nil && settings.PGKRecode != nil && !domain.ValidPGKRecode(settings.PGKRecode) {
		return result, errors.New("pemetaan PGK harus berisi enam skor berurutan dari 0 hingga 2 dan memuat skor 1")
	}
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		user, err := s.store.FindByID(tx, actor)
		if err != nil || !user.Active || user.Removed || !user.Role.IsAdministrative() {
			return errors.New("hanya admin aktif dapat mengubah petunjuk")
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
		answerInstructions := current.AnswerInstructions
		if supplied := settings.AnswerInstructions; supplied != nil {
			if len(supplied) != len(answerInstructions) {
				return errors.New("instruksi pengisian seluruh jenis soal wajib diisi")
			}
			answerInstructions = make(map[domain.QuestionType]domain.AnswerInstruction, len(supplied))
			for kind := range current.AnswerInstructions {
				value := supplied[kind]
				if !domain.ValidAnswerInstruction(value) {
					return errors.New("judul instruksi wajib diisi maksimal 200 karakter dan teks bantuan wajib diisi maksimal 1.000 karakter")
				}
				answerInstructions[kind] = value
			}
		}
		effectiveThreshold := current.ReviewRangeThreshold
		if threshold != nil {
			effectiveThreshold = *threshold
		}
		flags := domain.DefaultQuestionExamples()
		if examples := settings.ShowExamples; examples != nil {
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
		mappings := current.PGKScoreMappings
		if settings.PGKScoreMappings != nil {
			mappings = settings.PGKScoreMappings
		} else if settings.PGKRecode != nil && current.PGKRecode != nil {
			mappings = slices.Clone(mappings)
			index := slices.IndexFunc(mappings, func(mapping domain.PGKScoreMapping) bool {
				return mapping.OptionCount == 5
			})
			mappings[index].Scores = settings.PGKRecode
		}
		result, err = st.SaveQuestionInstructions(tx, expected, domain.QuestionInstructionConfig{
			ID: "question-instructions", Instructions: clean, AnswerInstructions: answerInstructions,
			ReviewRangeThreshold: effectiveThreshold, ShowExamples: flags, UpdatedAt: time.Now().UTC(),
			PGKRecode: legacyPGKRecode(mappings), PGKScoreMappings: mappings,
		})
		if err != nil {
			return err
		}
		return s.recordAudit(tx, actor, "Petunjuk jenis soal", "", "question_instructions_update")
	})
	return result, err
}
