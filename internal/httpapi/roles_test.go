package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/domain"
)

func roleApp(t *testing.T, role domain.Role, firstLogin bool) (*App, *testUsers, string) {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("qa-password"), bcrypt.MinCost)
	users := &testUsers{user: domain.User{ID: "qa-user", Username: "qa-user", Name: "QA User", Role: role,
		Active: true, PasswordHash: string(hash), MustChangePassword: firstLogin, ProjectID: "qa-project", SubjectCode: "MAT"}}
	service := auth.NewService(users, &testSessions{}, "qa-secret-at-least-32-characters", time.Minute, time.Hour)
	login, err := service.Login(context.Background(), "qa-user", "qa-password")
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{Auth: service}), users, login.AccessToken
}

func TestDeclaredRoleReachesOwnHandlerAndCannotReachOtherRoles(t *testing.T) {
	for _, tc := range []struct {
		role  domain.Role
		route string
		want  int
	}{
		{domain.RolePICMapel, "/api/v1/pic/monitoring", 503},
		{domain.RolePanelist, "/api/v1/panelist/form", 503},
		{domain.RolePICMapel, "/api/v1/projects", 403},
		{domain.RolePanelist, "/api/v1/pic/monitoring", 403},
		{domain.RoleAdminSuper, "/api/v1/panelist/form", 403},
	} {
		t.Run(string(tc.role)+tc.route, func(t *testing.T) {
			app, _, token := roleApp(t, tc.role, false)
			req := httptest.NewRequest("GET", tc.route, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.want {
				t.Fatalf("want %d, got %d", tc.want, res.StatusCode)
			}
		})
	}
}

func TestChangePasswordReturnsUsableFreshSession(t *testing.T) {
	app, _, token := roleApp(t, domain.RolePanelist, true)
	req := httptest.NewRequest("POST", "/api/v1/auth/change-password", strings.NewReader(`{"currentPassword":"qa-password","newPassword":"new-password"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var payload struct {
		Data auth.Result `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || payload.Data.AccessToken == "" || payload.Data.User.MustChangePassword {
		t.Fatalf("password change did not return fresh usable session: status=%d tokenPresent=%v", res.StatusCode, payload.Data.AccessToken != "")
	}
	next := httptest.NewRequest("GET", "/api/v1/me", nil)
	next.Header.Set("Authorization", "Bearer "+payload.Data.AccessToken)
	result, err := app.Test(next)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	if result.StatusCode != 200 {
		t.Fatalf("fresh token rejected: %d", result.StatusCode)
	}
}

func TestPICCannotOverrideProjectOrSubjectScope(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"?projectId=other-project", 403}, {"?subjectCode=BIN", 403},
		{"?projectId=qa-project&subjectCode=MAT", 503},
	} {
		app, _, token := roleApp(t, domain.RolePICMapel, false)
		req := httptest.NewRequest("GET", "/api/v1/pic/monitoring"+tc.query, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.want {
			t.Fatalf("%s: want %d got %d", tc.query, tc.want, res.StatusCode)
		}
	}
}

func TestInactiveAccountCannotReuseAccessToken(t *testing.T) {
	app, users, token := roleApp(t, domain.RolePanelist, false)
	users.user.Active = false
	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("inactive account got %d", res.StatusCode)
	}
}
