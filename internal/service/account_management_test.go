package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
	"standard-setting/backend/internal/domain"
)

// This store holds only this test's data and rolls back failed transactions.
type accountManagementStore struct {
	Store
	project  domain.Project
	users    map[string]domain.User
	audits   []domain.AuditEvent
	failSave bool
	touches  int
}

func (st *accountManagementStore) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	b, _ := json.Marshal(st.project)
	oldProject := domain.Project{}
	_ = json.Unmarshal(b, &oldProject)
	oldUsers := map[string]domain.User{}
	for id, u := range st.users {
		oldUsers[id] = u
	}
	count := len(st.audits)
	if err := fn(ctx); err != nil {
		st.project, st.users, st.audits = oldProject, oldUsers, st.audits[:count]
		return err
	}
	return nil
}
func (st *accountManagementStore) GetProject(_ context.Context, id string) (domain.Project, error) {
	if id != st.project.ID {
		return domain.Project{}, mongo.ErrNoDocuments
	}
	b, _ := json.Marshal(st.project)
	var p domain.Project
	_ = json.Unmarshal(b, &p)
	return p, nil
}
func (st *accountManagementStore) SaveProject(_ context.Context, p domain.Project) error {
	if st.failSave {
		return errors.New("save failed")
	}
	st.project = p
	return nil
}
func (st *accountManagementStore) TouchProject(context.Context, string) error {
	st.touches++
	return nil
}
func (st *accountManagementStore) FindByID(_ context.Context, id string) (domain.User, error) {
	u, ok := st.users[id]
	if !ok {
		return u, mongo.ErrNoDocuments
	}
	return u, nil
}
func (st *accountManagementStore) FindByUsername(_ context.Context, name string) (domain.User, error) {
	for _, u := range st.users {
		if u.Username == name {
			return u, nil
		}
	}
	return domain.User{}, mongo.ErrNoDocuments
}
func (st *accountManagementStore) SaveUser(_ context.Context, u domain.User) error {
	if _, ok := st.users[u.ID]; !ok {
		return mongo.ErrNoDocuments
	}
	st.users[u.ID] = u
	return nil
}
func (st *accountManagementStore) InsertUsers(_ context.Context, users []domain.User) error {
	for _, u := range users {
		if _, exists := st.users[u.ID]; exists {
			return errors.New("duplicate ID")
		}
		st.users[u.ID] = u
	}
	return nil
}
func (st *accountManagementStore) RecordAudit(_ context.Context, e domain.AuditEvent) error {
	st.audits = append(st.audits, e)
	return nil
}
func accountManagementFixture() (*Service, *accountManagementStore) {
	p := domain.Project{ID: "p", Name: "Project", Version: 4, Status: domain.ProjectActive, AccountsGenerated: true, Subjects: []domain.ProjectSubject{{Code: "MAT", PICName: "PIC", PICUserID: "pic", PanelistCount: 1, PanelistNames: []string{"Panelist"}, PanelistUserIDs: []string{"panel"}, Slots: []domain.PersonnelSlot{{ID: "pic-slot", Role: domain.RolePICMapel, Name: "PIC", UserID: "pic"}, {ID: "panel-slot", Role: domain.RolePanelist, Name: "Panelist", UserID: "panel"}}}}}
	st := &accountManagementStore{project: p, users: map[string]domain.User{"pic": {ID: "pic", Username: "pic.user", Name: "PIC", Role: domain.RolePICMapel, ProjectID: "p", SubjectCode: "MAT", SlotID: "pic-slot", Active: true}, "panel": {ID: "panel", Username: "panel.user", Name: "Panelist", Role: domain.RolePanelist, ProjectID: "p", SubjectCode: "MAT", SlotID: "panel-slot", Active: true, PasswordHash: "changed-password-hash", AuthVersion: 3}}}
	return New(st, nil, "initial-test-password"), st
}
func TestAccountManagementUsernameEditPreservesPasswordAndRoster(t *testing.T) {
	s, st := accountManagementFixture()
	u, err := s.UpdateParticipantAccount(context.Background(), "panel", ParticipantAccountEdit{ProjectVersion: 4, Name: "New name", Username: " NEW.USER "}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "new.user" || u.PasswordHash != "changed-password-hash" || u.AuthVersion != 4 || u.ID != "panel" {
		t.Fatalf("unsafe edit: %+v", u)
	}
	sub := st.project.Subjects[0]
	if sub.Slots[1].Name != "New name" || sub.PanelistNames[0] != "New name" || sub.PanelistUserIDs[0] != "panel" || st.project.Version != 5 || st.touches != 1 {
		t.Fatalf("roster not updated: %+v", st.project)
	}
	b, _ := json.Marshal(u)
	if string(b) == "" || strings.Contains(string(b), "changed-password-hash") {
		t.Fatal("password exposed")
	}
}
func TestAccountManagementCreateAddsParticipantToRoster(t *testing.T) {
	s, st := accountManagementFixture()
	u, err := s.CreateParticipantAccount(context.Background(), ParticipantAccountInput{ProjectID: "p", ProjectVersion: 4, SubjectCode: "MAT", Role: domain.RolePanelist, Name: "New participant", Username: "new.panel"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	sub := st.project.Subjects[0]
	if u.ProjectID != "p" || u.SubjectCode != "MAT" || u.SlotID == "" || !u.Active || !u.MustChangePassword || len(sub.Slots) != 3 || sub.PanelistCount != 2 || sub.PanelistUserIDs[1] != u.ID || st.project.Version != 5 {
		t.Fatalf("orphan participant: %+v %+v", u, sub)
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("initial-test-password")) != nil {
		t.Fatal("wrong initial password")
	}
	if u.UpdatedAt.IsZero() {
		t.Fatal("creation response missing timestamp")
	}
}
func TestAccountManagementCreateFillsPendingSlot(t *testing.T) {
	s, st := accountManagementFixture()
	st.project.Subjects[0].Slots[1].UserID = ""
	st.project.Subjects[0].PanelistUserIDs = nil
	delete(st.users, "panel")
	u, err := s.CreateParticipantAccount(context.Background(), ParticipantAccountInput{ProjectID: "p", ProjectVersion: 4, SubjectCode: "MAT", Role: domain.RolePanelist, SlotID: "panel-slot", Name: "Replacement", Username: "replacement"}, "admin")
	if err != nil || u.SlotID != "panel-slot" || len(st.project.Subjects[0].Slots) != 2 {
		t.Fatalf("pending slot not filled: %+v %v", u, err)
	}
}
func TestAccountManagementRejectsInvalidAssignmentsAndStaleVersions(t *testing.T) {
	for _, kind := range []string{"second PIC", "foreign subject", "occupied slot", "admin role", "stale", "duplicate username", "invalid username", "completed"} {
		t.Run(kind, func(t *testing.T) {
			s, st := accountManagementFixture()
			in := ParticipantAccountInput{ProjectID: "p", ProjectVersion: 4, SubjectCode: "MAT", Role: domain.RolePanelist, Name: "New", Username: "new.user"}
			switch kind {
			case "second PIC":
				in.Role = domain.RolePICMapel
			case "foreign subject":
				in.SubjectCode = "ENG"
			case "occupied slot":
				in.SlotID = "panel-slot"
			case "admin role":
				in.Role = domain.RoleAdminSuper
			case "stale":
				in.ProjectVersion = 3
			case "duplicate username":
				in.Username = "panel.user"
			case "invalid username":
				in.Username = "??"
			case "completed":
				st.project.Status = domain.ProjectCompleted
			}
			if _, err := s.CreateParticipantAccount(context.Background(), in, "admin"); err == nil {
				t.Fatal("unsafe assignment accepted")
			}
			if len(st.users) != 2 || st.project.Version != 4 {
				t.Fatal("partial mutation")
			}
		})
	}
}
func TestAccountManagementRemoveSoftDeletesWithoutRecyclingIdentity(t *testing.T) {
	s, st := accountManagementFixture()
	if err := s.RemoveParticipantAccount(context.Background(), "panel", 4, "admin"); err != nil {
		t.Fatal(err)
	}
	u := st.users["panel"]
	if !u.Removed || u.Active || u.AuthVersion != 4 || u.PasswordHash != "changed-password-hash" || u.ProjectID != "p" {
		t.Fatalf("history identity lost: %+v", u)
	}
	sub := st.project.Subjects[0]
	if len(sub.Slots) != 1 || len(sub.PanelistUserIDs) != 0 || sub.PanelistCount != 0 || st.project.Version != 5 {
		t.Fatalf("roster retained removed account: %+v", sub)
	}
	if len(st.audits) != 1 || st.audits[0].Action != "user_remove" {
		t.Fatal("missing audit")
	}
}
func TestAccountManagementRemovePICKeepsSingleVacantSlot(t *testing.T) {
	s, st := accountManagementFixture()
	if err := s.RemoveParticipantAccount(context.Background(), "pic", 4, "admin"); err != nil {
		t.Fatal(err)
	}
	sub := st.project.Subjects[0]
	if sub.PICUserID != "" || sub.Slots[0].UserID != "" || sub.Slots[0].ID == "pic-slot" || sub.Slots[0].Name != "" || st.project.AccountsGenerated {
		t.Fatalf("PIC identity recycled: %+v", sub)
	}
}
func TestAccountManagementProtectsAdminCompletedAndOrphanUsers(t *testing.T) {
	for _, kind := range []string{"admin", "completed", "orphan", "removed", "stale", "collision"} {
		t.Run(kind, func(t *testing.T) {
			s, st := accountManagementFixture()
			u := st.users["panel"]
			switch kind {
			case "admin":
				u.Role = domain.RoleAdminSuper
			case "completed":
				st.project.Status = domain.ProjectCompleted
			case "orphan":
				u.SlotID = "foreign"
			case "removed":
				u.Removed = true
			case "collision":
				u.Username = "other.user"
			}
			st.users["panel"] = u
			version := 4
			if kind == "stale" {
				version = 2
			}
			username := "edit.user"
			if kind == "collision" {
				username = "pic.user"
			}
			if _, err := s.UpdateParticipantAccount(context.Background(), "panel", ParticipantAccountEdit{ProjectVersion: version, Name: "Changed", Username: username}, "admin"); err == nil {
				t.Fatal("unsafe edit accepted")
			}
			if kind != "collision" {
				if err := s.RemoveParticipantAccount(context.Background(), "panel", version, "admin"); err == nil {
					t.Fatal("unsafe delete accepted")
				}
			}
			if st.project.Version != 4 {
				t.Fatal("partial write")
			}
		})
	}
}
func TestAccountManagementTransactionRollsBackUserOnProjectFailure(t *testing.T) {
	s, st := accountManagementFixture()
	st.failSave = true
	_, err := s.UpdateParticipantAccount(context.Background(), "panel", ParticipantAccountEdit{ProjectVersion: 4, Name: "Changed", Username: "edit.user"}, "admin")
	if err == nil || st.users["panel"].Username != "panel.user" || st.project.Version != 4 {
		t.Fatal("partial transaction")
	}
}

func TestAccountManagementMongoRemovalRetainsScoringHistory(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	if _, err := s.SaveSubmission(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(70)}); err != nil {
		t.Fatal(err)
	}
	before, err := st.ListSubmissions(ctx, p.ID, "MAT", 1, "p1")
	if err != nil || len(before) != 1 {
		t.Fatalf("submission setup: %v", err)
	}
	if err = s.RemoveParticipantAccount(ctx, "p1", p.Version, "admin"); err != nil {
		t.Fatal(err)
	}
	after, err := st.ListSubmissions(ctx, p.ID, "MAT", 1, "p1")
	if err != nil || len(after) != 1 || after[0].ID != before[0].ID || after[0].Version != before[0].Version {
		t.Fatalf("history rewritten: %+v %v", after, err)
	}
	u, err := st.FindByID(ctx, "p1")
	if err != nil || !u.Removed || u.Active || u.AuthVersion != 1 {
		t.Fatalf("removal not persisted: %+v %v", u, err)
	}
	saved, err := st.GetProject(ctx, p.ID)
	if err != nil || saved.Version != p.Version+1 || contains(saved.Subjects[0].PanelistUserIDs, "p1") {
		t.Fatalf("roster not persisted: %+v %v", saved, err)
	}
}

func TestAccountManagementMongoCreatesAndEditsParticipant(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	u, err := s.CreateParticipantAccount(ctx, ParticipantAccountInput{ProjectID: p.ID, ProjectVersion: p.Version, SubjectCode: "MAT", Role: domain.RolePanelist, Name: "Peserta baru", Username: "qa.new.participant"}, "admin")
	if err != nil {
		t.Fatalf("create new account: %v", err)
	}
	stored, err := st.FindByID(ctx, u.ID)
	if err != nil || !stored.Active || !stored.MustChangePassword || stored.SlotID == "" {
		t.Fatalf("new account persistence %+v %v", stored, err)
	}
	if bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("12345*")) != nil {
		t.Fatal("new account initial password incorrect")
	}
	fresh, _ := st.GetProject(ctx, p.ID)
	if fresh.Subjects[0].PanelistCount != 3 || !contains(fresh.Subjects[0].PanelistUserIDs, u.ID) {
		t.Fatal("new account not assigned")
	}
	edited, err := s.UpdateParticipantAccount(ctx, u.ID, ParticipantAccountEdit{ProjectVersion: fresh.Version, Name: "Peserta baru", Username: "qa.new.updated"}, "admin")
	if err != nil || edited.AuthVersion != stored.AuthVersion+1 || edited.PasswordHash != stored.PasswordHash {
		t.Fatalf("edit new account %+v %v", edited, err)
	}
}
