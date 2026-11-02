package httpapi

import (
	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/service"
)

func (h *Handler) picSubjectConfig(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	result, err := h.service.PICSubjectConfig(c.UserContext(), getClaims(c).Subject)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, result)
}

func (h *Handler) savePICSubjectConfig(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var request struct {
		CandidateDefinition *string `json:"candidateDefinition"`
		ProjectVersion      *int    `json:"projectVersion"`
	}
	if err := c.BodyParser(&request); err != nil || request.CandidateDefinition == nil || request.ProjectVersion == nil || *request.ProjectVersion < 0 {
		return errorResponse(c, 422, "candidateDefinition dan projectVersion wajib diisi dengan nilai yang valid")
	}
	result, err := h.service.SavePICSubjectConfig(c.UserContext(), getClaims(c).Subject, service.PICSubjectConfigInput{CandidateDefinition: *request.CandidateDefinition, ProjectVersion: *request.ProjectVersion})
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, result)
}
