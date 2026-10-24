package service

import (
	"errors"
	"standard-setting/backend/internal/domain"
	"testing"
	"time"
)

func TestContactPolicyGateAndVersionedUpdate(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.CollectContactPanelis = true
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PanelistForm(ctx, "p1", p.ID, "MAT"); !errors.Is(err, ErrContactRequired) {
		t.Fatalf("missing contact bypass: %v", err)
	}
	contact, err := s.UpdateContact(ctx, "p1", ContactInput{WhatsApp: "081234567890", Version: 0})
	if err != nil || contact.WhatsApp != "+6281234567890" || contact.Version != 1 {
		t.Fatalf("contact update: %+v %v", contact, err)
	}
	if _, err = s.PanelistForm(ctx, "p1", p.ID, "MAT"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateContact(ctx, "p1", ContactInput{Email: "a@example.org", Version: 0}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale contact accepted: %v", err)
	}
	if _, err = s.UpdateContact(ctx, "pic", ContactInput{Email: "a@example.org"}); err == nil {
		t.Fatal("PIC modified panelist contact")
	}
}

func TestSubmissionPeriodAndCandidateContextEnforced(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.StartDate = time.Now().Add(time.Hour)
	p.EndDate = p.StartDate.Add(time.Hour)
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	form, err := s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if err != nil || !form.ReadOnly {
		t.Fatalf("future period form: %+v %v", form, err)
	}
	a := domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(65)}
	if _, err = s.SaveSubmission(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", a); !errors.Is(err, ErrPeriodClosed) {
		t.Fatalf("future period save: %v", err)
	}
	p.StartDate = time.Now().Add(-time.Minute)
	p.EndDate = time.Now().Add(time.Hour)
	p.FeatureVersion = 1
	p.Subjects[0].CandidateVersion = 2
	p.Subjects[0].CandidateDefinition = "Kandidat konteks baru"
	if err = st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveSubmissionWithOptions(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", a, SubmissionOptions{RoundRevision: 1, CandidateVersion: 1}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("old context accepted: %v", err)
	}
}

func TestRoundExclusionIsLocalAndPreservesResponses(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if err := s.SetRoundExclusion(ctx, "pic", "round-1", "p2", "Berhalangan hadir", 1, true); err != nil {
		t.Fatal(err)
	}
	m, err := s.Monitoring(ctx, p.ID, "MAT")
	if err != nil || len(m.Panelists) != 1 || m.Statistics == nil || m.Statistics.PanelistCount != 1 || len(m.Excluded) != 1 {
		t.Fatalf("effective roster: %+v %v", m, err)
	}
	u, _ := st.FindByID(ctx, "p2")
	rows, _ := st.ListSubmissions(ctx, p.ID, "MAT", 1, "p2")
	if !u.Active || len(rows) != 3 {
		t.Fatal("exclusion changed global account or deleted responses")
	}
	if err = s.SetRoundExclusion(ctx, "pic", "round-1", "p1", "", 1, true); err == nil {
		t.Fatal("missing reason accepted")
	}
	if err = s.SetRoundExclusion(ctx, "p1", "round-1", "p1", "No permission", 1, true); err == nil {
		t.Fatal("panelist excluded another user")
	}
	if _, err = s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	current, _ := st.CurrentRound(ctx, p.ID, "MAT")
	if len(current.ExcludedPanelists) != 0 {
		t.Fatal("exclusion leaked to next round")
	}
	old, _ := st.ListRounds(ctx, p.ID, "MAT")
	if old[0].FinalSnapshot == nil || len(old[0].FinalSnapshot.PanelistIDs) != 1 || len(old[0].FinalSnapshot.Submissions) != 6 {
		t.Fatalf("snapshot inclusion/provenance missing: %+v", old[0])
	}
}

