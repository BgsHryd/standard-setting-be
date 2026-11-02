package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
)

// Keep the shared instruction fixture in this tracked file so CI does not
// depend on optional local test files being present.
type instructionMemory struct {
	Store
	config  domain.QuestionInstructionConfig
	readErr error
	actor   domain.User
}

func (m *instructionMemory) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (m *instructionMemory) FindByID(context.Context, string) (domain.User, error) {
	return m.actor, nil
}
func (m *instructionMemory) GetQuestionInstructions(context.Context) (domain.QuestionInstructionConfig, error) {
	if m.readErr != nil {
		return m.config, m.readErr
	}
	if m.config.Version == 0 {
		return m.config, mongo.ErrNoDocuments
	}
	return m.config, nil
}
func (m *instructionMemory) SaveQuestionInstructions(_ context.Context, expected int, cfg domain.QuestionInstructionConfig) (domain.QuestionInstructionConfig, error) {
	if m.config.Version != expected {
		return cfg, domain.ErrInstructionConflict
	}
	cfg.Version = expected + 1
	m.config = cfg
	return cfg, nil
}

func TestAnswerInstructionsPersistAndLegacySavePreserves(t *testing.T) {
	ctx := context.Background()
	st := &instructionMemory{actor: domain.User{ID: "admin", Role: domain.RoleAdminSuper, Active: true}}
	s := New(st, nil, "")
	initial, err := s.QuestionInstructions(ctx)
	if err != nil || initial.AnswerInstructions[domain.QuestionTypePG].Title != "Nilai kelayakan kandidat" {
		t.Fatal(initial, err)
	}
	values := initial.AnswerInstructions
	values[domain.QuestionTypePG] = domain.AnswerInstruction{Title: "Judul PG baru", Description: "Bantuan PG baru"}
	saved, err := s.SaveQuestionInstructionSettings(ctx, "admin", 0, initial.Instructions, nil, nil, values)
	if err != nil {
		t.Fatal(err)
	}
	saved, err = s.SaveQuestionInstructions(ctx, "admin", saved.Version, saved.Instructions)
	if err != nil || saved.AnswerInstructions[domain.QuestionTypePG].Title != "Judul PG baru" {
		t.Fatal("legacy save reset text", saved, err)
	}
	loaded, err := s.QuestionInstructions(ctx)
	if err != nil || loaded.AnswerInstructions[domain.QuestionTypePG].Description != "Bantuan PG baru" {
		t.Fatal(loaded, err)
	}
	if _, err = s.SaveQuestionInstructionSettings(ctx, "admin", 0, initial.Instructions, nil, nil, values); !errors.Is(err, domain.ErrInstructionConflict) {
		t.Fatal("stale save", err)
	}
	for _, bad := range []domain.AnswerInstruction{{Title: " ", Description: "Help"}, {Title: "Title", Description: " "}, {Title: strings.Repeat("x", 201), Description: "Help"}, {Title: "Title", Description: strings.Repeat("x", 1001)}} {
		values[domain.QuestionTypePG] = bad
		if _, err = s.SaveQuestionInstructionSettings(ctx, "admin", loaded.Version, loaded.Instructions, nil, nil, values); err == nil {
			t.Fatal("invalid text accepted", bad)
		}
	}
}

func TestAnswerInstructionsReachPanelistForm(t *testing.T) {
	ctx, s, _, p := fixture(t, true)
	config := domain.QuestionInstructionConfig{Version: 1, AnswerInstructions: domain.DefaultAnswerInstructions()}
	config.AnswerInstructions[domain.QuestionTypeEssay] = domain.AnswerInstruction{Title: "Instruksi esai khusus", Description: "Gunakan rubrik terbaru"}
	s.store = &answerFormStore{Store: s.store, config: config}
	form, err := s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if err != nil || form.AnswerInstructions[domain.QuestionTypeEssay].Title != "Instruksi esai khusus" {
		t.Fatal(form, err)
	}
}

type answerFormStore struct {
	Store
	config domain.QuestionInstructionConfig
}

func (m *answerFormStore) GetQuestionInstructions(context.Context) (domain.QuestionInstructionConfig, error) {
	return m.config, nil
}
func (m *answerFormStore) SaveQuestionInstructions(context.Context, int, domain.QuestionInstructionConfig) (domain.QuestionInstructionConfig, error) {
	panic("not used")
}
