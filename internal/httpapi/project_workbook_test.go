package httpapi

import (
	"github.com/gofiber/fiber/v2"
	"net/http/httptest"
	"testing"
)

func TestProjectWorkbookRoutesRequireAdminBeforeReadingFiles(t *testing.T) {
	app := fiber.New()
	h := &Handler{}
	h.registerProjectWorkbookRoutes(app.Group("/api/v1"), func(c *fiber.Ctx) error { return c.SendStatus(403) })
	for _, method := range []string{"GET", "POST"} {
		path := "/api/v1/project-workbook/template"
		if method == "POST" {
			path = "/api/v1/project-workbook/import"
		}
		response, err := app.Test(httptest.NewRequest(method, path, nil))
		if err != nil || response.StatusCode != 403 {
			t.Fatalf("%s route not guarded: %v %v", method, response, err)
		}
	}
}
