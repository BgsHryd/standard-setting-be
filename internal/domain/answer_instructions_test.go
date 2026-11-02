package domain

import (
	"go.mongodb.org/mongo-driver/bson"
	"testing"
)

func TestAnswerInstructionBSONAndOldConfig(t *testing.T) {
	cfg := QuestionInstructionConfig{AnswerInstructions: DefaultAnswerInstructions()}
	cfg.AnswerInstructions[QuestionTypePG] = AnswerInstruction{Title: "Judul khusus", Description: "Bantuan khusus"}
	raw, err := bson.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var read QuestionInstructionConfig
	if err = bson.Unmarshal(raw, &read); err != nil || read.AnswerInstructions[QuestionTypePG] != cfg.AnswerInstructions[QuestionTypePG] {
		t.Fatal(read, err)
	}
	for _, value := range []AnswerInstruction{{Title: "\u200b", Description: "Help"}, {Title: "Title", Description: "\u200b"}} {
		if ValidAnswerInstruction(value) {
			t.Fatal("invisible text accepted")
		}
	}
}
