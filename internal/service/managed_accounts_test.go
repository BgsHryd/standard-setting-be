package service

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
	"standard-setting/backend/internal/domain"
)

func managedAccountFixture() (*Service, *accountManagementStore) {
	s, st := accountManagementFixture()
	st.users["super"] = domain.User{ID: "super", Username: "super", Role: domain.RoleAdminSuper, Active: true}
	st.users["admin"] = domain.User{ID: "admin", Username: "admin", Role: "admin", Active: true}
	st.users["other-admin"] = domain.User{ID: "other-admin", Username: "other.admin", Role: "admin", Active: true, PasswordHash: "existing-admin-hash"}
	st.users["viewer"] = domain.User{ID: "viewer", Username: "viewer", Role: "user_monitoring", Active: true, AuthVersion: 3}
	return s, st
}

func TestManagedAccountCreationRequiresPermissionForRequestedRole(t *testing.T) {
	ctx := context.Background()
	s, st := managedAccountFixture()
	input := ManagedAccountInput{Name: " New Admin ", Username: " NEW.ADMIN ", Role: domain.Role("admin")}
	if _, err := s.CreateManagedAccount(ctx, input, "admin"); !errors.Is(err, ErrAccountAccessDenied) {
		t.Fatalf("admin created another admin: %v", err)
	}
	input.Role = domain.RoleAdminSuper
	if _, err := s.CreateManagedAccount(ctx, input, "super"); !errors.Is(err, ErrAccountAccessDenied) {
		t.Fatalf("created a protected superadmin: %v", err)
	}
	input.Role = "admin"
	created, err := s.CreateManagedAccount(ctx, input, "super")
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "New Admin" || created.Username != "new.admin" || created.ProjectID != "" || !created.Active || !created.MustChangePassword {
		t.Fatalf("unexpected account: %+v", created)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(created.PasswordHash), []byte("initial-test-password")); err != nil {
		t.Fatal("new account does not use the configured initial password")
	}
	if len(st.audits) != 1 || st.audits[0].ActorID != "super" || st.audits[0].TargetID != created.ID || st.audits[0].Action != "user_create" {
		t.Fatalf("incorrect audit: %+v", st.audits)
	}

	input = ManagedAccountInput{Name: "Monitor", Username: "new.viewer", Role: "user_monitoring"}
	if _, err := s.CreateManagedAccount(ctx, input, "admin"); err != nil {
		t.Fatalf("admin could not create monitoring account: %v", err)
	}
}

func TestManagedAccountMutationsProtectAdministrativeTargets(t *testing.T) {
	ctx := context.Background()
	s, st := managedAccountFixture()
	edit := ManagedAccountIdentity{Name: "Renamed", Username: "renamed.admin"}
	for _, tc := range []struct{ actor, target string }{{"admin", "other-admin"}, {"admin", "admin"}, {"super", "super"}, {"panel", "viewer"}, {"", "viewer"}} {
		t.Run(tc.actor+"/"+tc.target, func(t *testing.T) {
			before := st.users[tc.target]
			if _, err := s.UpdateManagedAccount(ctx, tc.target, edit, tc.actor); !errors.Is(err, ErrAccountAccessDenied) {
				t.Fatalf("identity edit: %v", err)
			}
			if err := s.RemoveManagedAccount(ctx, tc.target, tc.actor); !errors.Is(err, ErrAccountAccessDenied) {
				t.Fatalf("remove: %v", err)
			}
			if got := st.users[tc.target]; got != before {
				t.Fatalf("protected account changed: %+v", got)
			}
		})
	}
	if len(st.audits) != 0 {
		t.Fatalf("a denied mutation was audited as successful: %+v", st.audits)
	}
}

