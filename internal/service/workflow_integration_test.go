package service

import (
	"errors"
	"standard-setting/backend/internal/domain"
	"testing"
	"time"
)

func TestExplicitSubmissionLocksAndFinalNBKGatesEvaluation(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.WorkflowVersion = 1
	p.PlannedRounds = 1
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err == nil {
		t.Fatal("finalized before explicit submit")
	}
	receipt, err := s.SubmitPanelistRound(ctx, "p1", 1, 1, true)
	if err != nil || receipt.SubmittedAt == nil {
		t.Fatalf("submit: %+v %v", receipt, err)
	}
	form, err := s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if err != nil || !form.ReadOnly || form.SubmittedAt == nil {
		t.Fatalf("submitted form unlocked: %+v %v", form, err)
	}
	if _, err = s.SaveSubmission(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(75)}); err == nil {
		t.Fatal("changed submitted answers")
	}
	if _, err = s.SubmitPanelistRound(ctx, "p1", 1, 1, false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitPanelistRound(ctx, "p1", 1, 1, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitPanelistRound(ctx, "p2", 1, 1, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = st.CurrentRound(ctx, p.ID, "MAT"); err == nil {
		t.Fatal("opened beyond planned rounds")
	}
	form, err = s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if err != nil || !form.AllRoundsDone || !form.ReadOnly {
		t.Fatalf("last round display: %+v %v", form, err)
	}
	view, err := s.GetEvaluation(ctx, "p1")
	if err != nil || !view.Closed {
		t.Fatalf("evaluation opened before NBK: %+v %v", view, err)
	}
	eval := domain.Evaluation{Confidence: 3, Training: 4, TimeAdequacy: 3}
	if _, err = s.SaveEvaluation(ctx, "p1", eval); err == nil {
		t.Fatal("saved evaluation before NBK")
	}
	if _, err = s.LockFinalNBK(ctx, "p1", p.ID, "MAT", 1, 1, p.Version, "Kesepakatan panel"); err == nil {
		t.Fatal("panelist locked NBK")
	}
	decision, err := s.LockFinalNBK(ctx, "pic", p.ID, "MAT", 1, 1, p.Version, "Kesepakatan panel")
	if err != nil || decision.SnapshotID == "" || decision.ActorID != "pic" {
		t.Fatalf("NBK: %+v %v", decision, err)
	}
	if _, err = s.LockFinalNBK(ctx, "pic", p.ID, "MAT", 1, 1, p.Version, "Duplikat"); err == nil {
		t.Fatal("NBK changed after lock")
	}
	if _, err = s.SaveEvaluation(ctx, "p1", eval); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitPanelistRound(ctx, "p1", 1, 1, false); err == nil {
		t.Fatal("reopened finalized round")
	}
	fresh, _ := st.GetProject(ctx, p.ID)
	fresh.Subjects[0].FinalNBK = nil
	if saved, err := s.SaveProject(ctx, fresh, "admin"); err != nil || saved.Subjects[0].FinalNBK == nil {
		t.Fatalf("configuration removed final lock: %+v %v", saved, err)
	}
	if _, err = s.CompleteProject(ctx, p.ID, "SELESAIKAN PROYEK"); err != nil {
		t.Fatal(err)
	}
}

func TestPracticeIsSeparateAndPICControlsActualScoring(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.WorkflowVersion = 1
	p.PlannedRounds = 2
	p.SimulationEnabled = true
	p.Subjects[0].PracticeStatus = "open"
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	a := domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(65)}
	if _, err := s.SaveSubmission(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", a); err == nil {
		t.Fatal("scored while practice open")
	}
	practice, err := s.StartPractice(ctx, "p1")
	if err != nil || practice.Session.StartedAt.IsZero() || len(practice.Items) != 4 {
		t.Fatalf("practice start: %+v %v", practice, err)
	}
	if _, err = s.SavePracticeAnswer(ctx, "p1", "practice-pg", a, practice.Session.Version); err != nil {
		t.Fatal(err)
	}
	practice, _ = s.GetPractice(ctx, "p1")
	for _, item := range practice.Items {
		if item.QuestionType != domain.QuestionTypeComplexBS && item.QuestionType != domain.QuestionTypeComplexMCMA {
			continue
		}
		practice, err = s.SavePracticeAnswer(ctx, "p1", item.ID, domain.Answer{QuestionType: item.QuestionType, Distribution: []int{10, 20, 30, 40}}, practice.Session.Version)
		if err != nil {
			t.Fatal("split complex example could not be saved", item.ID, err)
		}
	}
	if _, err = s.SavePracticeAnswer(ctx, "p1", "practice-complex", domain.Answer{QuestionType: domain.QuestionTypeComplex, Distribution: []int{10, 20, 30, 40}}, practice.Session.Version); err == nil {
		t.Fatal("removed legacy complex example still accepts writes")
	}
	rows, _ := st.ListSubmissions(ctx, p.ID, "MAT", 0, "")
	if len(rows) != 0 {
		t.Fatal("practice polluted real submissions")
	}
	practice, _ = s.GetPractice(ctx, "p1")
	if _, err = s.FinishPractice(ctx, "p1", practice.Session.Version); err != nil {
		t.Fatal(err)
	}
	if err = s.EndSubjectPractice(ctx, "p1", p.ID, "MAT", p.Version); err == nil {
		t.Fatal("panelist started scoring")
	}
	if err = s.EndSubjectPractice(ctx, "pic", p.ID, "MAT", p.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveSubmission(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", a); err != nil {
		t.Fatal(err)
	}
	practice, err = s.GetPractice(ctx, "p1")
	if err != nil || !practice.ReadOnly {
		t.Fatalf("practice reopened: %+v %v", practice, err)
	}
}

func TestPracticeIncludesSeparateBSAndMCMANotLegacyComplex(t *testing.T) {
	items := practiceItems()
	want := []domain.QuestionType{domain.QuestionTypePG, domain.QuestionTypeComplexBS, domain.QuestionTypeComplexMCMA, domain.QuestionTypeEssay}
	if len(items) != len(want) {
		t.Fatalf("want four examples, got %d", len(items))
	}
	ids := map[string]bool{}
	for i, item := range items {
		if item.QuestionType != want[i] || item.ID == "practice-complex" || ids[item.ID] {
			t.Fatal("wrong example types/identity", items)
		}
		ids[item.ID] = true
		if item.QuestionType == domain.QuestionTypeComplexBS || item.QuestionType == domain.QuestionTypeComplexMCMA {
			if item.MaxScore != 3 || validateItemAnswer(item, domain.Answer{QuestionType: item.QuestionType, Distribution: []int{10, 20, 30, 40}}) != nil {
				t.Fatal("complex example rules changed", item)
			}
		}
	}
	row := domain.PracticeSession{Answers: map[string]domain.Answer{"practice-complex": {QuestionType: domain.QuestionTypeComplex, Distribution: []int{10, 20, 30, 40}}}}
	view := practiceView(domain.Project{}, domain.ProjectSubject{}, row)
	if len(view.Session.Answers) != 0 || len(row.Answers) != 1 {
		t.Fatal("legacy answer reused or original session mutated")
	}
}

func TestWorkflowRejectsStaleReceiptsAndAllowsUntimedPractice(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.WorkflowVersion = 1
	p.PlannedRounds = 2
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitPanelistRound(ctx, "p1", 1, 1, true); err == nil {
		t.Fatal("incomplete submitted")
	}
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.SubmitPanelistRound(ctx, "p1", 1, 2, true); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale receipt %v", err)
	}
	for _, u := range []string{"p1", "p2"} {
		if _, err := s.SubmitPanelistRound(ctx, u, 1, 1, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "rollback", "ROLLBACK PUTARAN", "pic", 2, 1); err != nil {
		t.Fatal(err)
	}
	reopened, _ := st.CurrentRound(ctx, p.ID, "MAT")
	if len(reopened.Receipts) != 0 {
		t.Fatal("rollback reused previous completion receipts")
	}
	if _, err := s.SubmitPanelistRound(ctx, "p1", 1, 1, true); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("rollback accepted a stale completion request")
	}
	if _, err := s.SubmitPanelistRound(ctx, "p1", 1, 2, true); err != nil {
		t.Fatal("rollback required item acknowledgements before a fresh completion", err)
	}
	p, _ = st.GetProject(ctx, p.ID)
	p.Subjects[0].PracticeStatus = "open"
	p.SimulationEnabled = true
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	practice, err := s.StartPractice(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	practice.Session.StartedAt = time.Now().Add(-11 * time.Minute)
	if err := st.SavePracticeSession(ctx, practice.Session, practice.Session.Version); err != nil {
		t.Fatal(err)
	}
	practice, err = s.SavePracticeAnswer(ctx, "p1", "practice-pg", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(50)}, practice.Session.Version)
	if err != nil || practice.ReadOnly || practice.ExpiresAt != nil {
		t.Fatal("practice must remain writable after ten minutes", practice, err)
	}
	if _, err = s.SavePracticeAnswer(ctx, "p1", "practice-essay", domain.Answer{QuestionType: domain.QuestionTypeEssay, Distribution: []int{20, 30, 50}}, practice.Session.Version); err != nil {
		t.Fatal("essay practice must accept score 0 through 2", err)
	}
}

func TestPracticeHasNoDeadlineAndEssayMaximumTwo(t *testing.T) {
	now := time.Now().UTC()
	p := domain.Project{Status: domain.ProjectActive, SimulationEnabled: true, StartDate: now.Add(-time.Hour), EndDate: now.Add(time.Hour)}
	sub := domain.ProjectSubject{PracticeStatus: "open"}
	row := domain.PracticeSession{StartedAt: now.Add(-11 * time.Minute), Answers: map[string]domain.Answer{
		"practice-pg":    {QuestionType: domain.QuestionTypePG, Value: intValue(50)},
		"practice-essay": {QuestionType: domain.QuestionTypeEssay, Distribution: []int{0, 0, 100, 0, 0}},
	}}
	view := practiceView(p, sub, row)
	if view.ReadOnly || view.ExpiresAt != nil {
		t.Fatal("elapsed practice still expires", view)
	}
	for _, item := range view.Items {
		if item.QuestionType == domain.QuestionTypeEssay {
			if item.MaxScore != 2 || validateItemAnswer(item, domain.Answer{QuestionType: domain.QuestionTypeEssay, Distribution: []int{20, 30, 50}}) != nil {
				t.Fatal("essay example must have three score bins", item)
			}
		}
	}
	if _, exists := view.Session.Answers["practice-essay"]; exists {
		t.Fatal("old five-bin essay answer shown as valid")
	}
	if _, exists := view.Session.Answers["practice-pg"]; !exists || len(row.Answers) != 2 {
		t.Fatal("valid answer lost or original session mutated")
	}
	finished := now
	for _, kind := range []string{"finished", "closed", "period", "completed"} {
		project, subject, session := p, sub, row
		switch kind {
		case "finished":
			session.FinishedAt = &finished
		case "closed":
			subject.PracticeStatus = "closed"
		case "period":
			project.EndDate = now.Add(-time.Minute)
		case "completed":
			project.Status = domain.ProjectCompleted
		}
		if !practiceView(project, subject, session).ReadOnly {
			t.Fatal("practice gate removed", kind)
		}
	}
}

func TestLastPlannedRoundCanReopenBeforeNBKAndCopiedSettingsPersist(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.WorkflowVersion = 1
	p.PlannedRounds = 1
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	fillRound(t, ctx, s, p, 1, 0)
	for _, id := range []string{"p1", "p2"} {
		if _, err := s.SubmitPanelistRound(ctx, id, 1, 1, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	m, err := s.Monitoring(ctx, p.ID, "MAT")
	if err == nil && (len(m.FinalNBKChoices) != 1 || m.FinalNBKChoices[0].RoundNumber != 1 || m.FinalNBKChoices[0].Statistics.PanelistCount != 2) {
		t.Fatalf("NBK source preview missing: %+v", m.FinalNBKChoices)
	}
	if err != nil || !m.CanRollback {
		t.Fatalf("last final cannot reopen: %v %+v", err, m)
	}
	reopened, err := s.RoundAction(ctx, p.ID, "MAT", "rollback", "ROLLBACK PUTARAN", "pic", 1, 1)
	if err != nil || reopened.Number != 1 || reopened.Revision != 2 {
		t.Fatalf("reopen last final: %+v %v", reopened, err)
	}
	if len(reopened.Receipts) != 0 {
		t.Fatal("reopening reused previous completion receipts")
	}
	if _, err = s.SubmitPanelistRound(ctx, "p1", 1, 2, true); err != nil {
		t.Fatal("reopened final round required item acknowledgements", err)
	}
	p, _ = st.GetProject(ctx, p.ID)
	p.PlannedRounds = 5
	p.SimulationEnabled = true
	if err = st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	copied, err := s.CopyProject(ctx, p.ID, "QA copy settings", "admin")
	if err != nil || copied.PlannedRounds != 5 || !copied.SimulationEnabled || copied.Subjects[0].FinalNBK != nil || copied.Subjects[0].PracticeStatus != "" {
		t.Fatalf("copied workflow settings %+v %v", copied, err)
	}
}
