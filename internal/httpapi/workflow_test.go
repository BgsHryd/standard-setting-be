package httpapi

import (
	"net/http/httptest"
	"standard-setting/backend/internal/domain"
	"strings"
	"testing"
)

func TestWorkflowEndpointsEnforceRoleAndCSRF(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		owner        domain.Role
	}{
		{"POST", "/panelist/rounds/submit", domain.RolePanelist},
		{"POST", "/panelist/rounds/reopen", domain.RolePanelist},
		{"GET", "/panelist/practice", domain.RolePanelist},
		{"POST", "/panelist/practice/start", domain.RolePanelist},
		{"POST", "/panelist/practice/finish", domain.RolePanelist},
		{"PUT", "/panelist/practice/answers/practice-pg", domain.RolePanelist},
		{"POST", "/panelist/connection", domain.RolePanelist},
		{"POST", "/pic/practice/end", domain.RolePICMapel},
		{"POST", "/pic/nbk/final", domain.RolePICMapel},
		{"POST", "/pic/round-plan/extend", domain.RolePICMapel},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			for _, role := range []domain.Role{domain.RoleAdminSuper, domain.RolePICMapel, domain.RolePanelist} {
				app, _, session := roleApp(t, role, false)
				req := httptest.NewRequest(tc.method, "/api/v1"+tc.path, strings.NewReader(`{}`))
				req.Header.Set("Content-Type", "application/json")
				attachTestSession(req, session)
				res, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				res.Body.Close()
				expected := 403
				if role == tc.owner {
					expected = 503
				}
				if res.StatusCode != expected {
					t.Fatalf("%s reached wrong handler: got%d want%d", role, res.StatusCode, expected)
				}
				if tc.method != "GET" && role == tc.owner {
					req = httptest.NewRequest(tc.method, "/api/v1"+tc.path, strings.NewReader(`{}`))
					attachTestSession(req, session)
					req.Header.Del("X-CSRF-Token")
					res, err = app.Test(req)
					if err != nil {
						t.Fatal(err)
					}
					res.Body.Close()
					if res.StatusCode != 403 {
						t.Fatalf("CSRF bypass: %d", res.StatusCode)
					}
				}
			}
		})
	}
}