func TestSuperadminCanEditResetDeactivateAndRemoveAdmin(t *testing.T) {
	ctx := context.Background()
	s, st := managedAccountFixture()
	id, actor := "other-admin", "super"
	u, err := s.UpdateManagedAccount(ctx, id, ManagedAccountIdentity{Name: "Renamed", Username: "renamed.admin"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "renamed.admin" || u.AuthVersion != 1 || u.PasswordHash != "existing-admin-hash" {
		t.Fatalf("unexpected edited admin: %+v", u)
	}
	if err := s.ResetPasswordAs(ctx, id, actor); err != nil {
		t.Fatal(err)
	}
	if !st.users[id].MustChangePassword || st.users[id].AuthVersion != 2 {
		t.Fatalf("reset failed to invalidate prior password/session: %+v", st.users[id])
	}
	if err := s.SetUserActiveAs(ctx, id, false, actor); err != nil {
		t.Fatal(err)
	}
	if st.users[id].Active || st.users[id].AuthVersion != 3 {
		t.Fatalf("deactivation failed: %+v", st.users[id])
	}
	if err := s.RemoveManagedAccount(ctx, id, actor); err != nil {
		t.Fatal(err)
	}
	if u := st.users[id]; !u.Removed || u.Active || u.AuthVersion != 4 {
		t.Fatalf("removal failed: %+v", u)
	}
	if st.touches != 0 || len(st.audits) != 4 {
		t.Fatalf("global account operation touched project roster or skipped audits: %d touches, %d audits", st.touches, len(st.audits))
	}
}

func TestMonitoringAssignmentAcceptsActiveProjectAndCanBeCleared(t *testing.T) {
	ctx := context.Background()
	s, st := managedAccountFixture()
	u, err := s.SetMonitoringProject(ctx, "viewer", "p", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if u.ProjectID != "p" || u.ProjectName != "Project" || u.AuthVersion != 4 || st.touches != 1 {
		t.Fatalf("incorrect monitoring assignment: %+v", u)
	}
	if _, err := s.SetMonitoringProject(ctx, "viewer", "p", "admin"); err != nil {
		t.Fatal(err)
	}
	if len(st.audits) != 1 || st.audits[0].Action != "user_monitoring_project_update" || st.audits[0].ProjectID != "p" || st.audits[0].ActorID != "admin" {
		t.Fatalf("incorrect assignment audit: %+v", st.audits)
	}
	st.project.Status = domain.ProjectCompleted
	rows, err := s.AccountRows(ctx, "p")
	if err != nil || len(rows) != 2 {
		t.Fatalf("monitoring account leaked into participant roster: %+v %v", rows, err)
	}
	u, err = s.SetMonitoringProject(ctx, "viewer", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if u.ProjectID != "" || u.ProjectName != "" || u.AuthVersion != 5 || len(st.audits) != 2 || st.audits[1].ProjectID != "p" {
		t.Fatalf("incorrect revocation: %+v, audits: %+v", u, st.audits)
	}
}

func TestMonitoringAssignmentRejectsInvalidProjectAndRole(t *testing.T) {
	ctx := context.Background()
	s, st := managedAccountFixture()
	for _, status := range []domain.ProjectStatus{domain.ProjectDraft, domain.ProjectCompleted} {
		st.project.Status = status
		if _, err := s.SetMonitoringProject(ctx, "viewer", "p", "admin"); err == nil {
			t.Errorf("assignment accepted %s project", status)
		}
	}
	st.project.Status = domain.ProjectActive
	if _, err := s.SetMonitoringProject(ctx, "viewer", "missing", "admin"); !errors.Is(err, mongo.ErrNoDocuments) {
		t.Errorf("assignment accepted unknown project: %v", err)
	}
	if _, err := s.SetMonitoringProject(ctx, "panel", "p", "admin"); !errors.Is(err, ErrAccountAccessDenied) {
		t.Errorf("assignment accepted project participant: %v", err)
	}
	if _, err := s.SetMonitoringProject(ctx, "viewer", "p", "panel"); !errors.Is(err, ErrAccountAccessDenied) {
		t.Errorf("participant changed another account's project: %v", err)
	}
	if u := st.users["viewer"]; u.ProjectID != "" || u.AuthVersion != 3 || len(st.audits) != 0 {
		t.Fatalf("invalid request changed viewer: %+v, audits: %+v", u, st.audits)
	}
}

func TestMonitoringAssignmentRollsBackWhenAuditFails(t *testing.T) {
	ctx := context.Background()
	s, st := managedAccountFixture()
	st.failAudit = true
	before := st.users["viewer"]
	if _, err := s.SetMonitoringProject(ctx, "viewer", "p", "admin"); err == nil {
		t.Fatal("assignment succeeded without its audit event")
	}
	if got := st.users["viewer"]; got != before {
		t.Fatalf("assignment survived failed transaction: %+v", got)
	}
}

func TestAdminAccountListHidesAdministrativeAccounts(t *testing.T) {
	ctx := context.Background()
	s, _ := managedAccountFixture()
	adminView, err := s.ListUsersAs(ctx, "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(adminView) != 3 {
		t.Fatalf("admin should see PIC, panelist, and viewer accounts: %+v", adminView)
	}
	for _, u := range adminView {
		if u.Role == "admin" || u.Role == domain.RoleAdminSuper {
			t.Fatalf("admin account leaked to regular admin: %+v", u)
		}
	}
	superView, err := s.ListUsersAs(ctx, "", "super")
	if err != nil || len(superView) != 6 {
		t.Fatalf("superadmin should see all six accounts: %+v %v", superView, err)
	}
}

func TestAdminCannotDeactivateOrResetAnotherAdmin(t *testing.T) {
	ctx := context.Background()
	s, st := managedAccountFixture()
	before := st.users["other-admin"]

	activeErr := s.SetUserActiveAs(ctx, before.ID, false, "admin")
	resetErr := s.ResetPasswordAs(ctx, before.ID, "admin")

	if activeErr == nil || resetErr == nil {
		t.Errorf("protected admin mutations returned status %v and reset %v", activeErr, resetErr)
	}
	if got := st.users[before.ID]; got != before || len(st.audits) != 0 {
		t.Fatalf("protected account changed: %+v, audits: %+v", got, st.audits)
	}
}
