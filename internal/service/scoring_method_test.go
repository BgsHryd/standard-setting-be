package service

import (
	"bytes"
	"context"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/store"
)

func TestRecalculateSnapshotUsesFrozenIncludedResponses(t *testing.T) {
	_, st := roundWorkbookFixture()
	snap := *st.rounds["MAT"][0].FinalSnapshot
	// Excluded old shapes cannot change original item maximums.
	snap.Submissions = append(snap.Submissions, domain.Submission{ItemID: "ESSAY", PanelistID: "c", Status: "saved", CandidateVersion: 1, Answer: domain.Answer{QuestionType: domain.QuestionTypeEssay, Distribution: []int{0, 0, 0, 0, 100}}})
	before := append([]domain.Submission(nil), snap.Submissions...)
	updated, err := recalculateSnapshot(snap, "average", "none")
	if err != nil || math.Abs(updated.Statistics.Cutoff-67.5) > 1e-10 || updated.ScoringMethod != "average" {
		t.Fatalf("frozen average incorrect: %+v %v", updated.Statistics, err)
	}
	if !reflect.DeepEqual(before, updated.Submissions) || !reflect.DeepEqual(snap.ItemStatistics, updated.ItemStatistics) || snap.Statistics.Cutoff != 70 {
		t.Fatal("rescore modified answers, item statistics, or input snapshot")
	}
	back, err := recalculateSnapshot(updated, "weighted", "none")
	if err != nil || back.Statistics.Cutoff != 70 {
		t.Fatal("weighted revert differs", back.Statistics, err)
	}
	broken := snap
	broken.Submissions = broken.Submissions[1:]
	if _, err := recalculateSnapshot(broken, "average", "none"); err == nil {
		t.Fatal("incomplete snapshot fabricated a result")
	}
}

func TestProjectScoringMethodDefaultAndValidation(t *testing.T) {
	ctx, s, _, _ := fixture(t, false)
	p, err := s.SaveProject(ctx, domain.Project{Name: "Default method"}, "admin")
	if err != nil || p.ScoringMethod != "weighted" {
		t.Fatal("new project must default weighted", p.ScoringMethod, err)
	}
	p.ScoringMethod = "unknown"
	if _, err := s.SaveProject(ctx, p, "admin"); err == nil {
		t.Fatal("invalid method accepted")
	}
	p.ScoringMethod = "average"
	p, err = s.SaveProject(ctx, p, "admin")
	if err != nil {
		t.Fatal(err)
	}
	p.ScoringMethod = "" // An older client omits the field on its next edit.
	p, err = s.SaveProject(ctx, p, "admin")
	if err != nil || p.ScoringMethod != "average" {
		t.Fatal("omitted method reset Average", err)
	}
	copy, err := s.CopyProject(ctx, p.ID, "Copy Average", "admin")
	if err != nil || copy.ScoringMethod != "average" {
		t.Fatal("copy lost scoring method", err)
	}
}

func TestScoringMethodInvalidatesOldImpactAtSameCutoff(t *testing.T) {
	batch := domain.ImpactSubject{CutoffSnapshotID: "same-id", OperationalCutoff: 50}
	if impactMatchesResult(batch, "same-id", domain.ItemResult{OperationalCutoff: 50, ScoringMethod: "average"}) {
		t.Fatal("old Weighted impact treated as Average")
	}
	if !impactMatchesResult(batch, "same-id", domain.ItemResult{OperationalCutoff: 50, ScoringMethod: "weighted"}) {
		t.Fatal("legacy Weighted batch became stale without a change")
	}
}

