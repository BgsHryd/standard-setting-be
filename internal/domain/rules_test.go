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

func TestAllocateItemsRejectsItemOwnedByAnotherProject(t *testing.T) {
	items := []Item{{ID: "MAT-001", ProjectID: "project-other", UsageStatus: ItemAllocated}}

	_, err := AllocateItems("project-current", items)
	if err == nil || !strings.Contains(err.Error(), "proyek lain") {
		t.Fatalf("expected exclusive allocation error, got %v", err)
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
