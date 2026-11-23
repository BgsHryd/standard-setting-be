package httpapi

import (
	"errors"
	"github.com/gofiber/fiber/v2"
	"io"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
	"strconv"
	"strings"
)

func (h *Handler) listSubjects(c *fiber.Ctx) error {
	data, err := h.service.ListSubjects(c.UserContext())
	if err != nil {
		return err
	}
	return success(c, data)
}
func (h *Handler) createSubject(c *fiber.Ctx) error {
	var input struct {
		Name      string `json:"name"`
		Code      string `json:"code"`
		KodeMapel string `json:"kode_mapel"`
	}
	if err := c.BodyParser(&input); err != nil {
		return errorResponse(c, 422, "nama mata pelajaran tidak valid")
	}
	if input.Code != "" && input.KodeMapel != "" && strings.ToUpper(strings.TrimSpace(input.Code)) != strings.ToUpper(strings.TrimSpace(input.KodeMapel)) {
		return errorResponse(c, 422, "kode mapel tidak konsisten")
	}
	code := input.KodeMapel
	if code == "" {
		code = input.Code
	}
	data, err := h.service.CreateSubject(c.UserContext(), input.Name, code)
	if err != nil {
		if errors.Is(err, service.ErrInvalidSubject) {
			return errorResponse(c, 422, err.Error())
		}
		return errorResponse(c, 409, err.Error())
	}
	return success(c, data)
}
func (h *Handler) accountRows(c *fiber.Ctx) error {
	rows, err := h.service.AccountRows(c.UserContext(), c.Params("id"))
	if err != nil {
		return errorResponse(c, 404, err.Error())
	}
	return success(c, rows)
}
func (h *Handler) userActive(c *fiber.Ctx) error {
	var input struct {
		Active *bool `json:"active"`
	}
	if err := c.BodyParser(&input); err != nil || input.Active == nil {
		return errorResponse(c, 422, "status aktif wajib diisi")
	}
	if err := h.requireService(c); err != nil {
		return err
	}
	if err := h.service.SetUserActiveAs(c.UserContext(), c.Params("id"), *input.Active, getClaims(c).Subject); err != nil {
		return accountMutationError(c, err)
	}
	return success(c, fiber.Map{"updated": true})
}
func (h *Handler) resetPassword(c *fiber.Ctx) error {
	var input struct {
		Confirmation string `json:"confirmation"`
	}
	if err := c.BodyParser(&input); err != nil || input.Confirmation != "RESET PASSWORD" {
		return errorResponse(c, 422, "konfirmasi reset tidak sesuai")
	}
	if err := h.requireService(c); err != nil {
		return err
	}
	if err := h.service.ResetPasswordAs(c.UserContext(), c.Params("id"), getClaims(c).Subject); err != nil {
		return accountMutationError(c, err)
	}
	return success(c, fiber.Map{"reset": true, "mustChangePassword": true})
}
func (h *Handler) template(c *fiber.Ctx) error {
	step, err := strconv.Atoi(c.Params("step"))
	if err != nil {
		return errorResponse(c, 422, "langkah tidak valid")
	}
	data, err := h.service.Template(c.UserContext(), c.Params("id"), step)
	if err != nil {
		return errorResponse(c, 409, err.Error())
	}
	c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set("Content-Disposition", "attachment; filename=standard-setting-langkah-"+strconv.Itoa(step)+".xlsx")
	return c.Send(data)
}
func (h *Handler) importWorkbook(c *fiber.Ctx) error {
	step, err := strconv.Atoi(c.Params("step"))
	if err != nil {
		return errorResponse(c, 422, "langkah tidak valid")
	}
	file, err := c.FormFile("file")
	if err != nil || file.Size > 5*1024*1024 || !strings.HasSuffix(strings.ToLower(file.Filename), ".xlsx") {
		return errorResponse(c, 422, "unggah file .xlsx maksimal 5 MB")
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
	result, err := h.service.ImportWorkbook(c.UserContext(), c.Params("id"), step, data, c.FormValue("apply") == "true")
	if err != nil {
		return errorResponse(c, 409, err.Error())
	}
	return success(c, result)
}

// Viewer payload contains the fields used by all-subject monitoring.
func monitoringViewerData(data service.AllMonitoring) fiber.Map {
	subjects := make([]fiber.Map, 0, len(data.Subjects))
	for _, m := range data.Subjects {
		panelists := make([]fiber.Map, 0, len(m.Panelists))
		for _, panelist := range m.Panelists {
			panelists = append(panelists, fiber.Map{"status": panelist.Status})
		}
		history := make([]fiber.Map, 0, len(m.History))
		for _, round := range m.History {
			history = append(history, fiber.Map{"number": round.Number})
		}
		if m.Statistics != nil {
			stats := *m.Statistics
			stats.PanelistIDs = nil
			m.Statistics = &stats
		}
		subjects = append(subjects, fiber.Map{
			"subject": fiber.Map{"code": m.Subject.Code, "name": m.Subject.Name},
			"round":   fiber.Map{"number": m.Round.Number, "status": m.Round.Status},
			"history": history, "panelists": panelists, "statistics": m.Statistics,
			"totalDone": m.TotalDone, "totalItems": m.TotalItems,
			"standardSettingError": fiber.Map{"unit": m.StandardSettingError.Unit},
			"angoffReview":         fiber.Map{"mix": m.AngoffReview.Mix},
		})
	}
	return fiber.Map{"project": fiber.Map{"id": data.Project.ID, "name": data.Project.Name}, "subjects": subjects}
}

func (h *Handler) monitoringAll(c *fiber.Ctx) error {
	claims := getClaims(c)
	id := c.Params("id")
	viewer := claims.Role == domain.RoleUserMonitoring
	if viewer && claims.ProjectID != id {
		return errorResponse(c, 403, service.ErrMonitoringAssignment.Error())
	}
	if err := h.requireService(c); err != nil {
		return err
	}
	round, err := strconv.Atoi(c.Query("round", "0"))
	if err != nil || round < 0 {
		return errorResponse(c, 422, "putaran tidak valid")
	}
	if viewer {
		data, err := h.service.MonitoringAllForViewer(c.UserContext(), id, round, claims.Subject)
		if errors.Is(err, service.ErrMonitoringAssignment) {
			return errorResponse(c, 403, err.Error())
		}
		if err != nil {
			return errorResponse(c, 404, err.Error())
		}
		return success(c, monitoringViewerData(data))
	}
	data, err := h.service.MonitoringAll(c.UserContext(), id, round)
	if err != nil {
		return errorResponse(c, 404, err.Error())
	}
	return success(c, data)
}