func TestProjectScoringMethodRecalculatesLockedAndCompletedResults(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.PlannedRounds = 1
	p.RoundingPolicy = "none"
	p.ScoringMethod = "weighted"
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1); err != nil {
		t.Fatal(err)
	}
	decision, err := s.LockFinalNBK(ctx, "pic", p.ID, "MAT", 1, 1, p.Version, "QA")
	if err != nil || decision.Statistics.Cutoff != 71.25 {
		t.Fatal("weighted baseline", decision, err)
	}
	rounds, _ := st.ListRounds(ctx, p.ID, "MAT")
	before := *rounds[0].FinalSnapshot
	// Save an impact batch so mode changes must invalidate its old pass decision.
	template, err := s.ImpactTemplate(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := excelize.OpenReader(bytes.NewReader(template))
	row := []any{"x", "MAT", 72, "persen", 100}
	f.SetSheetRow("Data", "A2", &row)
	buf, _ := f.WriteToBuffer()
	f.Close()
	preview, err := s.PreviewImpact(ctx, p.ID, buf.Bytes())
	if err != nil || !preview.Valid {
		t.Fatal(preview, err)
	}
	if _, err = s.ApplyImpact(ctx, p.ID, "admin", buf.Bytes(), preview.Binding); err != nil {
		t.Fatal(err)
	}
	fresh, _ := st.GetProject(ctx, p.ID)
	fresh.ScoringMethod = "average"
	saved, err := s.SaveProject(ctx, fresh, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(saved.Subjects[0].FinalNBK.Statistics.Cutoff-220.0/3) > 1e-10 {
		t.Fatal("final NBK did not recalculate", saved.Subjects[0].FinalNBK)
	}
	rounds, _ = st.ListRounds(ctx, p.ID, "MAT")
	for _, snap := range append(rounds[0].Snapshots, *rounds[0].FinalSnapshot) {
		if math.Abs(snap.Statistics.Cutoff-220.0/3) > 1e-10 || snap.ScoringMethod != "average" {
			t.Fatal("archive/final snapshot not rescored")
		}
	}
	if rounds[0].FinalSnapshot.ID != before.ID || !reflect.DeepEqual(rounds[0].FinalSnapshot.Submissions, before.Submissions) || !reflect.DeepEqual(rounds[0].FinalSnapshot.ItemStatistics, before.ItemStatistics) {
		t.Fatal("answers/round identity changed")
	}
	report, err := s.ProjectReport(ctx, p.ID, 1)
	if err != nil || math.Abs(report.Subjects[0].Statistics.Cutoff-220.0/3) > 1e-10 {
		t.Fatal("report method stale", err)
	}
	view, err := s.PICRoundOverview(ctx, "pic", 1)
	if err != nil || math.Abs(view.RoundReview.NBKPercent-220.0/3) > 1e-10 {
		t.Fatal("PIC overview method stale", err)
	}
	impacts, err := s.ImpactResults(ctx, p.ID)
	if err != nil || !impacts[0].Subjects[0].Stale || view.RoundReview.ProjectionPercent != nil {
		t.Fatal("old impact decisions shown as current", err)
	}
	if _, err = s.CompleteProject(ctx, p.ID, "SELESAIKAN PROYEK"); err != nil {
		t.Fatal(err)
	}
	complete, _ := st.GetProject(ctx, p.ID)
	complete.ScoringMethod = "weighted"
	complete.Name = "forged structural edit"
	complete, err = s.SaveProject(ctx, complete, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if complete.Status != domain.ProjectCompleted || complete.Name == "forged structural edit" || complete.ScoringMethod != "weighted" || complete.Subjects[0].FinalNBK.Statistics.Cutoff != 71.25 {
		t.Fatal("completed mode-only update failed", complete)
	}
}

type failingScoringStore struct{ *store.Mongo }

func (st failingScoringStore) SaveRound(ctx context.Context, round domain.Round) error {
	if err := st.Mongo.SaveRound(ctx, round); err != nil {
		return err
	}
	return errors.New("injected rescore failure")
}

func TestProjectScoringMethodChangeIsAtomic(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.ScoringMethod = "weighted"
	p.RoundingPolicy = "none"
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1); err != nil {
		t.Fatal(err)
	}
	p.ScoringMethod = "average"
	failing := New(failingScoringStore{st}, nil, "")
	if _, err := failing.SaveProject(ctx, p, "admin"); err == nil {
		t.Fatal("rescore failure ignored")
	}
	fresh, _ := st.GetProject(ctx, p.ID)
	rounds, _ := st.ListRounds(ctx, p.ID, "MAT")
	if fresh.ScoringMethod != "weighted" || rounds[0].FinalSnapshot.Statistics.Cutoff != 71.25 {
		t.Fatal("partial mode change escaped transaction")
	}
}

func TestScoringMethodDrivesActiveMonitoringFinalizationAndWorkbook(t *testing.T) {
	for _, tc := range []struct {
		method string
		want   float64
	}{{"weighted", 71.25}, {"average", 220.0 / 3}} {
		t.Run(tc.method, func(t *testing.T) {
			ctx, s, st, p := fixture(t, true)
			p.ScoringMethod = tc.method
			p.RoundingPolicy = "none"
			p.PlannedRounds = 1
			if err := st.SaveProject(ctx, p); err != nil {
				t.Fatal(err)
			}
			fillRound(t, ctx, s, p, 1, 0)
			m, err := s.Monitoring(ctx, p.ID, "MAT")
			if err != nil || m.Statistics == nil || math.Abs(m.Statistics.Cutoff-tc.want) > 1e-10 {
				t.Fatal("active monitoring wrong method", m.Statistics, err)
			}
			body, _, err := s.ExportRoundWorkbooks(ctx, p.ID, "MAT", true)
			if err != nil {
				t.Fatal(err)
			}
			for name, f := range openRoundWorkbooks(t, body) {
				if strings.Contains(name, "rekap_") {
					value, _ := strconv.ParseFloat(workbookCell(t, f, "B2"), 64)
					if math.Abs(value-tc.want) > 1e-10 {
						t.Fatal("workbook ignored method", value)
					}
				}
			}
			if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1); err != nil {
				t.Fatal(err)
			}
			rounds, _ := st.ListRounds(ctx, p.ID, "MAT")
			if math.Abs(rounds[0].FinalSnapshot.Statistics.Cutoff-tc.want) > 1e-10 || rounds[0].FinalSnapshot.ScoringMethod != tc.method {
				t.Fatal("finalization ignored configured method")
			}
			matrix, err := s.PICItemResults(ctx, "pic", 1)
			if err != nil || matrix.ScoringMethod != tc.method {
				t.Fatal("PIC test score method missing", err)
			}
		})
	}
}

