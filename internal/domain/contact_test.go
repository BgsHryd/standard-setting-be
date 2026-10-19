package domain

import (
	"testing"
	"time"
)

func TestContactRequiresOneValidChannelAndNormalizesIndonesianNumber(t *testing.T) {

	if _, _, err := NormalizeContact("", ""); err == nil {
		t.Fatal("empty channels accepted")
	}
	wa, email, err := NormalizeContact("0812 3456 7890", "")
	if err != nil || wa != "+6281234567890" || email != "" {
		t.Fatalf("normalization: %q %q %v", wa, email, err)
	}
	wa, email, err = NormalizeContact("", " person@example.org ")
	if err != nil || wa != "" || email != "person@example.org" {
		t.Fatalf("email-only: %q %q %v", wa, email, err)
	}
	for _, values := range [][2]string{{"123", "person@example.org"}, {"+6281234567890", "not-an-email"}, {"", "Name <person@example.org>"}} {
		if _, _, err := NormalizeContact(values[0], values[1]); err == nil {
			t.Fatalf("invalid nonempty channel accepted: %+v", values)
		}
	}
}

func TestProjectPeriodUsesInclusiveStartExclusiveEnd(t *testing.T) {
	start, _ := time.Parse(time.RFC3339, "2026-10-20T00:00:00+07:00")
	end, _ := time.Parse(time.RFC3339, "2026-10-21T00:00:00+07:00")
	p := Project{Status: ProjectActive, StartDate: start, EndDate: end}
	if ProjectWritable(p, start.Add(-1)) || !ProjectWritable(p, start) || ProjectWritable(p, end) {
		t.Fatal("period boundary contract violated")
	}
	p.Status = ProjectCompleted
	if ProjectWritable(p, start) {
		t.Fatal("completed project writable")
	}
}
