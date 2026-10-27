package httpapi

import (
	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/service"
)

func (h *Handler) registerTelemetryRoutes(pic, panelist fiber.Router) {
	panelist.Post("/connection", h.recordTelemetry)
}
func (h *Handler) recordTelemetry(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var req service.TelemetryInput
	if err := c.BodyParser(&req); err != nil {
		return errorResponse(c, 422, "telemetri tidak valid")
	}
	row, err := h.service.RecordTelemetry(c.UserContext(), getClaims(c).Subject, req)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, row)
}
