package httpapi

import (
	"github.com/gofiber/fiber/v2"
	"net/http"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
)

func (h *Handler) registerSecurityRoutes(admin fiber.Router, adminRole fiber.Handler) {
	admin.Get("/audit-events", adminRole, h.auditEvents)
}
func (h *Handler) auditEvents(c *fiber.Ctx) error {
	rows, err := h.auth.ListAudit(c.UserContext(), c.Query("projectId"), c.Query("search"), c.Query("action"))
	if err != nil {
		return errorResponse(c, http.StatusServiceUnavailable, "riwayat aktivitas sementara tidak tersedia")
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		for _, id := range []string{row.ActorID, row.TargetID} {
			if id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	users, err := h.auth.AuditUsers(c.UserContext(), ids)
	if err != nil {
		return errorResponse(c, http.StatusServiceUnavailable, "identitas riwayat sementara tidak tersedia")
	}
	contexts := map[string]service.AuditContext{}
	if h.service != nil {
		contexts, err = h.service.AuditContexts(c.UserContext(), rows)
		if err != nil {
			return errorResponse(c, 503, "keterangan riwayat sementara tidak tersedia")
		}
	}
	views := make([]auditEventView, 0, len(rows))
	for _, row := range rows {
		actor, target := users[row.ActorID], users[row.TargetID]
		views = append(views, auditEventView{AuditEvent: row, AuditContext: contexts[row.ID], ActorName: actor.Name, TargetName: target.Name, ActorUsername: actor.Username, TargetUsername: target.Username})
	}
	return success(c, views)
}

type auditEventView struct {
	domain.AuditEvent
	service.AuditContext
	ActorName      string `json:"actorName,omitempty"`
	TargetName     string `json:"targetName,omitempty"`
	ActorUsername  string `json:"actorUsername,omitempty"`
	TargetUsername string `json:"targetUsername,omitempty"`
}
