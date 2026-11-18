package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"standard-setting/backend/internal/domain"
)

type loginPeriodUsers struct {
	fakeUsers
	project    domain.Project
	projectErr error
	reads      []string
	lastLogins int
	events     []domain.AuditEvent
}

func (u *loginPeriodUsers) GetProject(_ context.Context, id string) (domain.Project, error) {
	u.reads = append(u.reads, id)
	return u.project, u.projectErr
}
func (u *loginPeriodUsers) UpdateLastLogin(context.Context, string, time.Time) error {
	u.lastLogins++
	return nil
}

func (u *loginPeriodUsers) RecordAudit(_ context.Context, event domain.AuditEvent) error {
	u.events = append(u.events, event)
	return nil
}

func periodLoginService(t *testing.T, role domain.Role) (*Service, *loginPeriodUsers) {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("period-password"), bcrypt.MinCost)
	now := time.Now().UTC()
	u := &loginPeriodUsers{
		fakeUsers: fakeUsers{user: domain.User{ID: "period-user", Username: "period-user", PasswordHash: string(hash), Active: true, Role: role, ProjectID: "assigned-project"}},
		project:   domain.Project{ID: "assigned-project", StartDate: now.Add(-time.Hour), EndDate: now.Add(time.Hour)},
	}
	s := NewService(u, &browserTestSessions{}, "period-test-secret", time.Minute, time.Hour)
	return s, u
}

func TestLoginPeriodRejectsPICAndPanelistOutsideWindow(t *testing.T) {
	for _, role := range []domain.Role{domain.RolePICMapel, domain.RolePanelist} {
		for _, phase := range []string{"before start", "after end"} {
			for _, mode := range []string{"browser", "token"} {
				t.Run(string(role)+"/"+phase+"/"+mode, func(t *testing.T) {
					s, u := periodLoginService(t, role)
					if phase == "before start" {
						u.project.StartDate = time.Now().Add(time.Hour)
						u.project.EndDate = time.Now().Add(2 * time.Hour)
					} else {
						u.project.StartDate = time.Now().Add(-2 * time.Hour)
						u.project.EndDate = time.Now().Add(-time.Hour)
					}
					var err error
					if mode == "browser" {
						var result BrowserResult
						result, err = s.LoginBrowser(context.Background(), "period-user", "period-password", "")
						if result.CookieToken != "" || result.SessionID != "" {
							t.Fatal("blocked login created a browser session")
						}
					} else {
						var result Result
						result, err = s.Login(context.Background(), "period-user", "period-password")
						if result.AccessToken != "" || result.RefreshToken != "" {
							t.Fatal("blocked login issued tokens")
						}
					}
					if !errors.Is(err, ErrOutsideAssessmentPeriod) || err.Error() != "Akun anda berada di luar jam penilaian" || u.lastLogins != 0 {
						t.Fatalf("period rejection: %v, last logins %d", err, u.lastLogins)
					}
					if len(u.reads) != 1 || u.reads[0] != "assigned-project" {
						t.Fatalf("wrong project scope: %+v", u.reads)
					}
					if len(u.events) != 1 || u.events[0].Action != "auth.login" || u.events[0].Outcome != "failure" {
						t.Fatalf("denied login not audited: %+v", u.events)
					}
				})
			}
		}
	}
}

func TestLoginPeriodAllowsParticipantsInsideWindowAndExemptsAdmin(t *testing.T) {
	for _, role := range []domain.Role{domain.RolePICMapel, domain.RolePanelist, domain.RoleAdminSuper} {
		t.Run(string(role), func(t *testing.T) {
			s, u := periodLoginService(t, role)
			if role == domain.RoleAdminSuper {
				u.user.ProjectID = ""
				u.projectErr = errors.New("must not read project for admin")
			}
			result, err := s.LoginBrowser(context.Background(), "period-user", "period-password", "")
			if err != nil || result.CookieToken == "" || u.lastLogins != 1 {
				t.Fatalf("valid login blocked: %+v %v", result, err)
			}
			if role == domain.RoleAdminSuper && len(u.reads) != 0 {
				t.Fatal("admin login depends on project dates")
			}
		})
	}
}

