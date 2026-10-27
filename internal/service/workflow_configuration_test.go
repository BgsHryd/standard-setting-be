package service

import (
	"errors"
	"testing"
	"time"

	"standard-setting/backend/internal/domain"
)

func TestAddedActiveSubjectRequiresPICToEndPractice(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.WorkflowVersion = 1
	p.PlannedRounds = 3
	p.SimulationEnabled = true
	p.Subjects[0].PracticeStatus = "open"
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertItems(ctx, []domain.Item{{ID: "BIN-001", SubjectCode: "BIN", SubjectName: "Bahasa Indonesia", QuestionType: domain.QuestionTypePG, MaxScore: 1, UsageStatus: domain.ItemAvailable}}); err != nil {
		t.Fatal(err)
	}
	p.Subjects = append(p.Subjects, domain.ProjectSubject{Code: "BIN", PanelistCount: 1, PanelistNames: []string{"C"}, PICName: "PIC BIN", CandidateDefinition: "Kandidat", ItemIDs: []string{"BIN-001"}})
	saved, err := s.SaveProject(ctx, p, "admin")
	if err != nil {
		t.Fatal(err)
	}
	inputs := []AccountInput{}
	for _, sub := range saved.Subjects {
		for _, slot := range sub.Slots {
			inputs = append(inputs, AccountInput{SlotID: slot.ID, Name: slot.Name, Role: slot.Role, SubjectCode: sub.Code})
		}
	}
	users, err := s.GenerateAccounts(ctx, p.ID, inputs)
	if err != nil {
		t.Fatal(err)
	}
	panelist, pic := "", ""
	for _, u := range users {
		if u.SubjectCode == "BIN" && u.Role == domain.RolePanelist {
			panelist = u.ID
		}
		if u.SubjectCode == "BIN" && u.Role == domain.RolePICMapel {
			pic = u.ID
		}
	}
	if panelist == "" || pic == "" {
		t.Fatal("new subject accounts were not generated")
	}
	practice, err := s.StartPractice(ctx, panelist)
	if err != nil || !practice.Open {
		t.Fatalf("new subject practice was not opened: %+v %v", practice, err)
	}
	answer := domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(70)}
	options := SubmissionOptions{RoundRevision: 1, CandidateVersion: saved.Subjects[1].CandidateVersion}
	if _, err = s.SaveSubmissionWithOptions(ctx, panelist, "C", p.ID, "BIN", 1, "BIN-001", answer, options); err == nil {
		t.Fatal("new subject allowed real scoring before PIC ended practice")
	}
	fresh, err := st.GetProject(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EndSubjectPractice(ctx, pic, p.ID, "BIN", fresh.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveSubmissionWithOptions(ctx, panelist, "C", p.ID, "BIN", 1, "BIN-001", answer, options); err != nil {
		t.Fatalf("PIC could not open real scoring: %v", err)
	}
}

func TestPracticeRejectsStartBeforeProjectPeriod(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.WorkflowVersion = 1
	p.PlannedRounds = 1
	p.SimulationEnabled = true
	p.Subjects[0].PracticeStatus = "open"
	p.StartDate = time.Now().Add(time.Hour)
	p.EndDate = p.StartDate.Add(time.Hour)
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartPractice(ctx, "p1"); err == nil {
		t.Fatal("practice started before project period")
	}
	view, err := s.GetPractice(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if !view.ReadOnly || !view.Session.StartedAt.IsZero() {
		t.Fatalf("practice outside project period remained writable or consumed the timer: %+v", view)
	}
}

func TestPracticeFinishRejectsClosedProjectPeriod(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.SimulationEnabled = true
	p.Subjects[0].PracticeStatus = "open"
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	practice, err := s.StartPractice(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	p.EndDate = time.Now().Add(-time.Second)
	if err = st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinishPractice(ctx, "p1", practice.Session.Version); !errors.Is(err, ErrPeriodClosed) {
		t.Fatalf("practice finished outside period: %v", err)
	}
}