func TestRollbackRequiresEveryResponseReviewAndRejectsStaleQueue(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "rollback", "ROLLBACK PUTARAN", "pic", 2, 1); err != nil {
		t.Fatal(err)
	}
	current, _ := st.CurrentRound(ctx, p.ID, "MAT")
	if domain.RoundRevision(current) != 2 || len(current.Snapshots) != 1 {
		t.Fatalf("rollback lost revision history: %+v", current)
	}
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 2); err == nil {
		t.Fatal("rollback finalized without review")
	}
	a := domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(65)}
	if _, err := s.SaveSubmissionWithOptions(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", a, SubmissionOptions{RoundRevision: 1}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("old queue accepted: %v", err)
	}
	for _, id := range []string{"p1", "p2"} {
		rows, _ := st.ListSubmissions(ctx, p.ID, "MAT", 1, id)
		for _, row := range rows {
			if _, err := s.ReviewSubmission(ctx, id, p.ID, "MAT", 1, row.ItemID, 2, row.Version); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 2); err != nil {
		t.Fatal(err)
	}
	rounds, _ := st.ListRounds(ctx, p.ID, "MAT")
	if len(rounds[0].Snapshots) != 2 || rounds[0].FinalSnapshot.Revision != 2 || rounds[0].Snapshots[0].Revision != 1 {
		t.Fatal("historical snapshot overwritten")
	}
}

func TestSubmissionCASAndIdempotencyDoNotOverwriteNewerWork(t *testing.T) {
	ctx, s, _, p := fixture(t, true)
	a := domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(65)}
	first, err := s.SaveSubmissionWithOptions(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", a, SubmissionOptions{RoundRevision: 1, Version: 0, IdempotencyKey: "job-1", Note: "Perlu dibahas"})
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.SaveSubmissionWithOptions(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", a, SubmissionOptions{RoundRevision: 1, Version: 0, IdempotencyKey: "job-1", Note: "Perlu dibahas"})
	if err != nil || retry.Version != first.Version {
		t.Fatalf("non-idempotent retry: %+v %v", retry, err)
	}
	a.Value = intValue(80)
	if _, err = s.SaveSubmissionWithOptions(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", a, SubmissionOptions{RoundRevision: 1, Version: 0, IdempotencyKey: "job-2"}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale write overwrote: %v", err)
	}
}

func TestProjectCopyRetainsConfigurationNotAccountsOrItems(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.Subjects[0].CandidateDefinition = "Kandidat batas lulus"
	p.CollectContactPanelis = true
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	copied, err := s.CopyProject(ctx, p.ID, "Proyek baru", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if copied.ID == p.ID || copied.Code == p.Code || copied.Status != domain.ProjectDraft || copied.SourceProjectID != p.ID || !copied.CollectContactPanelis || !copied.StartDate.IsZero() {
		t.Fatalf("copy metadata: %+v", copied)
	}
	sub := copied.Subjects[0]
	if len(sub.ItemIDs) != 0 || sub.PICUserID != "" || len(sub.PanelistUserIDs) != 0 || sub.CandidateDefinition == "" {
		t.Fatalf("copy data leakage: %+v", sub)
	}
	users, _ := st.ListUsers(ctx, copied.ID)
	if len(users) != 0 {
		t.Fatal("copied credentials")
	}
}

func TestPanelistPayloadDoesNotDiscloseFrozenResponses(t *testing.T) {
	ctx, s, _, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	f, err := s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Feedback) != 3 || !f.Feedback[0].SmallPanel || len(f.Feedback[0].Values) != 0 || f.Feedback[0].OwnValue == nil {
		t.Fatalf("incorrect anonymous feedback: %+v", f.Feedback)
	}
	for _, r := range append(f.History, f.Round) {
		if r.FinalSnapshot != nil || len(r.Snapshots) > 0 || len(r.ExcludedPanelists) > 0 {
			t.Fatal("panelist received frozen answers or private roster")
		}
	}
	d, err := s.Discussion(ctx, "pic", "MAT-001", 1)
	if err != nil || d.OwnValue != nil || len(d.Values) != 0 {
		t.Fatalf("discussion privacy: %+v %v", d, err)
	}
	if _, err = s.Discussion(ctx, "p1", "MAT-001", 1); err == nil {
		t.Fatal("panelist accessed PIC discussion")
	}
}
func TestFinalSnapshotDoesNotChangeAfterRosterDeactivation(t *testing.T) {
	ctx, s, _, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserActive(ctx, "p2", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteProject(ctx, p.ID, "SELESAIKAN PROYEK"); err != nil {
		t.Fatal(err)
	}
	r, err := s.ItemResult(ctx, "MAT-001")
	if err != nil || r.Statistics.PanelistCount != 2 || r.Statistics.Mean != 70 || len(r.Responses) != 1 {
		t.Fatalf("final history was recomputed: %+v %v", r, err)
	}
}

func TestReopenedLaterRoundRejectsQueueFromBeforeRollback(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "rollback", "ROLLBACK PUTARAN", "pic", 2, 1); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"p1", "p2"} {
		rows, _ := st.ListSubmissions(ctx, p.ID, "MAT", 1, id)
		for _, r := range rows {
			if _, err := s.ReviewSubmission(ctx, id, p.ID, "MAT", 1, r.ItemID, 2, r.Version); err != nil {
				t.Fatal(err)
			}
		}
	}
	reopened, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if domain.RoundRevision(reopened) != 2 {
		t.Fatalf("later round reused old authorization revision: %+v", reopened)
	}
	_, err = s.SaveSubmissionWithOptions(ctx, "p1", "", p.ID, "MAT", 2, "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(65)}, SubmissionOptions{RoundRevision: 1, Version: 0, IdempotencyKey: "queued-before-rollback"})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("old later-round queue accepted: %v", err)
	}
}

