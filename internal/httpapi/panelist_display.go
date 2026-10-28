package httpapi

import (
	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
)

type panelistItemDTO struct {
	domain.Item
	KJ      *string  `json:"kj,omitempty"`
	Measure *float64 `json:"measure,omitempty"`
}

func panelistFormView(form service.PanelistForm) any {
	items := make([]panelistItemDTO, len(form.Items))
	for i, item := range form.Items {
		items[i].Item = item
		if form.Subject.ShowKJ {
			key := item.KJ
			items[i].KJ = &key
		}
		if form.Subject.ShowMeasure {
			items[i].Measure = item.Measure
		}
	}
	return struct {
		service.PanelistForm
		Items []panelistItemDTO `json:"items"`
	}{form, items}
}

func (h *Handler) setPanelistDisplay(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var request struct {
		ShowKJ         *bool `json:"showKJ"`
		ShowMeasure    *bool `json:"showMeasure"`
		ProjectVersion *int  `json:"projectVersion"`
	}
	if err := c.BodyParser(&request); err != nil || request.ShowKJ == nil || request.ShowMeasure == nil || request.ProjectVersion == nil || *request.ProjectVersion < 0 {
		return errorResponse(c, 422, "showKJ, showMeasure, dan projectVersion wajib diisi dengan nilai yang valid")
	}
	result, err := h.service.SetPanelistDisplay(c.UserContext(), getClaims(c).Subject, service.PanelistDisplayInput{
		ShowKJ: *request.ShowKJ, ShowMeasure: *request.ShowMeasure, ProjectVersion: *request.ProjectVersion,
	})
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, result)
}
