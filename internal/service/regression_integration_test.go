package service

import (
	"context"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/store"
)

func fixture(t *testing.T, active bool) (context.Context, *Service, *store.Mongo, domain.Project) {
	t.Helper()
	uri := os.Getenv("QA_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set QA_TEST_MONGO_URI to the isolated QA replica set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	name := fmt.Sprintf("standard-setting-regression-%d", time.Now().UnixNano())
	st, err := store.NewMongo(ctx, uri, name)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		client, err := mongo.Connect(cleanup, options.Client().ApplyURI(uri))
		if err == nil {
			if err := client.Database(name).Drop(cleanup); err != nil {
				t.Error(err)
			}
			_ = client.Disconnect(cleanup)
		}
		_ = st.Close(cleanup)
		cancel()
	})
	now := time.Now().UTC()
	p := domain.Project{ID: "project-qa", Code: "SS-QA", Name: "QA Project", Description: "original", StartDate: now,
		EndDate: now.Add(time.Hour), Status: domain.ProjectDraft, WizardStep: 5, AccountsGenerated: true, CreatedBy: "admin", CreatedAt: now,
		Subjects: []domain.ProjectSubject{{Code: "MAT", Name: "Matematika", ItemIDs: []string{"MAT-001", "MAT-002", "MAT-003"},
			PICUserID: "pic", PICName: "PIC QA", PanelistCount: 2, PanelistUserIDs: []string{"p1", "p2"}, PanelistNames: []string{"A", "B"}}}}
	if active {
		p.Status = domain.ProjectActive
	}
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	users := []domain.User{
		{ID: "pic", Username: "ss-qa.mat.pic01", Name: "PIC QA", Role: domain.RolePICMapel, ProjectID: p.ID, SubjectCode: "MAT", Active: true},
		{ID: "p1", Username: "ss-qa.mat.panelis01", Name: "A", Role: domain.RolePanelist, ProjectID: p.ID, SubjectCode: "MAT", Active: true},
		{ID: "p2", Username: "ss-qa.mat.panelis02", Name: "B", Role: domain.RolePanelist, ProjectID: p.ID, SubjectCode: "MAT", Active: true},
	}
	if err := st.InsertUsers(ctx, users); err != nil {
		t.Fatal(err)
	}
	items := []domain.Item{{ID: "MAT-001", QuestionType: domain.QuestionTypePG, MaxScore: 1},
		{ID: "MAT-002", QuestionType: domain.QuestionTypeComplex, MaxScore: 3}, {ID: "MAT-003", QuestionType: domain.QuestionTypeEssay, MaxScore: 4}}
	for i := range items {
		items[i].SubjectCode = "MAT"
		items[i].SubjectName = "Matematika"
		items[i].UsageStatus = domain.ItemAvailable
		if active {
			items[i].ProjectID = p.ID
			items[i].UsageStatus = domain.ItemAllocated
		}
	}
	if err := st.UpsertItems(ctx, items); err != nil {
		t.Fatal(err)
	}
	if active {
		if err := st.SaveRound(ctx, domain.Round{ID: "round-1", ProjectID: p.ID, SubjectCode: "MAT", Number: 1, Status: "active", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	return ctx, New(st, nil, "12345*"), st, p
}

func intValue(v int) *int { return &v }
func fillRound(t *testing.T, ctx context.Context, s *Service, p domain.Project, round, delta int) {
	t.Helper()
	for i, id := range []string{"p1", "p2"} {
		answers := []domain.Answer{{QuestionType: domain.QuestionTypePG, Value: intValue(60 + 20*i + delta)},
			{QuestionType: domain.QuestionTypeComplex, Distribution: []int{0, 0, 0, 100}},
			{QuestionType: domain.QuestionTypeEssay, Distribution: []int{0, 0, 100, 0, 0}}}
		for j, answer := range answers {
			if _, err := s.SaveSubmission(ctx, id, id, p.ID, "MAT", round, p.Subjects[0].ItemIDs[j], answer); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestSubmissionResaveKeepsOneStableID(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	first, err := s.SaveSubmission(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(60)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.SaveSubmission(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(75)})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := st.ListSubmissions(ctx, p.ID, "MAT", 1, "p1")
	if err != nil || len(rows) != 1 || first.ID != second.ID || *rows[0].Answer.Value != 75 {
		t.Fatalf("resave contract failed: rows=%v err=%v", rows, err)
	}
}

func TestMonitoringUsesCompleteRealResponsesAndHistory(t *testing.T) {
	ctx, s, _, p := fixture(t, true)
	initial, err := s.Monitoring(ctx, p.ID, "MAT")
	if err != nil || initial.Statistics != nil || initial.TotalDone != 0 || initial.Panelists[0].LastActive != nil || initial.CanFinalize {
		t.Fatalf("initial monitoring misleading: %+v %v", initial, err)
	}
	fillRound(t, ctx, s, p, 1, 0)
	full, err := s.Monitoring(ctx, p.ID, "MAT")
	if err != nil || full.Statistics == nil || full.Statistics.Mean != 71.25 || math.Abs(full.Statistics.StandardDev-1.25*math.Sqrt(2)) > 1e-10 || full.TotalDone != 6 || !full.CanFinalize {
		t.Fatalf("wrong complete statistics: %+v %v", full, err)
	}
	if _, err := s.PreviewPanelist(ctx, p.ID, "MAT", 1, "pic"); err == nil {
		t.Fatal("PIC accepted as panelist preview")
	}
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 99); err == nil {
		t.Fatal("stale round action accepted")
	}
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1); err != nil {
		t.Fatal(err)
	}
	form, err := s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if err != nil || form.Round.Number != 2 || len(form.Submissions) != 0 || len(form.PreviousSubmissions) != 3 || form.PreviousRound == nil || *form.PreviousRound != 1 {
		t.Fatalf("wrong new form/history: %+v %v", form, err)
	}
}

func TestChangingSubjectsInvalidatesGeneratedAccountChecklist(t *testing.T) {
	ctx, s, _, p := fixture(t, false)
	p.Subjects = []domain.ProjectSubject{{Code: "BIN", Name: "Bahasa Indonesia", PanelistCount: 2, PICName: "New PIC", PanelistNames: []string{"C", "D"}}}
	updated, err := s.SaveProject(ctx, p, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if updated.AccountsGenerated {
		t.Fatal("replaced subject retained account readiness")
	}
}

func TestDeleteDraftReleasesItemsAndDeactivatesAccounts(t *testing.T) {
	ctx, s, st, p := fixture(t, false)
	if _, err := s.SaveProject(ctx, p, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProject(ctx, p.ID, "HAPUS PROYEK"); err != nil {
		t.Fatal(err)
	}
	item, err := st.GetItem(ctx, "MAT-001")
	if err != nil || item.ProjectID != "" || item.UsageStatus != domain.ItemAvailable {
		t.Fatalf("item not released: %+v %v", item, err)
	}
	for _, id := range []string{"pic", "p1", "p2"} {
		user, err := st.FindByID(ctx, id)
		if err != nil || user.Active {
			t.Fatalf("draft account remains active: %+v %v", user, err)
		}
	}
}

func TestSaveSubmissionValidatesAssignmentTypeAndDistributionLength(t *testing.T) {
	for _, tc := range []struct {
		name, user, item string
		answer           domain.Answer
	}{
		{"wrong type", "p1", "MAT-001", domain.Answer{QuestionType: domain.QuestionTypeEssay, Distribution: []int{100, 0}}},
		{"wrong length", "p1", "MAT-002", domain.Answer{QuestionType: domain.QuestionTypeComplex, Distribution: []int{50, 50}}},
		{"unassigned user", "outsider", "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(60)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, s, _, p := fixture(t, true)
			if _, err := s.SaveSubmission(ctx, tc.user, "QA", p.ID, "MAT", 1, tc.item, tc.answer); err == nil {
				t.Fatal("invalid submission accepted")
			}
		})
	}
}

func TestDraftDoesNotCreateActiveRoundWhenFormRead(t *testing.T) {
	ctx, s, st, p := fixture(t, false)
	if _, err := s.PanelistForm(ctx, "p1", p.ID, "MAT"); err == nil {
		t.Fatal("draft opened for grading")
	}
	rounds, err := st.ListRounds(ctx, p.ID, "MAT")
	if err != nil || len(rounds) != 0 {
		t.Fatalf("reading draft created a round: %v %v", rounds, err)
	}
}

func TestFailedAllocationPreservesItemsAndProject(t *testing.T) {
	ctx, s, st, p := fixture(t, false)
	p.Subjects[0].ItemIDs = []string{"MAT-001", "UNKNOWN"}
	p.Description = "should not persist"
	if _, err := s.SaveProject(ctx, p, "attacker"); err == nil {
		t.Fatal("unknown item accepted")
	}
	item, err := st.GetItem(ctx, "MAT-001")
	if err != nil || item.UsageStatus != domain.ItemAvailable || item.ProjectID != "" {
		t.Fatalf("partial allocation leaked: %+v %v", item, err)
	}
	stored, err := st.GetProject(ctx, p.ID)
	if err != nil || stored.Description != "original" {
		t.Fatalf("failed update persisted: %+v %v", stored, err)
	}
}

func TestProjectUpdatePreservesServerFieldsAndLocksActiveConfiguration(t *testing.T) {
	ctx, s, st, p := fixture(t, false)
	p.Code = "FORGED"
	p.CreatedBy = "attacker"
	p.Status = domain.ProjectCompleted
	p.AccountsGenerated = false
	forgedCompletion := time.Now().Add(time.Hour)
	p.CompletedAt = &forgedCompletion
	p.Subjects[0].PICUserID = "forged"
	p.Subjects[0].PanelistUserIDs = []string{"forged"}
	updated, err := s.SaveProject(ctx, p, "attacker")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Code != "SS-QA" || updated.CreatedBy != "admin" || updated.Status != domain.ProjectDraft || updated.CompletedAt != nil || !updated.AccountsGenerated || updated.Subjects[0].PICUserID != "pic" {
		t.Fatalf("client overwrote server fields: %+v", updated)
	}
	updated.Status = domain.ProjectActive
	if err := st.SaveProject(ctx, updated); err != nil {
		t.Fatal(err)
	}
	updated.Name = "changed while active"
	if _, err := s.SaveProject(ctx, updated, "admin"); err != nil {
		t.Fatalf("active information edit rejected: %v", err)
	}
}

func TestFinalizationRequiresEveryAssignedPanelistAndItem(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic"); err == nil {
		t.Fatal("incomplete round finalized")
	}
	r, err := st.CurrentRound(ctx, p.ID, "MAT")
	if err != nil || r.Number != 1 {
		t.Fatalf("rejected finalization changed round: %+v %v", r, err)
	}
}

func TestCompletionRequiresFinalizedResponses(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	if _, err := s.CompleteProject(ctx, p.ID, "SELESAIKAN PROYEK"); err == nil {
		t.Fatal("empty project completed")
	}
	item, _ := st.GetItem(ctx, "MAT-001")
	if item.ReadOnly {
		t.Fatal("rejected completion marked item tested")
	}
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.CompleteProject(ctx, p.ID, "SELESAIKAN PROYEK"); err == nil {
		t.Fatal("unfinalized project completed")
	}
}

func TestCompletionSnapshotsOnlyLastFinalizedRound(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic"); err != nil {
		t.Fatal(err)
	}
	fillRound(t, ctx, s, p, 2, 10)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteProject(ctx, p.ID, "SELESAIKAN PROYEK"); err != nil {
		t.Fatal(err)
	}
	item, err := st.GetItem(ctx, "MAT-001")
	if err != nil || item.FinalResult == nil {
		t.Fatalf("missing final snapshot: %v", err)
	}
	detail, err := s.ItemResult(ctx, "MAT-001")
	if err != nil || detail.Statistics.Mean != 80 || detail.Statistics.PanelistCount != 2 || len(detail.Responses) != 2 {
		t.Fatalf("statistics mixed rounds: %+v %v", detail, err)
	}
	if !item.ReadOnly || item.UsageStatus != domain.ItemTested {
		t.Fatal("completed item is editable")
	}
}

func TestFinalizeRollbackThenFinalizeReusesRoundWithoutDuplicate(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "rollback", "ROLLBACK PUTARAN", "pic", 2, 1); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"p1", "p2"} {
		rows, err := st.ListSubmissions(ctx, p.ID, "MAT", 1, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if _, err = s.ReviewSubmission(ctx, id, p.ID, "MAT", 1, r.ItemID, 2, r.Version); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 2); err != nil {
		t.Fatal(err)
	}
	rounds, err := st.ListRounds(ctx, p.ID, "MAT")
	if err != nil || len(rounds) != 2 || len(rounds[0].Snapshots) != 2 {
		t.Fatalf("duplicate round or missing revisions: %+v %v", rounds, err)
	}
}

func TestGeneratingSameProjectAccountsDoesNotDuplicateOrReset(t *testing.T) {
	ctx, s, st, p := fixture(t, false)
	inputs := []AccountInput{{Name: "PIC QA", Role: domain.RolePICMapel, SubjectCode: "MAT"}, {Name: "A", Role: domain.RolePanelist, SubjectCode: "MAT"}, {Name: "B", Role: domain.RolePanelist, SubjectCode: "MAT"}}
	users, err := s.GenerateAccounts(ctx, p.ID, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 3 {
		t.Fatalf("want 3 accounts, got %d", len(users))
	}
	stored, err := st.ListUsers(ctx, p.ID)
	if err != nil || len(stored) != 3 {
		t.Fatalf("duplicate accounts: %d %v", len(stored), err)
	}
	for _, u := range stored {
		if u.ID != "pic" && u.ID != "p1" && u.ID != "p2" {
			t.Fatalf("regeneration replaced account ID %s", u.ID)
		}
	}
}
