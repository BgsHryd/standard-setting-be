package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"standard-setting/backend/internal/domain"
)

func TestQuestionInstructionsRoutesRequireAdmin(t *testing.T) {
	for _, role := range []domain.Role{domain.RoleAdminSuper, domain.RolePanelist, domain.RolePICMapel} {
		app, _, session := roleApp(t, role, false)
		for _, method := range []string{"GET", "PUT"} {
			request := httptest.NewRequest(method, "/api/v1/config/question-instructions", strings.NewReader(`{"version":0,"instructions":{}}`))
			request.Header.Set("Content-Type", "application/json")
			attachTestSession(request, session)
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			want := 403
			if role == domain.RoleAdminSuper {
				want = 503
			}
			if response.StatusCode != want {
				t.Fatalf("%s %s got%d want%d", role, method, response.StatusCode, want)
			}
		}
	}
}