func TestLoginPeriodUsesStrictStartEndAndAbsoluteTime(t *testing.T) {
	s, u := periodLoginService(t, domain.RolePanelist)
	start, _ := time.Parse(time.RFC3339, "2026-10-28T08:00:00+07:00")
	end := start.Add(time.Hour)
	u.project.StartDate, u.project.EndDate = start, end
	for _, tc := range []struct {
		at      time.Time
		allowed bool
	}{
		{start.Add(-time.Nanosecond), false}, {start, false}, {start.Add(time.Nanosecond).UTC(), true},
		{end.Add(-time.Nanosecond).UTC(), true}, {end, false}, {end.Add(time.Nanosecond), false},
	} {
		err := s.checkLoginPeriod(context.Background(), u.user, tc.at)
		if (err == nil) != tc.allowed {
			t.Fatalf("%s: allowed %v, error %v", tc.at, tc.allowed, err)
		}
	}
}

func TestLoginPeriodDoesNotRevealDatesForInvalidCredentials(t *testing.T) {
	s, u := periodLoginService(t, domain.RolePanelist)
	u.projectErr = errors.New("database unavailable")
	if _, err := s.LoginBrowser(context.Background(), "period-user", "wrong-password", ""); !errors.Is(err, ErrInvalidCredentials) || len(u.reads) != 0 {
		t.Fatalf("invalid credentials leaked project state: %v %+v", err, u.reads)
	}
}

func TestLoginPeriodFailsClosedWhenProjectCannotBeChecked(t *testing.T) {
	s, u := periodLoginService(t, domain.RolePICMapel)
	u.projectErr = errors.New("database unavailable")
	if _, err := s.LoginBrowser(context.Background(), "period-user", "period-password", ""); !errors.Is(err, ErrSecurityUnavailable) {
		t.Fatalf("project outage bypassed period restriction: %v", err)
	}
	u.projectErr = nil
	for _, project := range []domain.Project{{}, {StartDate: time.Now().Add(time.Hour), EndDate: time.Now().Add(-time.Hour)}} {
		u.project = project
		if _, err := s.LoginBrowser(context.Background(), "period-user", "period-password", ""); !errors.Is(err, ErrOutsideAssessmentPeriod) {
			t.Fatalf("invalid period allowed: %v", err)
		}
	}
	u.user.ProjectID = ""
	if _, err := s.LoginBrowser(context.Background(), "period-user", "period-password", ""); !errors.Is(err, ErrOutsideAssessmentPeriod) {
		t.Fatalf("unassigned participant allowed: %v", err)
	}
	s.users = struct{ UserRepository }{&u.fakeUsers}
	u.user.ProjectID = "assigned-project"
	if _, err := s.LoginBrowser(context.Background(), "period-user", "period-password", ""); !errors.Is(err, ErrSecurityUnavailable) {
		t.Fatalf("missing period repository allowed: %v", err)
	}
}

func TestLoginPeriodDeniedAttemptDoesNotReplaceExistingSession(t *testing.T) {
	s, u := periodLoginService(t, domain.RolePanelist)
	first, err := s.LoginBrowser(context.Background(), "period-user", "period-password", "")
	if err != nil {
		t.Fatal(err)
	}
	u.project.EndDate = time.Now().Add(-time.Hour)
	if _, err := s.LoginBrowser(context.Background(), "period-user", "period-password", ""); !errors.Is(err, ErrOutsideAssessmentPeriod) {
		t.Fatalf("second login bypassed period: %v", err)
	}
	if _, _, err := s.AuthenticateBrowser(context.Background(), first.CookieToken); err != nil {
		t.Fatalf("denied attempt replaced an existing session: %v", err)
	}
	if u.lastLogins != 1 {
		t.Fatal("denied attempt changed last-login time")
	}
}
