package service

import (
	"golang.org/x/crypto/bcrypt"
	"standard-setting/backend/internal/domain"
	"testing"
)

func TestActiveConfigurationLocksItemsAfterResponse(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.Name = "Changed"
	updated, err := s.SaveProject(ctx, p, "admin")
	if err != nil || updated.Code != p.Code || updated.Status != domain.ProjectActive {
		t.Fatalf("active info edit: %+v %v", updated, err)
	}
	if _, err = s.SaveSubmission(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(70)}); err != nil {
		t.Fatal(err)
	}
	updated.Subjects[0].ItemIDs = []string{"MAT-001"}
	if _, err = s.SaveProject(ctx, updated, "admin"); err == nil {
		t.Fatal("edited items after scoring")
	}
	saved, _ := st.GetProject(ctx, p.ID)
	if len(saved.Subjects[0].ItemIDs) != 3 {
		t.Fatal("partial item removal")
	}
}
func TestRegeneratePreservesIdentityPasswordAndInactiveState(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	old, _ := st.FindByID(ctx, "p1")
	hash, _ := bcrypt.GenerateFromPassword([]byte("existing-password"), bcrypt.MinCost)
	old.PasswordHash = string(hash)
	old.Active = false
	if err := st.SaveUser(ctx, old); err != nil {
		t.Fatal(err)
	}
	p.Subjects[0].PanelistCount = 3
	p.Subjects[0].PanelistNames = []string{"A", "B", "New"}
	saved, err := s.SaveProject(ctx, p, "admin")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.AccountRows(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	inputs := []AccountInput{}
	for _, row := range rows {
		if row.Status != "removed" {
			inputs = append(inputs, AccountInput{SlotID: row.SlotID, Name: row.Name, Role: row.Role, SubjectCode: row.SubjectCode, Username: row.Username})
		}
	}
	if _, err = s.GenerateAccounts(ctx, p.ID, inputs); err != nil {
		t.Fatal(err)
	}
	after, _ := st.FindByID(ctx, "p1")
	if after.Active || after.ID != old.ID || after.PasswordHash != old.PasswordHash {
		t.Fatal("regeneration changed old identity/password/access")
	}
	again, _ := s.GenerateAccounts(ctx, p.ID, inputs)
	if len(again) != 4 {
		t.Fatalf("retry roster: %v %+v", saved, again)
	}
}
func TestRemovedSlotIsNotRecycled(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.Subjects[0].PanelistCount = 1
	p.Subjects[0].PanelistNames = []string{"A"}
	saved, err := s.SaveProject(ctx, p, "admin")
	if err != nil {
		t.Fatal(err)
	}
	removed, _ := st.FindByID(ctx, "p2")
	if removed.Active || !removed.Removed {
		t.Fatal("removed panelist still operational")
	}
	saved.Subjects[0].PanelistCount = 2
	saved.Subjects[0].PanelistNames = []string{"A", "New"}
	if _, err = s.SaveProject(ctx, saved, "admin"); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.AccountRows(ctx, p.ID)
	for _, r := range rows {
		if r.Status == "pending" && r.UserID == "p2" {
			t.Fatal("new slot recycled removed user")
		}
	}
}
func TestCatalogUniquenessAndReset(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	if _, err := s.CreateSubject(ctx, "  Sejarah  Dunia ", "SEJ"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSubject(ctx, "sejarah dunia", "SEJ2"); err == nil {
		t.Fatal("duplicate mapel accepted")
	}
	if err := s.ResetPassword(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	u, _ := st.FindByID(ctx, "p1")
	if !u.MustChangePassword || u.AuthVersion < 1 || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("12345*")) != nil {
		t.Fatalf("reset failed on %s", p.ID)
	}
}

func TestStalePasswordChangeCannotOverwriteAdminReset(t *testing.T) {
	ctx, s, st, _ := fixture(t, true)
	before, _ := st.FindByID(ctx, "p1")
	if err := s.ResetPassword(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdatePasswordIfVersion(ctx, before.ID, before.AuthVersion, "stale-hash", false); err == nil {
		t.Fatal("stale password update overwrote admin reset")
	}
	after, _ := st.FindByID(ctx, "p1")
	if !after.MustChangePassword || after.PasswordHash == "stale-hash" {
		t.Fatal("forced reset lost")
	}
}
func TestInactiveResponsesDoNotBlockCompletion(t *testing.T) {
	ctx, s, _, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSubmission(ctx, "p2", "B", p.ID, "MAT", 2, "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(70)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserActive(ctx, "p2", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteProject(ctx, p.ID, "SELESAIKAN PROYEK"); err != nil {
		t.Fatalf("inactive response blocked completion: %v", err)
	}
}

func TestConcurrentScoringAndItemRemovalCannotBothCommit(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	copyProject := p
	copyProject.Subjects = append([]domain.ProjectSubject(nil), p.Subjects...)
	copyProject.Subjects[0].ItemIDs = []string{"MAT-002", "MAT-003"}
	ready := make(chan struct{})
	result := make(chan error, 2)
	go func() { <-ready; _, err := s.SaveProject(ctx, copyProject, "admin"); result <- err }()
	go func() {
		<-ready
		_, err := s.SaveSubmission(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(65)})
		result <- err
	}()
	close(ready)
	a, b := <-result, <-result
	if a == nil && b == nil {
		t.Fatal("scoring and removal both committed")
	}
	saved, _ := st.GetProject(ctx, p.ID)
	rows, _ := st.ListSubmissions(ctx, p.ID, "MAT", 1, "p1")
	if !contains(saved.Subjects[0].ItemIDs, "MAT-001") && len(rows) > 0 {
		t.Fatal("saved response points to removed item")
	}
}

func TestInactiveParticipantHiddenFromItemResultsWithoutRewritingSnapshot(t *testing.T) {
	ctx, s, _, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteProject(ctx, p.ID, "SELESAIKAN PROYEK"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserActive(ctx, "p2", false); err != nil {
		t.Fatal(err)
	}
	result, err := s.ItemResult(ctx, "MAT-001")
	if err != nil || len(result.Responses) != 1 || result.Responses[0].PanelistID != "p1" || result.Statistics.PanelistCount != 2 {
		t.Fatalf("inactive result exposure or snapshot rewrite %+v %v", result, err)
	}
}

func TestSnapshotRosterDoesNotAcquireReactivatedExcludedParticipant(t *testing.T) {
	ctx, s, _, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if err := s.SetUserActive(ctx, "p2", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteProject(ctx, p.ID, "SELESAIKAN PROYEK"); err != nil {
		t.Fatal(err)
	}
	s.SetUserActive(ctx, "p2", true)
	s.SetUserActive(ctx, "p1", false)
	result, err := s.ItemResult(ctx, "MAT-001")
	if err != nil || len(result.Responses) != 0 || result.Statistics.PanelistCount != 1 || result.Warning == "" {
		t.Fatalf("snapshot admitted wrong person: %+v %v", result, err)
	}
}
