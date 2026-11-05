package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"standard-setting/backend/internal/domain"
)

func TestBetweenRoundsUsesFrozenResultsAndPersistentStart(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	f, err := s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if err != nil || !f.RoundStartRequired || f.RoundReview == nil || f.RoundReview.RoundNumber != 1 || len(f.RoundOutcomes) != 1 || len(f.RoundReview.Items) != 3 {
		t.Fatal("missing review gate", err)
	}
	if len(f.RoundReview.Items[1].OwnDistribution) != 4 || len(f.RoundReview.Items[1].PanelDistribution) != 0 || len(f.RoundReview.Items[0].OwnDistribution) != 0 {
		t.Fatal("overview distribution contract incorrect for small panel/PG")
	}
	out := f.RoundOutcomes[0]
	if out.NBKPercent != 71.25 || out.MaxScore == nil || *out.MaxScore != 8 || out.NBKScore == nil || *out.NBKScore != 5.7 || out.ProjectionPercent != nil {
		t.Fatalf("wrong frozen outcome: %+v", out)
	}
	encoded, _ := json.Marshal(f.RoundReview)
	for _, field := range []string{"panelistIds", "roster", "submissions", "PIC QA", `"p1"`, `"p2"`} {
		if strings.Contains(string(encoded), field) {
			t.Fatal("review leaked participant identity", field)
		}
	}
	if _, err := s.StartPanelistRound(ctx, "pic", 2, 1); err == nil {
		t.Fatal("PIC started panelist round")
	}
	if _, err := s.StartPanelistRound(ctx, "p1", 1, 1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale start accepted", err)
	}
	start, err := s.StartPanelistRound(ctx, "p1", 2, 1)
	if err != nil || start.StartedAt.IsZero() {
		t.Fatal("start failed", err)
	}
	again, err := s.StartPanelistRound(ctx, "p1", 2, 1)
	if err != nil || !again.StartedAt.Equal(start.StartedAt) {
		t.Fatal("start was not idempotent", err)
	}
	f, _ = New(st, nil, "12345*").PanelistForm(ctx, "p1", p.ID, "MAT")
	if f.RoundStartRequired || f.RoundStartedAt == nil {
		t.Fatal("start not retained across service reload")
	}
	other, _ := s.PanelistForm(ctx, "p2", p.ID, "MAT")
	if !other.RoundStartRequired {
		t.Fatal("start leaked to another panelist")
	}
	rows, _ := st.ListSubmissions(ctx, p.ID, "MAT", 2, "p1")
	if len(rows) != 0 {
		t.Fatal("start created grading answers")
	}
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "rollback", "ROLLBACK PUTARAN", "pic", 2, 1); err != nil {
		t.Fatal(err)
	}
	f, _ = s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if f.RoundStartRequired || len(f.RoundOutcomes) != 0 {
		t.Fatal("rollback exposed invalidated final results or forced start")
	}
}

func TestDiscussionNotesAreVersionedSeparateAndVisibleToPIC(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	note, err := s.SaveDiscussionNote(ctx, "p1", "MAT-001", DiscussionNoteInput{RoundNumber: 2, RoundRevision: 1, Text: "Perlu dibahas bersama PIC"})
	if err != nil || note.Version != 1 {
		t.Fatal("note not saved", err)
	}
	if _, err := s.SaveDiscussionNote(ctx, "p1", "MAT-001", DiscussionNoteInput{RoundNumber: 2, RoundRevision: 1, Text: "stale"}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale note accepted", err)
	}
	if _, err := s.SaveDiscussionNote(ctx, "p1", "OTHER", DiscussionNoteInput{RoundNumber: 2, RoundRevision: 1, Text: "wrong scope"}); err == nil {
		t.Fatal("foreign item accepted")
	}
	if _, err := s.SaveDiscussionNote(ctx, "pic", "MAT-001", DiscussionNoteInput{RoundNumber: 2, RoundRevision: 1, Text: "wrong role"}); err == nil {
		t.Fatal("PIC wrote another panelist note")
	}
	f, _ := s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if len(f.DiscussionNotes) != 1 || !f.RoundStartRequired {
		t.Fatal("note created scoring or bypassed start")
	}
	other, _ := s.PanelistForm(ctx, "p2", p.ID, "MAT")
	if len(other.DiscussionNotes) != 0 {
		t.Fatal("another panelist received private note")
	}
	rows, _ := st.ListSubmissions(ctx, p.ID, "MAT", 2, "")
	if len(rows) != 0 {
		t.Fatal("discussion note modified grading")
	}
	m, err := s.Monitoring(ctx, p.ID, "MAT")
	if err != nil || len(m.Notes) != 1 || m.Notes[0].PanelistID != "p1" {
		t.Fatal("PIC cannot see discussion note", err)
	}
	note, err = s.SaveDiscussionNote(ctx, "p1", "MAT-001", DiscussionNoteInput{RoundNumber: 2, RoundRevision: 1, Version: 1, Text: ""})
	if err != nil || note.Version != 2 || note.Text != "" {
		t.Fatal("note could not be cleared", err)
	}
}

