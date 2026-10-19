package httpapi

import (
	"errors"
	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
	"strconv"
)

func coreError(c *fiber.Ctx, err error, fallback int) error {
	code := ""
	if errors.Is(err, service.ErrContactRequired) {
		fallback = 428
		code = "CONTACT_REQUIRED"
	}
	if errors.Is(err, service.ErrRevisionConflict) {
		fallback = 409
		code = "REVISION_CONFLICT"
	}
	if errors.Is(err, service.ErrPeriodClosed) {
		fallback = 422
		code = "PERIOD_CLOSED"
	}
	return c.Status(fallback).JSON(fiber.Map{"status": "error", "message": err.Error(), "code": code})
}
func (h *Handler) registerCoreRoutes(protected, admin fiber.Router, adminRole fiber.Handler, pic, panelist fiber.Router) {
	profile := protected.Group("/profile", h.requireRoles(domain.RolePanelist))
	profile.Get("/contact", h.contact)
	profile.Put("/contact", h.updateContact)
	admin.Post("/projects/:id/copy", adminRole, h.copyProject)
	pic.Post("/rounds/:id/exclusions/:userId", h.excludePanelist)
	pic.Delete("/rounds/:id/exclusions/:userId", h.includePanelist)
	pic.Get("/discussion/:itemId", h.discussion)
	panelist.Post("/submissions/:itemId/review", h.reviewSubmission)
	panelist.Get("/feedback", h.panelistFeedback)
}
func (h *Handler) contact(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	r, err := h.service.Contact(c.UserContext(), getClaims(c).Subject)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, r)
}
func (h *Handler) updateContact(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var req service.ContactInput
	if err := c.BodyParser(&req); err != nil {
		return errorResponse(c, 422, "kontak tidak valid")
	}
	r, err := h.service.UpdateContact(c.UserContext(), getClaims(c).Subject, req)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, r)
}
func (h *Handler) copyProject(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&req); err != nil {
		return errorResponse(c, 422, "nama proyek tidak valid")
	}
	r, err := h.service.CopyProject(c.UserContext(), c.Params("id"), req.Name, getClaims(c).Subject)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, r)
}
func (h *Handler) changeRoundExclusion(c *fiber.Ctx, exclude bool) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var req struct {
		Reason        string `json:"reason"`
		RoundRevision int    `json:"roundRevision"`
		Confirmation  string `json:"confirmation"`
	}
	if err := c.BodyParser(&req); err != nil || req.RoundRevision < 1 {
		return errorResponse(c, 422, "revisi putaran wajib diisi")
	}
	if exclude && req.Confirmation != "KELUARKAN PANELIS" {
		return errorResponse(c, 422, "konfirmasi pengeluaran panelis tidak sesuai")
	}
	err := h.service.SetRoundExclusion(c.UserContext(), getClaims(c).Subject, c.Params("id"), c.Params("userId"), req.Reason, req.RoundRevision, exclude)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, fiber.Map{"updated": true})
}
func (h *Handler) excludePanelist(c *fiber.Ctx) error { return h.changeRoundExclusion(c, true) }
func (h *Handler) includePanelist(c *fiber.Ctx) error { return h.changeRoundExclusion(c, false) }
func (h *Handler) discussion(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	n, err := strconv.Atoi(c.Query("round", "0"))
	if err != nil || n < 0 {
		return errorResponse(c, 422, "nomor putaran tidak valid")
	}
	r, err := h.service.Discussion(c.UserContext(), getClaims(c).Subject, c.Params("itemId"), n)
	if err != nil {
		return coreError(c, err, 422)
	}
	item, err := h.service.GetItem(c.UserContext(), c.Params("itemId"))
	if err != nil {
		return errorResponse(c, 404, "butir tidak tersedia")
	}
	return success(c, struct {
		service.ItemFeedback
		Item fiber.Map `json:"item"`
	}{r, fiber.Map{"id": item.ID, "questionType": item.QuestionType, "maxScore": item.MaxScore}})
}
func (h *Handler) reviewSubmission(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var req struct {
		RoundNumber   int `json:"roundNumber"`
		RoundRevision int `json:"roundRevision"`
		Version       int `json:"version"`
	}
	if err := c.BodyParser(&req); err != nil || req.RoundNumber < 1 || req.RoundRevision < 1 {
		return errorResponse(c, 422, "putaran dan revisi wajib diisi")
	}
	u := getClaims(c)
	r, err := h.service.ReviewSubmission(c.UserContext(), u.Subject, u.ProjectID, u.SubjectCode, req.RoundNumber, c.Params("itemId"), req.RoundRevision, req.Version)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, r)
}
func (h *Handler) panelistFeedback(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	u := getClaims(c)
	f, err := h.service.PanelistForm(c.UserContext(), u.Subject, u.ProjectID, u.SubjectCode)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, f.Feedback)
}
