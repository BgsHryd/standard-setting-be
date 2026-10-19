package domain

import (
	"encoding/json"
	"testing"
)

func TestProjectPreservesPanelistNamesDuringJSONRoundTrip(t *testing.T) {
	input := []byte(`{"subjects":[{"code":"MAT","panelistCount":2,"panelistNames":["Rina","Budi"]}]}`)
	var project Project
	if err := json.Unmarshal(input, &project); err != nil {
		t.Fatalf("unexpected project decode error: %v", err)
	}
	output, err := json.Marshal(project)
	if err != nil {
		t.Fatalf("unexpected project encode error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("unexpected round-trip decode error: %v", err)
	}
	subjects := decoded["subjects"].([]any)
	subject := subjects[0].(map[string]any)
	names, ok := subject["panelistNames"].([]any)
	if !ok || len(names) != 2 || names[0] != "Rina" || names[1] != "Budi" {
		t.Fatalf("expected panelist names to survive round trip, got %v", subject["panelistNames"])
	}
}
