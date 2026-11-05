package service

import (
	"context"
	"standard-setting/backend/internal/domain"
	"testing"
)

func TestQuestionExamplesDefaultPersistCompatibilityAndCAS(t *testing.T) {
	m := &instructionMemory{actor: domain.User{ID: "admin", Role: domain.RoleAdminSuper, Active: true}}
	s := New(m, nil, "")
	ctx := context.Background()
	initial, err := s.QuestionInstructions(ctx)
	if err != nil || len(initial.ShowExamples) != 2 || initial.ShowExamples[domain.QuestionTypeComplexMCMA] {
		t.Fatal("legacy defaults", initial, err)
	}
	flags := map[domain.QuestionType]bool{domain.QuestionTypeComplexMCMA: true, domain.QuestionTypeComplexBS: false}
	saved, err := s.SaveQuestionInstructions(ctx, "admin", 0, initial.Instructions, flags)
	if err != nil || !saved.ShowExamples[domain.QuestionTypeComplexMCMA] {
		t.Fatal("flag not persisted", saved, err)
	}
	reloaded, err := s.QuestionInstructions(ctx)
	if err != nil || !reloaded.ShowExamples[domain.QuestionTypeComplexMCMA] {
		t.Fatal("flag not reloaded", reloaded, err)
	}
	// An old client that only edits HTML must not erase the new setting.
	saved, err = s.SaveQuestionInstructions(ctx, "admin", 1, reloaded.Instructions)
	if err != nil || !saved.ShowExamples[domain.QuestionTypeComplexMCMA] {
		t.Fatal("old client erased flag", saved, err)
	}
	flags[domain.QuestionTypeComplexMCMA] = false
	saved, err = s.SaveQuestionInstructions(ctx, "admin", 2, reloaded.Instructions, flags)
	if err != nil || saved.ShowExamples[domain.QuestionTypeComplexMCMA] {
		t.Fatal("cannot disable", saved, err)
	}
	if _, err = s.SaveQuestionInstructions(ctx, "admin", 2, reloaded.Instructions, flags); err != domain.ErrInstructionConflict {
		t.Fatal("stale config accepted", err)
	}
}

func TestQuestionExamplesRejectUnsupportedTypes(t *testing.T) {
	m := &instructionMemory{actor: domain.User{Role: domain.RoleAdminSuper, Active: true}}
	s := New(m, nil, "")
	for _, kind := range []domain.QuestionType{domain.QuestionTypePG, domain.QuestionTypeComplex, domain.QuestionTypeEssay, "unknown"} {
		if _, err := s.SaveQuestionInstructions(context.Background(), "admin", 0, domain.DefaultQuestionInstructions(), map[domain.QuestionType]bool{kind: true}); err == nil || m.config.Version != 0 {
			t.Fatal("unsupported flag persisted", kind, err)
		}
	}
}
