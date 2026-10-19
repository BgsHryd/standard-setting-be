package service

import (
	"math"
	"testing"
)

func TestStatisticsKnownPopulationDataset(t *testing.T) {
	s := calculateStatistics([]float64{40, 60, 80})
	if s.Mean != 60 || s.Minimum != 40 || s.Maximum != 80 || math.Abs(s.StandardDev-math.Sqrt(800.0/3)) > 1e-9 {
		t.Fatalf("wrong population statistics %+v", s)
	}
}
func TestMonitoringAllExcludesInactiveAndZeroRoster(t *testing.T) {
	ctx, s, _, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if err := s.SetUserActive(ctx, "p2", false); err != nil {
		t.Fatal(err)
	}
	all, err := s.MonitoringAll(ctx, p.ID, 0)
	if err != nil || len(all.Subjects) != 1 || len(all.Subjects[0].Panelists) != 1 || all.Subjects[0].Statistics == nil || all.Subjects[0].Statistics.PanelistCount != 1 {
		t.Fatalf("all-subject effective roster %+v %v", all, err)
	}
	if _, err := s.PreviewPanelist(ctx, p.ID, "MAT", 1, "p2"); err == nil {
		t.Fatal("inactive preview exposed")
	}
	s.SetUserActive(ctx, "p1", false)
	zero, err := s.Monitoring(ctx, p.ID, "MAT")
	if err != nil || zero.Statistics != nil || zero.CanFinalize || len(zero.Panelists) != 0 {
		t.Fatalf("zero roster misleading %+v %v", zero, err)
	}
}
func TestMonitoringAllRepresentsMissingRoundPerSubject(t *testing.T) {
	ctx, s, _, p := fixture(t, true)
	all, err := s.MonitoringAll(ctx, p.ID, 2)
	if err != nil || len(all.Subjects) != 1 || all.Subjects[0].Round.Status != "unavailable" || all.Subjects[0].Statistics != nil {
		t.Fatalf("missing round should remain visible and null: %+v %v", all, err)
	}
}
