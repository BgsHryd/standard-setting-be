package httpapi

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/domain"
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
	result, err := h.service.SaveQuestionInstructionSettings(c.UserContext(), getClaims(c).Subject, *request.Version, request.Instructions, flags, request.ReviewRangeThreshold, request.AnswerInstructions)
	if errors.Is(err, domain.ErrInstructionConflict) {
		return errorResponse(c, 409, err.Error())
	}
	if err != nil {
		return errorResponse(c, 422, err.Error())
	}
	return success(c, result)
}
