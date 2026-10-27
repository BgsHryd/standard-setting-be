package httpapi

import (
	"github.com/gofiber/fiber/v2"
	"io"
	"strings"
)

func (h *Handler) registerProjectWorkbookRoutes(admin fiber.Router, adminRole fiber.Handler) {
	admin.Get("/project-workbook/template", adminRole, h.projectWorkbookTemplate)
	admin.Post("/project-workbook/import", adminRole, h.projectWorkbookImport)
}
func (h *Handler) projectWorkbookTemplate(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	data, err := h.service.ProjectWorkbookTemplate(c.UserContext())
	if err != nil {
		return errorResponse(c, 409, err.Error())
	}
	c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set("Content-Disposition", "attachment; filename=template-proyek-standard-setting.xlsx")
	return c.Send(data)
}
func (h *Handler) projectWorkbookImport(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	file, err := c.FormFile("file")
	if err != nil || file.Size > 5*1024*1024 || !strings.HasSuffix(strings.ToLower(file.Filename), ".xlsx") {
		return errorResponse(c, 422, "unggah file .xlsx maksimal 5 MB")
	}
	apply := c.FormValue("apply")
	if apply != "" && apply != "false" && apply != "true" {
		return errorResponse(c, 422, "apply harus true atau false")
	}
	stream, err := file.Open()
	if err != nil {
		return err
	}
	defer stream.Close()
	data, err := io.ReadAll(io.LimitReader(stream, 5*1024*1024+1))
	if err != nil {
		return err
	}
	result, err := h.service.ImportProjectWorkbook(c.UserContext(), data, apply == "true", getClaims(c).Subject)
	if err != nil {
		return errorResponse(c, 409, err.Error())
	}
	return success(c, result)
}
