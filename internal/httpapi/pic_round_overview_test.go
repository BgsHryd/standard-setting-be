package httpapi

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/service"
)

func TestPICRoundOverviewRejectsInvalidRoundAndScopeOverrides(t *testing.T) {
	h := &Handler{service: service.New(nil, nil, "")}
	app := fiber.New()
	app.Get("/round-results", func(c *fiber.Ctx) error {
		c.Locals("claims", &auth.Claims{ProjectID: "own-project", SubjectCode: "MAT"})
		return h.picRoundOverview(c)
	})
	for _, tc := range []struct {
		query string
		code  int
	}{
		{"round=abc", 422},
		{"round=-1", 422},
		{"round=0", 422},
		{"round=", 422},
		{"round=1.5", 422},
		{"round=9999999999999999999999999999999999999", 422},
		{"projectId=other", 403},
		{"subjectCode=ENG", 403},
	} {
		t.Run(tc.query, func(t *testing.T) {
			res, err := app.Test(httptest.NewRequest("GET", "/round-results?"+tc.query, nil))
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
