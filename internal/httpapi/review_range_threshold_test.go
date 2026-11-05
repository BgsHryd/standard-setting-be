package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/service"
)

func TestReviewThresholdRejectsInvalidJSONValues(t *testing.T) {
	h := &Handler{service: service.New(nil, nil, "")}
	app := fiber.New()
	app.Put("/config", func(c *fiber.Ctx) error { c.Locals("claims", &auth.Claims{}); return h.saveQuestionInstructions(c) })
	for _, value := range []string{"-1", "0", "101", "1.5", `"20"`, `true`} {
		req := httptest.NewRequest("PUT", "/config", strings.NewReader(`{"version":0,"instructions":{},"reviewRangeThreshold":`+value+`}`))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 422 {
			t.Fatal("invalid threshold accepted", value, res.StatusCode)
		}
	}
}