func TestReviewOutcomesUseMatchingImpactAndProtectReadonlyStart(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	rounds, _ := st.ListRounds(ctx, p.ID, "MAT")
	id := rounds[0].FinalSnapshot.ID
	if err := st.SaveImpactBatch(ctx, domain.ImpactBatch{ID: "impact-qa", ProjectID: p.ID, Scores: []domain.ImpactScore{{ParticipantID: "participant-private", SubjectCode: "MAT", Score: 80}}, Subjects: []domain.ImpactSubject{{SubjectCode: "MAT", CutoffSnapshotID: id, Count: 10, Percentage: 40}}}); err != nil {
		t.Fatal(err)
	}
	summaries, err := st.ListImpactSummaries(ctx, p.ID)
	if err != nil || len(summaries) != 1 || len(summaries[0].Scores) != 0 {
		t.Fatal("impact summaries failed", err)
	}
	f, _ := s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if f.RoundOutcomes[0].ProjectionPercent == nil || *f.RoundOutcomes[0].ProjectionPercent != 40 {
		t.Fatal("matching impact aggregate not used")
	}
	encoded, _ := json.Marshal(f.RoundReview)
	if strings.Contains(string(encoded), "participant-private") {
		t.Fatal("raw participant score leaked")
	}
	p.Status = domain.ProjectCompleted
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartPanelistRound(ctx, "p1", 2, 1); err == nil {
		t.Fatal("completed project start accepted")
	}
}

func TestBetweenRoundsPreservesFirstRoundExistingAnswersAndScopes(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	first, err := s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if err != nil || first.RoundStartRequired || first.RoundReview != nil {
		t.Fatal("first-round flow changed", err)
	}
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	value := 0
	if _, err := s.SaveSubmission(ctx, "p1", "A", p.ID, "MAT", 2, "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: &value}); err != nil {
		t.Fatal(err)
	}
	f, err := s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if err != nil || f.RoundStartRequired {
		t.Fatal("existing answers were gated", err)
	}
	for _, in := range []DiscussionNoteInput{
		{RoundNumber: 1, RoundRevision: 1, Text: "stale"},
		{RoundNumber: 2, RoundRevision: 2, Text: "stale revision"},
		{RoundNumber: 2, RoundRevision: 1, Text: strings.Repeat("あ", 1001)},
	} {
		if _, err := s.SaveDiscussionNote(ctx, "p1", "MAT-001", in); err == nil {
			t.Fatal("invalid note accepted", in.RoundNumber, in.RoundRevision)
		}
	}
	u, _ := st.FindByID(ctx, "p2")
	u.Active = false
	if err := st.SaveUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartPanelistRound(ctx, "p2", 2, 1); err == nil {
		t.Fatal("inactive panelist started")
	}
	u.Active = true
	u.Removed = true
	if err := st.SaveUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDiscussionNote(ctx, "p2", "MAT-001", DiscussionNoteInput{RoundNumber: 2, RoundRevision: 1, Text: "removed"}); err == nil {
		t.Fatal("removed panelist wrote note")
	}
	p.EndDate = time.Now().Add(-time.Hour)
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartPanelistRound(ctx, "p1", 2, 1); !errors.Is(err, ErrPeriodClosed) {
		t.Fatal("closed period accepted", err)
	}
}