func TestScoringMethodPICHistoryUsesOriginalAnswerShapes(t *testing.T) {
	s, st := monitoringHistoryFixture()
	s.store = &picResultsStore{st}
	st.project.ScoringMethod = "average"
	st.project.Subjects[0].PICUserID = "pic"
	st.project.Subjects[0].ItemIDs = []string{"i", "essay"}
	st.users = append(st.users, domain.User{ID: "pic", Role: domain.RolePICMapel, Active: true, ProjectID: "p", SubjectCode: "MAT"})
	// Both master types/maxima have changed since finalization.
	st.items = []domain.Item{{ID: "i", QuestionType: domain.QuestionTypeEssay, MaxScore: 3}, {ID: "essay", QuestionType: domain.QuestionTypeEssay, MaxScore: 2}}
	snap := st.rounds[0].FinalSnapshot
	snap.ItemStatistics = map[string]domain.ItemResult{"i": domain.ComputeStatistics([]float64{65}, "none"), "essay": domain.ComputeStatistics([]float64{50}, "none")}
	snap.Submissions = append(snap.Submissions,
		domain.Submission{ItemID: "essay", PanelistID: "u", Status: "saved", CandidateVersion: 1, RoundRevision: 1, Answer: domain.Answer{QuestionType: domain.QuestionTypeEssay, Distribution: []int{0, 0, 100, 0, 0}}},
		domain.Submission{ItemID: "essay", PanelistID: "excluded", Status: "saved", CandidateVersion: 1, Answer: domain.Answer{QuestionType: domain.QuestionTypeEssay, Distribution: []int{0, 100}}},
	)
	updated, err := recalculateSnapshot(*snap, "average", "none")
	if err != nil {
		t.Fatal(err)
	}
	st.rounds[0].FinalSnapshot = &updated
	result, err := s.PICItemResults(context.Background(), "pic", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[0].MaxScore != 1 || result.Items[0].QuestionType != domain.QuestionTypePG || result.Items[0].Values["u"] != 65 || result.Items[1].MaxScore != 4 || result.Items[1].Values["u"] != 50 {
		t.Fatal("PIC matrix lost frozen responses after rescore", result.Items)
	}
	m, err := s.MonitoringAtRound(context.Background(), "p", "MAT", 1)
	if err != nil || m.Panelists[0].Done != 2 || m.Statistics.Cutoff != 57.5 {
		t.Fatal("historical monitoring differs from recalculated NBK", err)
	}
	if maximum := snapshotMaxScore(updated); maximum == nil || *maximum != 5 {
		t.Fatal("excluded shape changed equivalent score maximum", maximum)
	}
}
