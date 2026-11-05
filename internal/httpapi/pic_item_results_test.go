package httpapi

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/service"
)

func TestPICItemResultsRejectsInvalidRoundAndScopeOverrides(t *testing.T) {
	h := &Handler{service: service.New(nil, nil, "")}
	app := fiber.New()
	app.Get("/item-results", func(c *fiber.Ctx) error {
		c.Locals("claims", &auth.Claims{ProjectID: "own-project", SubjectCode: "MAT"})
		return h.picItemResults(c)
	})
	for _, tc := range []struct {
		query string
		code  int
	}{
		{"round=abc", 422},
		{"round=-1", 422},
		{"round=1.5", 422},
		{"round=9999999999999999999999999999999999999", 422},
		{"projectId=other", 403},
		{"subjectCode=ENG", 403},
	} {
		t.Run(tc.query, func(t *testing.T) {
			res, err := app.Test(httptest.NewRequest("GET", "/item-results?"+tc.query, nil))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.code {
				t.Fatalf("got%d want%d", res.StatusCode, tc.code)
			}
		})
	}
}
