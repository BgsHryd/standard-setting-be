package httpapi

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/service"
)

func TestAccountManagementRoutesRequireAdminMiddleware(t *testing.T) {
	app := fiber.New()
	h := &Handler{}
	h.registerAccountManagementRoutes(app, func(c *fiber.Ctx) error { return c.SendStatus(403) })
	for _, route := range []struct{ method, path string }{{"POST", "/users"}, {"PATCH", "/users/participant"}, {"DELETE", "/users/participant"}} {
		response, err := app.Test(httptest.NewRequest(route.method, route.path, nil))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatalf("%s %s bypassed admin middleware: %d", route.method, route.path, response.StatusCode)
		}
	}
}
func TestAccountManagementRejectsMalformedPayloadBeforeMutation(t *testing.T) {
	app := fiber.New()
	h := &Handler{}
	h.registerAccountManagementRoutes(app, func(c *fiber.Ctx) error { return c.Next() })
	for _, route := range []struct{ method, path, body string }{{"POST", "/users", `{}`}, {"PATCH", "/users/participant", `{"name":`}, {"DELETE", "/users/participant?confirmation=HAPUS%20PESERTA", ""}, {"DELETE", "/users/participant?projectVersion=2", ""}} {
		req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
		req.Header.Set("Content-Type", "application/json")
		response, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 422 {
			t.Fatalf("invalid %s accepted: %d", route.method, response.StatusCode)
		}
	}
}
func TestAccountManagementErrorStatuses(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{{service.ErrInvalidParticipantAccount, 422}, {mongo.ErrNoDocuments, 404}, {errors.New("stale project"), 409}} {
		app := fiber.New()
		app.Get("/test", func(c *fiber.Ctx) error { return participantAccountError(c, test.err) })
		response, err := app.Test(httptest.NewRequest("GET", "/test", nil))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != test.status {
			t.Fatalf("status %d, expected %d", response.StatusCode, test.status)
		}
	}
}
