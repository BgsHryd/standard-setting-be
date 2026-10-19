package store

import "testing"

func TestSubmissionFilterOmitsRoundForFinalItemResult(t *testing.T) {
	filter := submissionFilter("project-1", "MAT", 0, "")
	if _, exists := filter["roundNumber"]; exists {
		t.Fatalf("expected all rounds when round number is zero, got %v", filter)
	}
}

func TestSubmissionFilterScopesActiveRoundAndPanelist(t *testing.T) {
	filter := submissionFilter("project-1", "MAT", 2, "panelist-1")
	if filter["roundNumber"] != 2 || filter["panelistId"] != "panelist-1" {
		t.Fatalf("expected round and panelist scope, got %v", filter)
	}
}
