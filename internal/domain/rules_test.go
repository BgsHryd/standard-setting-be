package domain

import (
	"strings"
	"testing"
)

func TestValidateAnswerAcceptsSinglePGScore(t *testing.T) {
	answer := Answer{QuestionType: QuestionTypePG, Value: intPointer(72)}

	if err := ValidateAnswer(answer); err != nil {
		t.Fatalf("expected valid PG score, got %v", err)
	}
}

func TestValidateAnswerRejectsPGScoreOutsideRange(t *testing.T) {
	answer := Answer{QuestionType: QuestionTypePG, Value: intPointer(101)}

	err := ValidateAnswer(answer)
	if err == nil || !strings.Contains(err.Error(), "0 sampai 100") {
		t.Fatalf("expected range validation error, got %v", err)
	}
}

func TestValidateAnswerRequiresDistributionTotalOfOneHundred(t *testing.T) {
	answer := Answer{QuestionType: QuestionTypeEssay, Distribution: []int{5, 12, 26, 38, 18}}

	err := ValidateAnswer(answer)
	if err == nil || !strings.Contains(err.Error(), "tepat 100") {
		t.Fatalf("expected total validation error, got %v", err)
	}
}

func TestValidateActivationReportsIncompleteWizardSections(t *testing.T) {
	project := Project{
		Name:     "Standard Setting 2026",
		Subjects: []ProjectSubject{{Code: "MAT", Name: "Matematika", ItemIDs: []string{"MAT-001"}}},
	}

	errs := ValidateActivation(project)
	joined := strings.Join(errs, "|")
	for _, expected := range []string{"periode proyek", "PIC Matematika", "panelis Matematika", "akun proyek"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected %q in validation errors: %v", expected, errs)
		}
	}
}

func TestAllocateItemsAllowsReuseWithoutTransferringExistingProject(t *testing.T) {
	items := []Item{{ID: "MAT-001", ProjectID: "project-other", ProjectName: "Other", UsageStatus: ItemAllocated}}
	updated, err := AllocateItems("project-current", items)
	if err != nil {
		t.Fatal(err)
	}
	if updated[0].ProjectID != "project-other" || updated[0].UsageStatus != ItemAllocated {
		t.Fatalf("existing allocation transferred: %+v", updated[0])
	}
	if len(updated[0].ProjectHistory) != 2 {
		t.Fatalf("missing shared usage history: %+v", updated[0])
	}
	if len(items[0].ProjectHistory) != 0 {
		t.Fatal("input mutated")
	}
	if _, err = AllocateItems("third", []Item{{ID: "tested", UsageStatus: ItemTested}}); err == nil {
		t.Fatal("tested item reused")
	}
}

func TestCompleteProjectMarksAllocatedItemsTested(t *testing.T) {
	project := Project{ID: "project-1", Status: ProjectActive}
	items := []Item{
		{ID: "MAT-001", ProjectID: "project-1", UsageStatus: ItemAllocated},
		{ID: "BIN-001", UsageStatus: ItemAvailable},
	}

	completed, updated, err := CompleteProject(project, items)
	if err != nil {
		t.Fatalf("unexpected completion error: %v", err)
	}
	if completed.Status != ProjectCompleted {
		t.Fatalf("expected completed project, got %s", completed.Status)
	}
	if updated[0].UsageStatus != ItemTested || !updated[0].ReadOnly {
		t.Fatalf("expected owned item tested and read-only, got %+v", updated[0])
	}
	if updated[1].UsageStatus != ItemAvailable {
		t.Fatalf("expected unrelated item untouched, got %+v", updated[1])
	}
}

func intPointer(value int) *int { return &value }

func TestCompleteProjectUsesSubjectMembershipForSharedItems(t *testing.T) {
	p := Project{ID: "second", Status: ProjectActive, Subjects: []ProjectSubject{{ItemIDs: []string{"shared"}}}}
	_, updated, err := CompleteProject(p, []Item{{ID: "shared", ProjectID: "first", UsageStatus: ItemAllocated}})
	if err != nil || updated[0].UsageStatus != ItemTested {
		t.Fatal("shared item completion ignored", updated, err)
	}
}
