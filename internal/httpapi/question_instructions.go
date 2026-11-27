package httpapi

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
)

func (h *Handler) getQuestionInstructions(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	result, err := h.service.QuestionInstructions(c.UserContext())
	if err != nil {
		return errorResponse(c, 503, "Petunjuk belum dapat dimuat")
	}
	return success(c, result)
}

func (h *Handler) saveQuestionInstructions(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var request struct {
		PGKScoreMappings []struct {
			OptionCount int    `json:"optionCount"`
			Scores      []*int `json:"scores"`
		} `json:"pgkScoreMappings"`
		PGKRecode            []*int                                           `json:"pgkRecode"`
		AnswerInstructions   map[domain.QuestionType]domain.AnswerInstruction `json:"answerInstructions"`
		ReviewRangeThreshold *int                                             `json:"reviewRangeThreshold"`
		Version              *int                                             `json:"version"`
		Instructions         map[domain.QuestionType]string                   `json:"instructions"`
		ShowExamples         map[domain.QuestionType]*bool                    `json:"tampilkan_soal_contoh"`
	}
	if err := c.BodyParser(&request); err != nil || request.Version == nil || *request.Version < 0 || request.Instructions == nil {
		return errorResponse(c, 422, "versi dan petunjuk seluruh jenis soal wajib diisi")
	}
	var flags map[domain.QuestionType]bool
	if request.ShowExamples != nil {
		flags = make(map[domain.QuestionType]bool, len(request.ShowExamples))
		for kind, enabled := range request.ShowExamples {
			if enabled == nil {
				return errorResponse(c, 422, "tampilkan_soal_contoh harus bernilai true atau false")
			}
			flags[kind] = *enabled
		}
	}
	var mappings []domain.PGKScoreMapping
	if request.PGKScoreMappings != nil {
		mappings = make([]domain.PGKScoreMapping, 0, len(request.PGKScoreMappings))
		for _, mapping := range request.PGKScoreMappings {
			scores, valid := pgkScores(mapping.Scores)
			if !valid {
				return errorResponse(c, 422, "setiap skor dalam pemetaan PGK harus berupa angka bulat")
			}
			mappings = append(mappings, domain.PGKScoreMapping{OptionCount: mapping.OptionCount, Scores: scores})
		}
	}
	var recode []int
	if mappings == nil {
		var valid bool
		recode, valid = pgkScores(request.PGKRecode)
		if !valid {
			return errorResponse(c, 422, "setiap skor dalam pemetaan PGK harus berupa angka bulat")
		}
	}
	result, err := h.service.SaveQuestionSettings(c.UserContext(), getClaims(c).Subject, *request.Version, request.Instructions, service.QuestionInstructionSettings{
		AnswerInstructions:   request.AnswerInstructions,
		ShowExamples:         flags,
		ReviewRangeThreshold: request.ReviewRangeThreshold,
		PGKRecode:            recode,
		PGKScoreMappings:     mappings,
	})
	if errors.Is(err, domain.ErrInstructionConflict) {
		return errorResponse(c, 409, err.Error())
	}
	if err != nil {
		return errorResponse(c, 422, err.Error())
	}
	return success(c, result)
}

func pgkScores(values []*int) ([]int, bool) {
	if values == nil {
		return nil, true
	}
	scores := make([]int, len(values))
	for i, value := range values {
		if value == nil {
			return nil, false
		}
		scores[i] = *value
	}
	return scores, true
}
