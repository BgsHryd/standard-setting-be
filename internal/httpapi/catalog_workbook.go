package httpapi

import (
	"errors"
	"io"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/service"
)

func (h *Handler) registerCatalogWorkbookRoutes(admin fiber.Router, adminRole fiber.Handler) {
	admin.Get("/catalog-workbook/:kind/template", adminRole, h.catalogWorkbookTemplate)
	admin.Post("/catalog-workbook/:kind/import", adminRole, h.catalogWorkbookImport)
}

func (h *Handler) catalogWorkbookTemplate(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	kind := c.Params("kind")
	if kind != "subjects" && kind != "items" {
		return errorResponse(c, 422, "jenis impor tidak dikenal")
	}
	data, err := h.service.CatalogWorkbookTemplate(c.UserContext(), kind)
	if err != nil {
		return errorResponse(c, 409, err.Error())
	}
	name := "mata-pelajaran"
	if kind == "items" {
		name = "id-butir"
	}
	timestamp := time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("20060102-150405")
	c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set("Content-Disposition", "attachment; filename=template-"+name+"_"+timestamp+".xlsx")
	return c.Send(data)
}

func (h *Handler) catalogWorkbookImport(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	kind := c.Params("kind")
	if kind != "subjects" && kind != "items" {
		return errorResponse(c, 422, "jenis impor tidak dikenal")
	}
	const limit = 5 * 1024 * 1024
	file, err := c.FormFile("file")
	if err != nil || file.Size > limit || !strings.HasSuffix(strings.ToLower(file.Filename), ".xlsx") {
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
	data, err := io.ReadAll(io.LimitReader(stream, limit+1))
	if err != nil {
		return err
	}
	result, err := h.service.ImportCatalogWorkbook(c.UserContext(), kind, data, apply == "true")
	if err != nil {
		status := 409
		if errors.Is(err, service.ErrInvalidCatalogWorkbook) {
			status = 422
		}
		return errorResponse(c, status, err.Error())
	}
	return success(c, result)
}
