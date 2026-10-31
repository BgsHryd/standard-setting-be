package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/service"
)

func TestAnswerInstructionsRejectMalformedJSON(t *testing.T) {
	h := &Handler{service: service.New(nil, nil, "")}
	app := fiber.New()
	app.Put("/config", func(c *fiber.Ctx) error { c.Locals("claims", &auth.Claims{}); return h.saveQuestionInstructions(c) })
	for _, value := range []string{`true`, `"text"`, `[]`, `{"pilihan_ganda":{"title":12,"description":"help"}}`} {
		req := httptest.NewRequest("PUT", "/config", strings.NewReader(`{"version":0,"instructions":{},"answerInstructions":`+value+`}`))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 422 {
			t.Fatal("malformed answer instructions accepted", value, res.StatusCode)
		}
	}
}
