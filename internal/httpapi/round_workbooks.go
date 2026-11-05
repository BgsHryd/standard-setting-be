package httpapi

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/service"
)

func (h *Handler) roundWorkbooks(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	confirmed := c.Query("confirmed", "false")
	if confirmed != "true" && confirmed != "false" {
		return errorResponse(c, 422, "confirmed harus true atau false")
	}
	data, name, err := h.service.ExportRoundWorkbooks(c.UserContext(), c.Params("id"), c.Query("subjectCode", "all"), confirmed == "true")
	if errors.Is(err, service.ErrExportConfirmationRequired) {
		return c.Status(409).JSON(fiber.Map{"status": "error", "code": "EXPORT_CONFIRMATION_REQUIRED", "message": err.Error()})
	}
	if err != nil {
		return reportingError(c, err)
	}
	c.Set(fiber.HeaderContentType, "application/zip")
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="`+name+`"`)
	c.Set("Cache-Control", "no-store")
	return c.Send(data)
}
