package httpapi

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
)

func (h *Handler) registerWrightMapRoutes(protected fiber.Router) {
	protected.Get("/projects/:id/wright-maps/:subjectCode", h.requireRoles(domain.RoleAdminSuper, domain.RolePICMapel, domain.RolePanelist), h.getWrightMap)
	protected.Put("/projects/:id/wright-maps/:subjectCode", h.requireRoles(domain.RoleAdminSuper), h.putWrightMap)
}

func wrightMapError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrWrightMapForbidden):
		return errorResponse(c, 403, err.Error())
	case errors.Is(err, domain.ErrWrightMapNotFound):
		return errorResponse(c, 404, err.Error())
	case errors.Is(err, domain.ErrWrightMapConflict):
		return errorResponse(c, 409, err.Error())
	default:
		return errorResponse(c, 422, err.Error())
	}
}

func (h *Handler) getWrightMap(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	result, err := h.service.WrightMap(c.UserContext(), getClaims(c).Subject, c.Params("id"), c.Params("subjectCode"))
	if err != nil {
		return wrightMapError(c, err)
	}
	return success(c, result)
}

func (h *Handler) putWrightMap(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var request struct {
		FileName string `json:"fileName"`
		Text     string `json:"text"`
		Version  *int   `json:"version"`
	}
	if err := c.BodyParser(&request); err != nil || request.Version == nil {
		return errorResponse(c, 422, "nama berkas, teks, dan versi Wright map wajib diisi")
	}
	result, err := h.service.SaveWrightMap(c.UserContext(), getClaims(c).Subject, c.Params("id"), c.Params("subjectCode"), service.WrightMapInput{FileName: request.FileName, Text: request.Text, Version: *request.Version})
	if err != nil {
		return wrightMapError(c, err)
	}
	return success(c, result)
}
