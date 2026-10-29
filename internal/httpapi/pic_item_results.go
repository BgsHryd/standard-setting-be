package httpapi

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func (h *Handler) registerPICItemResultRoutes(pic fiber.Router) {
	pic.Get("/item-results", h.picItemResults)
}
func (h *Handler) picItemResults(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	claims := getClaims(c)
	if (c.Query("projectId") != "" && c.Query("projectId") != claims.ProjectID) || (c.Query("subjectCode") != "" && c.Query("subjectCode") != claims.SubjectCode) {
		return errorResponse(c, 403, "proyek/mapel di luar penugasan PIC")
	}
	number, err := strconv.Atoi(c.Query("round", "0"))
	if err != nil || number < 0 {
		return errorResponse(c, 422, "putaran harus angka positif atau 0 untuk putaran terbaru")
	}
	result, err := h.service.PICItemResults(c.UserContext(), claims.Subject, number)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, result)
}
