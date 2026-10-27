package httpapi

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/service"
)

func (h *Handler) registerAccountManagementRoutes(admin fiber.Router, adminRole fiber.Handler) {
	admin.Post("/users", adminRole, h.createParticipantAccount)
	admin.Patch("/users/:id", adminRole, h.editParticipantAccount)
	admin.Delete("/users/:id", adminRole, h.removeParticipantAccount)
}
func participantAccountError(c *fiber.Ctx, err error) error {
	status := fiber.StatusConflict
	if errors.Is(err, service.ErrInvalidParticipantAccount) {
		status = fiber.StatusUnprocessableEntity
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return errorResponse(c, fiber.StatusNotFound, "akun atau proyek tidak ditemukan")
	}
	return errorResponse(c, status, err.Error())
}
func (h *Handler) createParticipantAccount(c *fiber.Ctx) error {
	var input service.ParticipantAccountInput
	if c.BodyParser(&input) != nil || input.ProjectID == "" || input.SubjectCode == "" {
		return errorResponse(c, 422, "data proyek dan mata pelajaran wajib diisi")
	}
	u, err := h.service.CreateParticipantAccount(c.UserContext(), input, getClaims(c).Subject)
	if err != nil {
		return participantAccountError(c, err)
	}
	return success(c, u)
}
func (h *Handler) editParticipantAccount(c *fiber.Ctx) error {
	var input service.ParticipantAccountEdit
	if c.BodyParser(&input) != nil {
		return errorResponse(c, 422, "data akun peserta tidak valid")
	}
	u, err := h.service.UpdateParticipantAccount(c.UserContext(), c.Params("id"), input, getClaims(c).Subject)
	if err != nil {
		return participantAccountError(c, err)
	}
	return success(c, u)
}
func (h *Handler) removeParticipantAccount(c *fiber.Ctx) error {
	version, err := strconv.Atoi(c.Query("projectVersion"))
	if err != nil || version < 0 {
		return errorResponse(c, 422, "versi proyek wajib diisi")
	}
	if c.Query("confirmation") != "HAPUS PESERTA" {
		return errorResponse(c, 422, "konfirmasi penghapusan peserta tidak sesuai")
	}
	if err = h.service.RemoveParticipantAccount(c.UserContext(), c.Params("id"), version, getClaims(c).Subject); err != nil {
		return participantAccountError(c, err)
	}
	return success(c, fiber.Map{"removed": true})
}
