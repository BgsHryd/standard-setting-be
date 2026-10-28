package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/domain"
)

type loginPeriodHTTPUsers struct {
	*testUsers
	project domain.Project
	err     error
	reads   int
}

func (u *loginPeriodHTTPUsers) GetProject(_ context.Context, id string) (domain.Project, error) {
	u.reads++
	if id != "assigned-project" {
		return domain.Project{}, errors.New("wrong project lookup")
	}
	return u.project, u.err
}

func TestLoginPeriodHTTPMessageStatusAndNoCookie(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("period-password"), bcrypt.MinCost)
	for _, role := range []domain.Role{domain.RolePICMapel, domain.RolePanelist, domain.RoleAdminSuper} {
		for _, phase := range []string{"before", "inside", "after", "unavailable", "wrong password"} {
			t.Run(string(role)+"/"+phase, func(t *testing.T) {
				now := time.Now().UTC()
				users := &loginPeriodHTTPUsers{
					testUsers: &testUsers{user: domain.User{ID: "period-user", Username: "period-user", Role: role, ProjectID: "assigned-project", Active: true, PasswordHash: string(hash)}},
					project:   domain.Project{ID: "assigned-project", StartDate: now.Add(-time.Hour), EndDate: now.Add(time.Hour)},
				}
				want := 200
				password := "period-password"
				switch phase {
				case "before":
					users.project.StartDate, users.project.EndDate = now.Add(time.Hour), now.Add(2*time.Hour)
					want = 403
				case "after":
					users.project.StartDate, users.project.EndDate = now.Add(-2*time.Hour), now.Add(-time.Hour)
					want = 403
				case "unavailable":
					users.err = errors.New("private database failure")
					want = 503
				case "wrong password":
					password, want = "wrong-password", 401
				}
				if role == domain.RoleAdminSuper && phase != "wrong password" {
					want = 200
				}
				s := auth.NewService(users, &testSessions{}, "period-http-secret", time.Minute, time.Hour)
				app := New(Config{Auth: s, AllowedOrigins: "http://localhost:5176"})
				r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"period-user","password":"`+password+`"}`))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", "http://localhost:5176")
				res, err := app.Test(r)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				var payload struct{ Code, Message string }
				if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if res.StatusCode != want {
					t.Fatalf("status %d, want %d: %+v", res.StatusCode, want, payload)
				}
				if want == 403 && (payload.Message != "Akun anda berada di luar jam penilaian" || payload.Code != "OUTSIDE_ASSESSMENT_PERIOD") {
					t.Fatalf("wrong period response: %+v", payload)
				}
				if want != 200 && len(res.Cookies()) != 0 {
					t.Fatal("denied login set a session cookie")
				}
				if (role == domain.RoleAdminSuper || phase == "wrong password") && users.reads != 0 {
					t.Fatal("unnecessary project lookup")
				}
			})
		}
	}
}
