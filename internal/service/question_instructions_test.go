package service

import (
	"context"
	"errors"
	"testing"

	"standard-setting/backend/internal/domain"
)

func TestQuestionInstructionsSaveSanitizesPersistsAndRejectsStaleVersion(t *testing.T) {
	m := &instructionMemory{actor: domain.User{ID: "admin", Role: domain.RoleAdminSuper, Active: true}}
	s := New(m, nil, "")
	ctx := context.Background()
	first, err := s.QuestionInstructions(ctx)
	if err != nil || first.Version != 0 || len(first.Instructions) != 5 {
		t.Fatal(first, err)
	}
	first.Instructions[domain.QuestionTypePG] = `<h1 onclick="bad()">Petunjuk baru</h1><p><u>Isi</u></p>`
	saved, err := s.SaveQuestionInstructions(ctx, "admin", 0, first.Instructions)
	if err != nil || saved.Version != 1 || saved.Instructions[domain.QuestionTypePG] != `<h1>Petunjuk baru</h1><p><u>Isi</u></p>` {
		t.Fatal(saved, err)
	}
	reloaded, err := s.QuestionInstructions(ctx)
	if err != nil || reloaded.Version != 1 || reloaded.Instructions[domain.QuestionTypePG] != saved.Instructions[domain.QuestionTypePG] {
		t.Fatal(reloaded, err)
	}
	if _, err = s.SaveQuestionInstructions(ctx, "admin", 0, first.Instructions); !errors.Is(err, domain.ErrInstructionConflict) {
		t.Fatal("stale save accepted", err)
	}
}

func TestQuestionInstructionsEnforceAdminAndCompleteValidContent(t *testing.T) {
	for _, role := range []domain.Role{domain.RolePanelist, domain.RolePICMapel, domain.RoleAdminSuper} {
		m := &instructionMemory{actor: domain.User{Role: role, Active: role != domain.RoleAdminSuper}}
		if _, err := New(m, nil, "").SaveQuestionInstructions(context.Background(), "actor", 0, domain.DefaultQuestionInstructions()); err == nil {
			t.Fatal("unauthorized actor accepted", role)
		}
	}
	m := &instructionMemory{actor: domain.User{Role: domain.RoleAdminSuper, Active: true}}
	for _, kind := range []domain.QuestionType{domain.QuestionTypePG, "unknown"} {
		content := domain.DefaultQuestionInstructions()
		content[kind] = "<p> </p>"
		if _, err := New(m, nil, "").SaveQuestionInstructions(context.Background(), "admin", 0, content); err == nil || m.config.Version != 0 {
			t.Fatal("invalid content saved", err)
		}
	}
	m.readErr = errors.New("database unavailable")
	if _, err := New(m, nil, "").QuestionInstructions(context.Background()); err == nil {
		t.Fatal("DB outage hidden by defaults")
	}
}

func TestQuestionInstructionsAuditUsesReadableTarget(t *testing.T) {
	contexts, err := New(nil, nil, "").AuditContexts(context.Background(), []domain.AuditEvent{{ID: "a", Action: "question_instructions_update"}})
	if err != nil || contexts["a"].TargetLabel != "Config · Petunjuk jenis soal" {
		t.Fatal(contexts, err)
	}
}
