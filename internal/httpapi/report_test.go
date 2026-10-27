package httpapi

import (
	"net/http/httptest"
	"standard-setting/backend/internal/domain"
	"testing"
)

func TestReportingRoutesRoleGuards(t *testing.T) {
	adminRoutes := []struct{ method, path string }{{"GET", "reports"}, {"GET", "exports"}, {"GET", "impact/template"}, {"GET", "impact"}, {"POST", "impact/preview"}, {"POST", "impact/apply"}, {"GET", "evaluations"}, {"GET", "evaluations/export"}}
	for _, route := range adminRoutes {
		for _, role := range []domain.Role{domain.RoleAdminSuper, domain.RolePICMapel, domain.RolePanelist} {
			t.Run(string(role)+route.method+route.path, func(t *testing.T) {
				app, _, token := roleApp(t, role, false)
				req := httptest.NewRequest(route.method, "/api/v1/projects/p/"+route.path, nil)
				attachTestSession(req, token)
				res, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				want := 403
				if role == domain.RoleAdminSuper {
					want = 503
				}
				if res.StatusCode != want {
					t.Fatalf("got %d want %d", res.StatusCode, want)
				}
			})
		}
	}
	for _, method := range []string{"GET", "PUT"} {
		for _, role := range []domain.Role{domain.RoleAdminSuper, domain.RolePICMapel, domain.RolePanelist} {
			t.Run(string(role)+method+"evaluation", func(t *testing.T) {
				app, _, token := roleApp(t, role, false)
				req := httptest.NewRequest(method, "/api/v1/panelist/evaluation?projectId=other&userId=other", nil)
				attachTestSession(req, token)
				res, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				want := 403
				if role == domain.RolePanelist {
					want = 503
				}
				if res.StatusCode != want {
					t.Fatalf("got %d want %d", res.StatusCode, want)
				}
			})
		}
	}
}
