package service

import (
	"context"
	"standard-setting/backend/internal/domain"
	"testing"
)

func TestReviewRangeThresholdConfigDefaultPersistAndCompatibility(t *testing.T) {
	ctx := context.Background()
	st := &instructionMemory{actor: domain.User{ID: "admin", Role: domain.RoleAdminSuper, Active: true}}
	s := New(st, nil, "")
	initial, err := s.QuestionInstructions(ctx)
	if err != nil || initial.ReviewRangeThreshold != 20 {
		t.Fatal("default threshold", initial, err)
	}
	threshold := 15
	saved, err := s.SaveQuestionInstructionSettings(ctx, "admin", 0, initial.Instructions, nil, &threshold)
	if err != nil || saved.ReviewRangeThreshold != 15 {
		t.Fatal("threshold not saved", saved, err)
	}
	saved, err = s.SaveQuestionInstructions(ctx, "admin", saved.Version, saved.Instructions)
	if err != nil || saved.ReviewRangeThreshold != 15 {
		t.Fatal("legacy save reset threshold", saved, err)
	}
	fresh, err := s.QuestionInstructions(ctx)
	if err != nil || fresh.ReviewRangeThreshold != 15 {
		t.Fatal("threshold not reloaded", fresh, err)
	}
	for _, invalid := range []int{-1, 0, 101} {
		if _, err = s.SaveQuestionInstructionSettings(ctx, "admin", fresh.Version, fresh.Instructions, nil, &invalid); err == nil {
			t.Fatal("invalid threshold saved", invalid)
		}
	}
	if _, err = s.SaveQuestionInstructionSettings(ctx, "admin", 0, fresh.Instructions, nil, &threshold); err != domain.ErrInstructionConflict {
		t.Fatal("stale threshold save accepted", err)
	}
}

func TestPanelistPrefillAndRangeUseImmediatelyPreviousSnapshot(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	// Live rows may later differ; inheritance must use the locked snapshot.
	rows, err := st.ListSubmissions(ctx, p.ID, "MAT", 1, "p1")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ItemID == "MAT-001" {
			row.Answer.Value = intValue(99)
			if _, err = st.UpsertSubmission(ctx, row); err != nil {
				t.Fatal(err)
			}
		}
	}
	form, err := s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if err != nil || form.ReviewRangeThreshold != 20 || form.PreviousRound == nil || *form.PreviousRound != 1 {
		t.Fatal(form, err)
	}
	found := false
	for _, row := range form.PreviousSubmissions {
		if row.ItemID == "MAT-001" {
			found = true
			if *row.Answer.Value != 60 {
				t.Fatal("live prior row used instead of snapshot", row)
			}
		}
	}
	if !found {
		t.Fatal("missing frozen prefill")
	}
	if form.Feedback[0].RoundNumber != 1 || form.Feedback[0].Maximum-form.Feedback[0].Minimum != 20 {
		t.Fatal("wrong R1 flag source", form.Feedback)
	}
	for _, id := range []string{"p1", "p2"} {
		if _, err = s.StartPanelistRound(ctx, id, 2, 1); err != nil {
			t.Fatal(err)
		}
	}
	fillRound(t, ctx, s, p, 2, 0)
	for _, entry := range []struct {
		id    string
		score int
	}{{"p1", 65}, {"p2", 70}} {
		if _, err = s.SaveSubmission(ctx, entry.id, entry.id, p.ID, "MAT", 2, "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(entry.score)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.SaveSubmission(ctx, "p2", "B", p.ID, "MAT", 2, "MAT-003", domain.Answer{QuestionType: domain.QuestionTypeEssay, Distribution: []int{0, 0, 0, 100, 0}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 2, 1); err != nil {
		t.Fatal(err)
	}
	form, err = s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if err != nil || form.Round.Number != 3 || form.PreviousRound == nil || *form.PreviousRound != 2 {
		t.Fatal("wrong previous round", form, err)
	}
	for _, row := range form.PreviousSubmissions {
		if row.ItemID == "MAT-001" && *row.Answer.Value != 65 {
			t.Fatal("R3 did not inherit R2", row)
		}
	}
	for _, f := range form.Feedback {
		if f.RoundNumber != 2 {
			t.Fatal("stale flags inherited from R1", f)
		}
		if f.ItemID == "MAT-001" && f.Maximum-f.Minimum != 5 {
			t.Fatal("wrong updated PG range", f)
		}
		if f.ItemID == "MAT-003" && f.Maximum-f.Minimum != 25 {
			t.Fatal("wrong updated essay range", f)
		}
	}
}
