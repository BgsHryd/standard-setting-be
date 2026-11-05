package httpapi

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/service"
)

func TestAdminMonitoringRejectsInvalidRoundQuery(t *testing.T) {
	h := &Handler{service: service.New(nil, nil, "")}
	app := fiber.New()
	app.Get("/monitoring", h.adminMonitoring)
	for _, query := range []string{"-1", "abc", "1.5", "99999999999999999999999999"} {
		response, err := app.Test(httptest.NewRequest("GET", "/monitoring?round="+query, nil))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 422 {
			t.Fatalf("invalid round %q got %d", query, response.StatusCode)
		}
	}
}
