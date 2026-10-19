package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/mongo"
	"io"
	"path/filepath"
	"standard-setting/backend/internal/domain"
	"strconv"
	"strings"
)

func (h *Handler) registerReportingRoutes(admin fiber.Router, adminRole fiber.Handler, panelist fiber.Router) {
	admin.Get("/projects/:id/reports", adminRole, h.projectReport)
	admin.Get("/projects/:id/exports", adminRole, h.exportReport)
	admin.Get("/projects/:id/impact/template", adminRole, h.impactTemplate)
	admin.Post("/projects/:id/impact/preview", adminRole, h.impactPreview)
	admin.Post("/projects/:id/impact/apply", adminRole, h.impactApply)
	admin.Get("/projects/:id/impact", adminRole, h.impactResults)
	admin.Get("/projects/:id/evaluations", adminRole, h.evaluationSummary)
	admin.Get("/projects/:id/evaluations/export", adminRole, h.evaluationExport)
	panelist.Get("/evaluation", h.getEvaluation)
	panelist.Put("/evaluation", h.putEvaluation)
}
func reportRound(c *fiber.Ctx) (int, error) {
	n, err := strconv.Atoi(c.Query("round", "0"))
	if err != nil || n < 0 {
		return 0, fiber.NewError(422, "putaran harus angka positif; 0 memilih hasil final terbaru")
	}
	return n, nil
}
func reportRevision(c *fiber.Ctx) (int, error) {
	n, err := strconv.Atoi(c.Query("revision", "0"))
	if err != nil || n < 0 {
		return 0, fiber.NewError(422, "revisi snapshot harus angka positif atau 0")
	}
	return n, nil
}
func reportingError(c *fiber.Ctx, err error) error {
	status := 422
	if errors.Is(err, mongo.ErrNoDocuments) {
		status = 404
	}
	message := err.Error()
	if strings.Contains(message, "berubah") || strings.Contains(message, "kedaluwarsa") {
		status = 409
	}
	return errorResponse(c, status, message)
}
func (h *Handler) projectReport(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	n, err := reportRound(c)
	if err != nil {
		return err
	}
	revision, err := reportRevision(c)
	if err != nil {
		return err
	}
	result, err := h.service.ProjectReportRevision(c.UserContext(), c.Params("id"), n, revision)
	if err != nil {
		return reportingError(c, err)
	}
	return success(c, result)
}
func sendReportCSV(c *fiber.Ctx, data []byte, name string) error {
	c.Set(fiber.HeaderContentType, "text/csv; charset=utf-8")
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="`+name+`"`)
	c.Set("Cache-Control", "no-store")
	return c.Send(data)
}
func (h *Handler) exportReport(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	n, err := reportRound(c)
	if err != nil {
		return err
	}
	revision, err := reportRevision(c)
	if err != nil {
		return err
	}
	kind := c.Query("type", "summary")
	data, err := h.service.ExportReportRevision(c.UserContext(), c.Params("id"), n, revision, c.Query("format", "id"), kind)
	if err != nil {
		return reportingError(c, err)
	}
	return sendReportCSV(c, data, "standard-setting-"+kind+".csv")
}
func (h *Handler) impactTemplate(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	data, err := h.service.ImpactTemplate(c.UserContext(), c.Params("id"))
	if err != nil {
		return reportingError(c, err)
	}
	c.Set(fiber.HeaderContentType, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="template-skor-dampak.xlsx"`)
	c.Set("Cache-Control", "no-store")
	return c.Send(data)
}
func impactFile(c *fiber.Ctx) ([]byte, error) {
	file, err := c.FormFile("file")
	if err != nil {
		return nil, fiber.NewError(422, "unggah file template .xlsx")
	}
	if file.Size > 5*1024*1024 || strings.ToLower(filepath.Ext(file.Filename)) != ".xlsx" {
		return nil, fiber.NewError(422, "file wajib .xlsx maksimal 5 MB")
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, 5*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 5*1024*1024 {
		return nil, fiber.NewError(422, "file maksimal 5 MB")
	}
	return data, nil
}
func (h *Handler) impactPreview(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	data, err := impactFile(c)
	if err != nil {
		return err
	}
	result, err := h.service.PreviewImpact(c.UserContext(), c.Params("id"), data)
	if err != nil {
		return reportingError(c, err)
	}
	return success(c, result)
}
func (h *Handler) impactApply(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	data, err := impactFile(c)
	if err != nil {
		return err
	}
	var binding domain.ImpactBinding
	if err = json.Unmarshal([]byte(c.FormValue("binding")), &binding); err != nil || binding.ProjectID == "" || binding.FileHash == "" {
		return errorResponse(c, 422, "binding dari pratinjau wajib dikirim")
	}
	result, err := h.service.ApplyImpact(c.UserContext(), c.Params("id"), getClaims(c).Subject, data, binding)
	if err != nil {
		return reportingError(c, err)
	}
	return success(c, result)
}
func (h *Handler) impactResults(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	result, err := h.service.ImpactResults(c.UserContext(), c.Params("id"))
	if err != nil {
		return reportingError(c, err)
	}
	return success(c, result)
}
func (h *Handler) getEvaluation(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	result, err := h.service.GetEvaluation(c.UserContext(), getClaims(c).Subject)
	if err != nil {
		return reportingError(c, err)
	}
	return success(c, result)
}
func (h *Handler) putEvaluation(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var input domain.Evaluation
	if c.BodyParser(&input) != nil {
		return errorResponse(c, 422, "evaluasi tidak valid")
	}
	result, err := h.service.SaveEvaluation(c.UserContext(), getClaims(c).Subject, input)
	if err != nil {
		return reportingError(c, err)
	}
	return success(c, result)
}
func (h *Handler) evaluationSummary(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	result, err := h.service.EvaluationSummary(c.UserContext(), c.Params("id"))
	if err != nil {
		return reportingError(c, err)
	}
	return success(c, result)
}
func (h *Handler) evaluationExport(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	data, err := h.service.EvaluationCSV(c.UserContext(), c.Params("id"), c.Query("format", "id"))
	if err != nil {
		return reportingError(c, err)
	}
	return sendReportCSV(c, data, "evaluasi-agregat.csv")
}
