package httpapi

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/service"
)

func TestCatalogWorkbookRoutesRejectNonAdminBeforeReadingUpload(t *testing.T) {
	h := &Handler{}
	app := fiber.New()
	h.registerCatalogWorkbookRoutes(app.Group("/api/v1"), func(c *fiber.Ctx) error { return c.SendStatus(403) })
	for _, kind := range []string{"subjects", "items"} {
		for _, method := range []string{"GET", "POST"} {
			suffix := "template"
			if method == "POST" {
				suffix = "import"
			}
			response, err := app.Test(httptest.NewRequest(method, "/api/v1/catalog-workbook/"+kind+"/"+suffix, nil))
			if err != nil || response.StatusCode != 403 {
				t.Fatalf("unguarded %s %s: %v %v", method, kind, response, err)
			}
			response.Body.Close()
		}
	}
}

func TestCatalogWorkbookTransportRejectsInvalidUploadAndApplyFlag(t *testing.T) {
	h := &Handler{service: service.New(nil, nil, "")}
	app := fiber.New()
	h.registerCatalogWorkbookRoutes(app.Group("/api/v1"), func(c *fiber.Ctx) error { return c.Next() })
	for _, tc := range []struct{ name, filename, apply string }{
		{"wrong extension", "data.csv", "false"},
		{"invalid apply", "data.xlsx", "yes"},
		{"malformed xlsx", "data.xlsx", "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, _ := writer.CreateFormFile("file", tc.filename)
			io.Copy(part, strings.NewReader("invalid workbook"))
			writer.WriteField("apply", tc.apply)
			writer.Close()
			request := httptest.NewRequest("POST", "/api/v1/catalog-workbook/subjects/import", &body)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			response, err := app.Test(request)
			if err != nil || response.StatusCode != 422 {
				t.Fatalf("invalid upload accepted: %v %v", response, err)
			}
			response.Body.Close()
		})
	}
}
