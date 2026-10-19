package service

import (
	"bytes"
	"encoding/json"
	"github.com/xuri/excelize/v2"
	"standard-setting/backend/internal/domain"
	"strings"
	"testing"
)

func TestReportsFrozenExportAndImpactAtomicBinding(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1); err != nil {
		t.Fatal(err)
	}
	report, err := s.ProjectReport(ctx, p.ID, 1)
	if err != nil || len(report.Subjects) != 1 || report.Subjects[0].SnapshotID == "" {
		t.Fatalf("report %+v %v", report, err)
	}
	frozen := report.Subjects[0].Statistics
	u, _ := st.FindByID(ctx, "p2")
	u.Active = false
	if err := st.SaveUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	again, err := s.ProjectReport(ctx, p.ID, 1)
	if err != nil || again.Subjects[0].Statistics.Mean != frozen.Mean || again.Subjects[0].Statistics.PanelistCount != 2 {
		t.Fatal("roster mutation changed report", err)
	}
	csv, err := s.ExportReport(ctx, p.ID, 1, "id", "raw")
	if err != nil || !bytes.HasPrefix(csv, []byte("\xef\xbb\xbf")) || !strings.Contains(string(csv), ";p2;") || strings.Contains(string(csv), "password") {
		t.Fatalf("export %s %v", csv, err)
	}
	data, err := s.ImpactTemplate(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	row := []any{"001", "MAT", frozen.OperationalCutoff, "persen", 100}
	if err = f.SetSheetRow("Data", "A2", &row); err != nil {
		t.Fatal(err)
	}
	row = []any{"002", "MAT", frozen.OperationalCutoff / 2, "mentah", 50}
	f.SetSheetRow("Data", "A3", &row)
	b, _ := f.WriteToBuffer()
	f.Close()
	validFile := append([]byte(nil), b.Bytes()...)
	preview, err := s.PreviewImpact(ctx, p.ID, b.Bytes())
	if err != nil || !preview.Valid || preview.Count != 2 {
		t.Fatalf("preview %+v %v", preview, err)
	}
	badBinding := preview.Binding
	badBinding.FileHash = "wrong"
	if _, err = s.ApplyImpact(ctx, p.ID, "admin", b.Bytes(), badBinding); err == nil {
		t.Fatal("changed file binding accepted")
	}
	if batches, _ := s.ImpactResults(ctx, p.ID); len(batches) != 0 {
		t.Fatal("invalid apply persisted")
	}
	batch, err := s.ApplyImpact(ctx, p.ID, "admin", b.Bytes(), preview.Binding)
	if err != nil || len(batch.Subjects) != 1 || batch.Subjects[0].Passed != 2 {
		t.Fatalf("impact %+v %v", batch, err)
	}
	payload, _ := json.Marshal(batch)
	if !strings.Contains(string(payload), report.Subjects[0].SnapshotID) {
		t.Fatal("missing snapshot provenance")
	}
	f, _ = excelize.OpenReader(bytes.NewReader(b.Bytes()))
	f.SetCellValue("Data", "C3", 101)
	b, _ = f.WriteToBuffer()
	f.Close()
	invalid, err := s.PreviewImpact(ctx, p.ID, b.Bytes())
	if err != nil || invalid.Valid || len(invalid.Errors) == 0 || invalid.Errors[0].Row != 3 {
		t.Fatalf("invalid rows %+v %v", invalid, err)
	}
	if _, err = s.ApplyImpact(ctx, p.ID, "admin", b.Bytes(), invalid.Binding); err == nil {
		t.Fatal("partial import accepted")
	}
	if batches, _ := s.ImpactResults(ctx, p.ID); len(batches) != 1 {
		t.Fatal("bad row wrote a batch")
	}
	// A new finalized snapshot never rewrites an existing analysis batch.
	rounds, err := st.ListRounds(ctx, p.ID, "MAT")
	if err != nil {
		t.Fatal(err)
	}
	for _, round := range rounds {
		if round.Number == 1 {
			copy := *round.FinalSnapshot
			copy.ID = "new-final-snapshot"
			copy.Revision++
			round.FinalSnapshot = &copy
			round.Snapshots = append(round.Snapshots, copy)
			if err = st.SaveRound(ctx, round); err != nil {
				t.Fatal(err)
			}
		}
	}
	batches, err := s.ImpactResults(ctx, p.ID)
	if err != nil || len(batches) != 1 || !batches[0].Subjects[0].Stale || batches[0].Subjects[0].CutoffSnapshotID == "new-final-snapshot" {
		t.Fatalf("historical batch overwritten/unlabelled: %+v %v", batches, err)
	}
	if _, err = s.ApplyImpact(ctx, p.ID, "admin", validFile, preview.Binding); err == nil || !strings.Contains(err.Error(), "kedaluwarsa") {
		t.Fatal("stale snapshot/template accepted")
	}

}
func TestEvaluationVersionPrivacyAndClose(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	in := domain.Evaluation{Confidence: 4, Training: 3, TimeAdequacy: 5, Comment: "Cukup"}
	first, err := s.SaveEvaluation(ctx, "p1", in)
	if err != nil || first.Version != 1 || first.InstrumentVersion != 1 {
		t.Fatalf("evaluation %+v %v", first, err)
	}
	if _, err = s.SaveEvaluation(ctx, "p1", in); err == nil {
		t.Fatal("stale evaluation overwrite accepted")
	}
	in.Version = first.Version
	in.Confidence = 5
	next, err := s.SaveEvaluation(ctx, "p1", in)
	if err != nil || next.Version != 2 {
		t.Fatalf("revision %+v %v", next, err)
	}
	summary, err := s.EvaluationSummary(ctx, p.ID)
	if err != nil || summary.Responded != 1 || !summary.SmallPanel || len(summary.Questions) != 0 {
		t.Fatalf("privacy %+v %v", summary, err)
	}
	p.EvaluationClosed = true
	if err = st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	in.Version = 2
	if _, err = s.SaveEvaluation(ctx, "p1", in); err == nil {
		t.Fatal("closed evaluation accepted")
	}
	if got, err := s.GetEvaluation(ctx, "p1"); err != nil || got.Response == nil || got.Response.Version != 2 {
		t.Fatalf("read response %+v %v", got, err)
	}
}
