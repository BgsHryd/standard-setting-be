package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
)

func TestPICSubjectConfigRouteRequiresPIC(t *testing.T) {
	for _, role := range []domain.Role{domain.RolePICMapel, domain.RolePanelist, domain.RoleAdminSuper} {
		for _, method := range []string{"GET", "PUT"} {
			app, _, session := roleApp(t, role, false)
			request := httptest.NewRequest(method, "/api/v1/pic/subject-config", strings.NewReader(`{"candidateDefinition":"JQC","projectVersion":0}`))
			request.Header.Set("Content-Type", "application/json")
			attachTestSession(request, session)
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			want := 403
			if role == domain.RolePICMapel {
				want = 503
			}
			if response.StatusCode != want {
				t.Fatalf("%s %s: got %d want %d", role, method, response.StatusCode, want)
			}
		}
	}
}

func TestPICSubjectConfigRejectsMissingFields(t *testing.T) {
	h := &Handler{service: service.New(nil, nil, "")}
	app := fiber.New()
	app.Put("/settings", h.savePICSubjectConfig)
	for _, body := range []string{`{}`, `{"candidateDefinition":"JQC"}`, `{"projectVersion":0}`, `{"candidateDefinition":false,"projectVersion":0}`, `{"candidateDefinition":"JQC","projectVersion":-1}`, `{"candidateDefinition":"JQC","projectVersion":1.2}`} {
		request := httptest.NewRequest("PUT", "/settings", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response, err := app.Test(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 422 {
			t.Fatal("invalid config accepted", body, response.StatusCode)
		}
	}
}
