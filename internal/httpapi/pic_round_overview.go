package httpapi

import (
	"github.com/gofiber/fiber/v2"
	"strconv"
)

func (h *Handler) picRoundOverview(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	claims := getClaims(c)
	if (c.Query("projectId") != "" && c.Query("projectId") != claims.ProjectID) || (c.Query("subjectCode") != "" && c.Query("subjectCode") != claims.SubjectCode) {
		return errorResponse(c, 403, "proyek/mapel di luar penugasan PIC")
	}
	number, err := strconv.Atoi(c.Query("round"))
	if err != nil || number < 1 {
		return errorResponse(c, 422, "nomor putaran harus angka bulat positif")
	}
	result, err := h.service.PICRoundOverview(c.UserContext(), claims.Subject, number)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, result)
}
