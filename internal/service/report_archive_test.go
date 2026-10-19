package service

import (
	"bytes"
	"standard-setting/backend/internal/domain"
	"testing"
)

func TestArchivedSnapshotSelectionDoesNotBecomeOperationalImpact(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	one := domain.RoundSnapshot{ID: "snapshot-rev1", Revision: 1, RoundNumber: 1, SubjectCode: "MAT", PanelistIDs: []string{"p1"}, Statistics: domain.ComputeStatistics([]float64{60}, "ceil_1pp"), ItemStatistics: map[string]domain.ItemResult{"MAT-001": domain.ComputeStatistics([]float64{60}, "ceil_1pp")}, Submissions: []domain.Submission{{ProjectID: p.ID, SubjectCode: "MAT", PanelistID: "p1", ItemID: "MAT-001", RoundNumber: 1, Answer: domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(60)}, Status: "saved"}}, RoundingPolicy: "ceil_1pp"}
	two := one
	two.ID = "snapshot-rev2"
	two.Revision = 2
	two.Statistics = domain.ComputeStatistics([]float64{70}, "ceil_1pp")
	two.Submissions = append([]domain.Submission(nil), one.Submissions...)
	two.Submissions[0].Answer.Value = intValue(70)
	if err := st.SaveRound(ctx, domain.Round{ID: "round-1", ProjectID: p.ID, SubjectCode: "MAT", Number: 1, Revision: 2, Status: "finalized", FinalSnapshot: &two, Snapshots: []domain.RoundSnapshot{one, two}}); err != nil {
		t.Fatal(err)
	}
	latest, err := s.ProjectReport(ctx, p.ID, 1)
	if err != nil || latest.Subjects[0].SnapshotID != two.ID || len(latest.Subjects[0].ArchiveSnapshots) != 2 {
		t.Fatalf("archive list %+v %v", latest, err)
	}
	old, err := s.ProjectReportRevision(ctx, p.ID, 1, 1)
	if err != nil || old.Subjects[0].SnapshotID != one.ID || old.Subjects[0].Statistics.Cutoff != 60 || !old.Subjects[0].Historical {
		t.Fatalf("archived revision %+v %v", old, err)
	}
	csv, err := s.ExportReportRevision(ctx, p.ID, 1, 1, "standard", "raw")
	if err != nil || !bytes.Contains(csv, []byte(one.ID)) || bytes.Contains(csv, []byte(two.ID)) {
		t.Fatalf("revision export %q %v", csv, err)
	}
	if _, err = s.ProjectReportRevision(ctx, p.ID, 0, 1); err == nil {
		t.Fatal("revision without round accepted")
	}
	if b := impactBindings(latest); len(b) != 1 || b[0].SnapshotID != two.ID {
		t.Fatal("operational binding selected old cutoff", b)
	}
	if b := impactBindings(old); len(b) != 0 {
		t.Fatal("archive used as operational impact cutoff", b)
	}
	reopened := domain.Round{ID: "round-1", ProjectID: p.ID, SubjectCode: "MAT", Number: 1, Revision: 3, Status: "active", FinalSnapshot: &two, Snapshots: []domain.RoundSnapshot{one, two}}
	if err = st.SaveRound(ctx, reopened); err != nil {
		t.Fatal(err)
	}
	historical, err := s.ProjectReportRevision(ctx, p.ID, 1, 1)
	if err != nil || historical.Subjects[0].SnapshotID != one.ID {
		t.Fatal("rollback hid historical revision", err)
	}
}