func TestCandidateContextHistorySurvivesEditsBeforeFinalization(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.FeatureVersion = 1
	p.Subjects[0].CandidateDefinition = "Kandidat v1"
	p.Subjects[0].CandidateVersion = 1
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSubmissionWithOptions(ctx, "p1", "", p.ID, "MAT", 1, "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(60)}, SubmissionOptions{RoundRevision: 1, CandidateVersion: 1}); err != nil {
		t.Fatal(err)
	}
	p.Subjects[0].CandidateDefinition = "Kandidat v2"
	updated, err := s.SaveProject(ctx, p, "admin")
	if err != nil {
		t.Fatal(err)
	}
	h := updated.Subjects[0].CandidateHistory
	if len(h) != 2 || h[0].Version != 1 || h[0].Definition != "Kandidat v1" || h[1].Version != 2 || h[1].Definition != "Kandidat v2" {
		t.Fatalf("context lost: %+v", h)
	}
	p = updated
	p.Subjects[0].CandidateHistory = nil
	p.Subjects[0].CandidateDefinition = "Kandidat v3"
	updated, err = s.SaveProject(ctx, p, "admin")
	if err != nil || len(updated.Subjects[0].CandidateHistory) != 3 {
		t.Fatalf("client erased history: %+v %v", updated, err)
	}
}

func TestReviewRequiresTheCandidateContextActuallySeen(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.FeatureVersion = 1
	p.Subjects[0].CandidateVersion = 1
	p.Subjects[0].CandidateDefinition = "Kandidat pertama"
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	row, err := s.SaveSubmissionWithOptions(ctx, "p1", "", p.ID, "MAT", 1, "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(60)}, SubmissionOptions{RoundRevision: 1, CandidateVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	p.Subjects[0].CandidateDefinition = "Kandidat kedua"
	p, err = s.SaveProject(ctx, p, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReviewSubmission(ctx, "p1", p.ID, "MAT", 1, "MAT-001", 1, row.Version, 1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("unseen candidate acknowledged: %v", err)
	}
	if _, err = s.ReviewSubmission(ctx, "p1", p.ID, "MAT", 1, "MAT-001", 1, row.Version, p.Subjects[0].CandidateVersion); err != nil {
		t.Fatal(err)
	}
}
