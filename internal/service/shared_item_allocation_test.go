package service

import (
	"context"
	"testing"
	"time"

	"standard-setting/backend/internal/domain"
)

func createSharedProject(t *testing.T, ctx context.Context, s *Service, name string) domain.Project {
	t.Helper()
	p, err := s.SaveProject(ctx, domain.Project{
		Name:          name,
		PlannedRounds: 1,
		StartDate:     time.Now().Add(-time.Hour),
		EndDate:       time.Now().Add(time.Hour),
		Subjects: []domain.ProjectSubject{{
			Code:                "MAT",
			ItemIDs:             []string{"MAT-001"},
			CandidateDefinition: "Menguasai konsep dasar.",
			PICName:             "PIC baru",
			PanelistCount:       1,
			PanelistNames:       []string{"Panelis baru"},
		}},
	}, "admin")
	if err != nil {
		t.Fatal("create shared project:", err)
	}
	return p
}

func TestSharedItemProjectLifecycle(t *testing.T) {
	ctx, s, st, a := fixture(t, true)
	b := createSharedProject(t, ctx, s, "Proyek kedua")
	if a.ID == b.ID {
		t.Fatal("project identity reused")
	}
	for _, id := range []string{a.ID, b.ID} {
		items, err := st.ListItems(ctx, "MAT", id, false)
		if err != nil || len(items) == 0 || items[0].ID != "MAT-001" {
			t.Fatal("shared item missing in project", id, items, err)
		}
	}
	rows, err := s.AccountRows(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	inputs := []AccountInput{}
	for _, r := range rows {
		inputs = append(inputs, AccountInput{SlotID: r.SlotID, Name: r.Name, Role: r.Role, SubjectCode: r.SubjectCode})
	}
	if _, err = s.GenerateAccounts(ctx, b.ID, inputs); err != nil {
		t.Fatal(err)
	}
	b, validation, err := s.ActivateProject(ctx, b.ID)
	if err != nil || len(validation) > 0 {
		t.Fatal("activation failed", validation, err)
	}
	userID := b.Subjects[0].PanelistUserIDs[0]
	form, err := s.PanelistForm(ctx, userID, b.ID, "MAT")
	if err != nil || len(form.Items) != 1 {
		t.Fatal("shared item missing from form", err)
	}
	if _, err = s.SaveSubmissionWithOptions(ctx, userID, "Panelis baru", b.ID, "MAT", 1, "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(35)}, SubmissionOptions{CandidateVersion: b.Subjects[0].CandidateVersion, RoundRevision: 1}); err != nil {
		t.Fatal("shared score rejected", err)
	}
	fillRound(t, ctx, s, a, 1, 0)
	if _, err = s.RoundAction(ctx, a.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1); err != nil {
		t.Fatal("original project finalization rejected", err)
	}
	if _, err = s.CompleteProject(ctx, a.ID, "SELESAIKAN PROYEK"); err != nil {
		t.Fatal(err)
	}
	item, err := st.GetItem(ctx, "MAT-001")
	if err != nil || item.UsageStatus != domain.ItemTested {
		t.Fatal(item, err)
	}
	if _, err = s.SaveSubmissionWithOptions(ctx, userID, "Panelis baru", b.ID, "MAT", 1, "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(45)}, SubmissionOptions{Version: 1, CandidateVersion: b.Subjects[0].CandidateVersion, RoundRevision: 1}); err != nil {
		t.Fatal("completion of A blocked B", err)
	}
	b, err = s.SaveProject(ctx, b, "admin")
	if err != nil {
		t.Fatal("retaining shared tested item failed", err)
	}
	if _, err = s.SubmitPanelistRound(ctx, userID, 1, 1, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RoundAction(ctx, b.ID, "MAT", "finalize", "FINALISASI PUTARAN", b.Subjects[0].PICUserID, 1, 1); err != nil {
		t.Fatal("B finalization rejected", err)
	}
	got, err := st.ListSubmissions(ctx, b.ID, "MAT", 1, "")
	if err != nil || len(got) != 1 || *got[0].Answer.Value != 45 {
		t.Fatal("B results mixed with A", got, err)
	}
	aRows, err := st.ListSubmissions(ctx, a.ID, "MAT", 1, "")
	if err != nil || len(aRows) != 6 {
		t.Fatal("A results changed", aRows, err)
	}

	// Completing the second project must keep its bank-result summary and responses together.
	b, err = st.GetProject(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.LockFinalNBK(ctx, b.Subjects[0].PICUserID, b.ID, "MAT", 1, 1, b.Version, "Kesepakatan panel"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CompleteProject(ctx, b.ID, "SELESAIKAN PROYEK"); err != nil {
		t.Fatal(err)
	}
	detail, err := s.ItemResult(ctx, "MAT-001")
	if err != nil || detail.Statistics.Mean != 45 || len(detail.Responses) != 1 || detail.Responses[0].ProjectID != b.ID {
		t.Fatal("bank result uses statistics and responses from different projects", detail, err)
	}
	if _, err = s.SaveProject(ctx, domain.Project{Name: "Third", Subjects: []domain.ProjectSubject{{Code: "MAT", ItemIDs: []string{"MAT-001"}}}}, "admin"); err == nil {
		t.Fatal("tested item assigned to third project")
	}
}

func TestSharedItemRemovalAndDeletionPreserveOtherProject(t *testing.T) {
	ctx, s, st, a := fixture(t, true)
	b := createSharedProject(t, ctx, s, "Draft kedua")
	b.Subjects[0].ItemIDs = []string{}
	var err error
	b, err = s.SaveProject(ctx, b, "admin")
	if err != nil {
		t.Fatal(err)
	}
	item, _ := st.GetItem(ctx, "MAT-001")
	if item.UsageStatus != domain.ItemAllocated || item.ProjectID != a.ID {
		t.Fatal("removal released other allocation", item)
	}
	b.Subjects[0].ItemIDs = []string{"MAT-001"}
	b, err = s.SaveProject(ctx, b, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteProject(ctx, b.ID, "HAPUS PROYEK"); err != nil {
		t.Fatal(err)
	}
	item, _ = st.GetItem(ctx, "MAT-001")
	if item.UsageStatus != domain.ItemAllocated || item.ProjectID != a.ID || len(item.ProjectHistory) < 2 {
		t.Fatal("deletion lost other allocation/history", item)
	}
	if _, err = s.SaveSubmission(ctx, "p1", "A", a.ID, "MAT", 1, "MAT-001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(70)}); err != nil {
		t.Fatal("original project unusable after deletion", err)
	}
}

func TestSharedItemPrimaryRemovalReassignsSummaryAndLastDeletionReleases(t *testing.T) {
	ctx, s, st, a := fixture(t, false)
	a, err := s.SaveProject(ctx, a, "admin")
	if err != nil {
		t.Fatal(err)
	}
	b := createSharedProject(t, ctx, s, "Draft lain")
	a.Subjects[0].ItemIDs = []string{}
	if _, err = s.SaveProject(ctx, a, "admin"); err != nil {
		t.Fatal(err)
	}
	item, _ := st.GetItem(ctx, "MAT-001")
	if item.ProjectID != b.ID || item.UsageStatus != domain.ItemAllocated {
		t.Fatal("primary removal lost remaining membership", item)
	}
	if err = s.DeleteProject(ctx, b.ID, "HAPUS PROYEK"); err != nil {
		t.Fatal(err)
	}
	item, _ = st.GetItem(ctx, "MAT-001")
	if item.ProjectID != "" || item.UsageStatus != domain.ItemAvailable || len(item.ProjectHistory) != 2 {
		t.Fatal("last deletion did not release item and preserve history", item)
	}
}

func TestSharedItemConcurrentNewProjectsKeepAllMemberships(t *testing.T) {
	ctx, s, st, a := fixture(t, true)
	ready := make(chan struct{})
	type result struct {
		p   domain.Project
		err error
	}
	results := make(chan result, 2)
	for _, name := range []string{"Concurrent A", "Concurrent B"} {
		go func(name string) {
			<-ready
			p, err := s.SaveProject(ctx, domain.Project{Name: name, Subjects: []domain.ProjectSubject{{Code: "MAT", ItemIDs: []string{"MAT-001"}}}}, "admin")
			results <- result{p, err}
		}(name)
	}
	close(ready)
	for i := 0; i < 2; i++ {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		items, err := st.ListItems(ctx, "MAT", got.p.ID, false)
		if err != nil || len(items) != 1 {
			t.Fatal("concurrent project lost membership", items, err)
		}
	}
	item, err := st.GetItem(ctx, "MAT-001")
	if err != nil || item.ProjectID != a.ID || len(item.ProjectHistory) != 3 {
		t.Fatal("concurrent allocation lost history/original project", item, err)
	}
	projects, err := st.ListProjects(ctx)
	if err != nil || len(projects) != 3 {
		t.Fatal("transaction retry leaked draft", projects, err)
	}
}
