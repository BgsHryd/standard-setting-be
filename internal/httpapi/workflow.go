package httpapi

import (
	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
)

func (h *Handler) registerWorkflowRoutes(pic, panelist fiber.Router) {
	panelist.Post("/rounds/start", h.startPanelistRound)
	panelist.Put("/rounds/notes/:itemId", h.saveDiscussionNote)
	panelist.Post("/rounds/submit", func(c *fiber.Ctx) error { return h.submitPanelistRound(c, true) })
	panelist.Post("/rounds/reopen", func(c *fiber.Ctx) error { return h.submitPanelistRound(c, false) })
	panelist.Get("/practice", h.getPractice)
	panelist.Post("/practice/start", h.startPractice)
	panelist.Post("/practice/finish", h.finishPractice)
	panelist.Put("/practice/answers/:itemId", h.savePracticeAnswer)
	pic.Post("/practice/end", h.endPractice)
	pic.Post("/nbk/final", h.lockFinalNBK)
	pic.Post("/round-plan/extend", h.extendRoundPlan)
}
func (h *Handler) submitPanelistRound(c *fiber.Ctx, submit bool) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var req struct {
		RoundNumber   int `json:"roundNumber"`
		RoundRevision int `json:"roundRevision"`
	}
	if err := c.BodyParser(&req); err != nil || req.RoundNumber < 1 || req.RoundRevision < 1 {
		return errorResponse(c, 422, "nomor dan revisi putaran wajib diisi")
	}
	r, err := h.service.SubmitPanelistRound(c.UserContext(), getClaims(c).Subject, req.RoundNumber, req.RoundRevision, submit)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, r)
}
func (h *Handler) getPractice(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	r, err := h.service.GetPractice(c.UserContext(), getClaims(c).Subject)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, r)
}
func (h *Handler) startPractice(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	r, err := h.service.StartPractice(c.UserContext(), getClaims(c).Subject)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, r)
}
func (h *Handler) finishPractice(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var req struct {
		Version int `json:"version"`
	}
	if err := c.BodyParser(&req); err != nil {
		return errorResponse(c, 422, "versi latihan wajib diisi")
	}
	r, err := h.service.FinishPractice(c.UserContext(), getClaims(c).Subject, req.Version)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, r)
}
func (h *Handler) savePracticeAnswer(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var req struct {
		Version int           `json:"version"`
		Answer  domain.Answer `json:"answer"`
	}
	if err := c.BodyParser(&req); err != nil {
		return errorResponse(c, 422, "jawaban latihan tidak valid")
	}
	r, err := h.service.SavePracticeAnswer(c.UserContext(), getClaims(c).Subject, c.Params("itemId"), req.Answer, req.Version)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, r)
}
func (h *Handler) endPractice(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var req struct {
		Version      int    `json:"projectVersion"`
		Confirmation string `json:"confirmation"`
	}
	if err := c.BodyParser(&req); err != nil || req.Confirmation != "MULAI PENILAIAN" {
		return errorResponse(c, 422, "ketik MULAI PENILAIAN")
	}
	u := getClaims(c)
	if err := h.service.EndSubjectPractice(c.UserContext(), u.Subject, u.ProjectID, u.SubjectCode, req.Version); err != nil {
		return coreError(c, err, 422)
	}
	return success(c, fiber.Map{"updated": true})
}
func (h *Handler) lockFinalNBK(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var req struct {
		RoundNumber   int    `json:"roundNumber"`
		RoundRevision int    `json:"roundRevision"`
		Version       int    `json:"projectVersion"`
		Reason        string `json:"reason"`
		Confirmation  string `json:"confirmation"`
	}
	if err := c.BodyParser(&req); err != nil || req.Confirmation != "TETAPKAN NBK FINAL" {
		return errorResponse(c, 422, "ketik TETAPKAN NBK FINAL")
	}
	u := getClaims(c)
	r, err := h.service.LockFinalNBK(c.UserContext(), u.Subject, u.ProjectID, u.SubjectCode, req.RoundNumber, req.RoundRevision, req.Version, req.Reason)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, r)
}
func (h *Handler) extendRoundPlan(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var req struct {
		Version      int    `json:"projectVersion"`
		Confirmation string `json:"confirmation"`
	}
	if err := c.BodyParser(&req); err != nil || req.Confirmation != "TAMBAH PUTARAN" {
		return errorResponse(c, 422, "ketik TAMBAH PUTARAN")
	}
	r, err := h.service.ExtendRounds(c.UserContext(), getClaims(c).Subject, req.Version)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, r)
}

func (h *Handler) startPanelistRound(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var req struct {
		RoundNumber   int `json:"roundNumber"`
		RoundRevision int `json:"roundRevision"`
	}
	if err := c.BodyParser(&req); err != nil || req.RoundNumber < 1 || req.RoundRevision < 1 {
		return errorResponse(c, 422, "nomor dan revisi putaran wajib diisi")
	}
	result, err := h.service.StartPanelistRound(c.UserContext(), getClaims(c).Subject, req.RoundNumber, req.RoundRevision)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, result)
}

func (h *Handler) saveDiscussionNote(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var req struct {
		RoundNumber   int     `json:"roundNumber"`
		RoundRevision int     `json:"roundRevision"`
		Version       *int    `json:"version"`
		Text          *string `json:"text"`
	}
	if err := c.BodyParser(&req); err != nil || req.RoundNumber < 1 || req.RoundRevision < 1 || req.Version == nil || *req.Version < 0 || req.Text == nil {
		return errorResponse(c, 422, "putaran, revisi, versi, dan teks catatan wajib diisi")
	}
	result, err := h.service.SaveDiscussionNote(c.UserContext(), getClaims(c).Subject, c.Params("itemId"), service.DiscussionNoteInput{RoundNumber: req.RoundNumber, RoundRevision: req.RoundRevision, Version: *req.Version, Text: *req.Text})
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, result)
}
