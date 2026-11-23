package httpapi

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
)

func (h *Handler) registerAccountManagementRoutes(admin fiber.Router, adminRole fiber.Handler) {
	admin.Get("/users", adminRole, h.listUsers)
	admin.Post("/users", adminRole, h.createAccount)
	admin.Patch("/users/:id", adminRole, h.editParticipantAccount)
	admin.Put("/users/:id", adminRole, h.editManagedAccount)
	admin.Delete("/users/:id", adminRole, h.removeAccount)
	admin.Patch("/users/:id/active", adminRole, h.userActive)
	admin.Post("/users/:id/reset-password", adminRole, h.resetPassword)
	admin.Put("/users/:id/monitoring-project", adminRole, h.setMonitoringProject)
}
func accountMutationError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, service.ErrAccountAccessDenied):
		return errorResponse(c, fiber.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrInvalidParticipantAccount), errors.Is(err, service.ErrInvalidManagedAccount):
		return errorResponse(c, fiber.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, mongo.ErrNoDocuments):
		return errorResponse(c, fiber.StatusNotFound, "akun atau proyek tidak ditemukan")
	default:
		return errorResponse(c, fiber.StatusConflict, err.Error())
	}
}
func (h *Handler) listUsers(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	users, err := h.service.ListUsersAs(c.UserContext(), c.Query("projectId"), getClaims(c).Subject)
	if errors.Is(err, service.ErrAccountAccessDenied) {
		return errorResponse(c, fiber.StatusForbidden, err.Error())
	}
	if err != nil {
		return err
	}
	return success(c, users)
}
func (h *Handler) createAccount(c *fiber.Ctx) error {
	var input service.ParticipantAccountInput
	if c.BodyParser(&input) != nil {
		return errorResponse(c, 422, "data akun tidak valid")
	}
	switch input.Role {
	case domain.RoleAdmin, domain.RoleUserMonitoring, domain.RoleAdminSuper:
		if input.ProjectID != "" || input.SubjectCode != "" || input.SlotID != "" {
			return errorResponse(c, 422, "tentukan penugasan monitoring setelah akun dibuat")
		}
		if err := h.requireService(c); err != nil {
			return err
		}
		u, err := h.service.CreateManagedAccount(c.UserContext(), service.ManagedAccountInput{
			Name: input.Name, Username: input.Username, Role: input.Role,
		}, getClaims(c).Subject)
		if err != nil {
			return accountMutationError(c, err)
		}
		return success(c, u)
	case domain.RolePICMapel, domain.RolePanelist:
		if input.ProjectID == "" || input.SubjectCode == "" {
			return errorResponse(c, 422, "data proyek dan mata pelajaran wajib diisi")
		}
	default:
		return errorResponse(c, 422, "role akun tidak valid")
	}
	if err := h.requireService(c); err != nil {
		return err
	}
	u, err := h.service.CreateParticipantAccount(c.UserContext(), input, getClaims(c).Subject)
	if err != nil {
		return accountMutationError(c, err)
	}
	return success(c, u)
}
func (h *Handler) editParticipantAccount(c *fiber.Ctx) error {
	var input service.ParticipantAccountEdit
	if c.BodyParser(&input) != nil {
		return errorResponse(c, 422, "data akun peserta tidak valid")
	}
	if err := h.requireService(c); err != nil {
		return err
	}
	u, err := h.service.UpdateParticipantAccount(c.UserContext(), c.Params("id"), input, getClaims(c).Subject)
	if err != nil {
		return accountMutationError(c, err)
	}
	return success(c, u)
}
func (h *Handler) editManagedAccount(c *fiber.Ctx) error {
	var input service.ManagedAccountIdentity
	if c.BodyParser(&input) != nil {
		return errorResponse(c, 422, "data akun tidak valid")
	}
	if err := h.requireService(c); err != nil {
		return err
	}
	u, err := h.service.UpdateManagedAccount(c.UserContext(), c.Params("id"), input, getClaims(c).Subject)
	if err != nil {
		return accountMutationError(c, err)
	}
	return success(c, u)
}
func (h *Handler) removeAccount(c *fiber.Ctx) error {
	confirmation := c.Query("confirmation")
	if confirmation != "HAPUS AKUN" && confirmation != "HAPUS PESERTA" {
		return errorResponse(c, 422, "konfirmasi penghapusan akun tidak sesuai")
	}
	var version int
	if confirmation == "HAPUS PESERTA" {
		var err error
		version, err = strconv.Atoi(c.Query("projectVersion"))
		if err != nil || version < 0 {
			return errorResponse(c, 422, "versi proyek wajib diisi")
		}
	}
	if err := h.requireService(c); err != nil {
		return err
	}
	var err error
	if confirmation == "HAPUS PESERTA" {
		err = h.service.RemoveParticipantAccount(c.UserContext(), c.Params("id"), version, getClaims(c).Subject)
	} else {
		err = h.service.RemoveManagedAccount(c.UserContext(), c.Params("id"), getClaims(c).Subject)
	}
	if err != nil {
		return accountMutationError(c, err)
	}
	return success(c, fiber.Map{"removed": true})
}

func (h *Handler) setMonitoringProject(c *fiber.Ctx) error {
	var input struct {
		ProjectID *string `json:"projectId"`
	}
	if c.BodyParser(&input) != nil || input.ProjectID == nil {
		return errorResponse(c, 422, "projectId wajib diisi")
	}
	if err := h.requireService(c); err != nil {
		return err
	}
	u, err := h.service.SetMonitoringProject(c.UserContext(), c.Params("id"), *input.ProjectID, getClaims(c).Subject)
	if err != nil {
		return accountMutationError(c, err)
	}
	return success(c, u)
}
