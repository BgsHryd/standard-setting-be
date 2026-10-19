package service

import (
	"bytes"
	"context"
	"errors"
	"github.com/xuri/excelize/v2"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/store"
	"testing"
)

type failImpactAfterInsert struct{ *store.Mongo }

func (st failImpactAfterInsert) SaveImpactBatch(ctx context.Context, batch domain.ImpactBatch) error {
	if err := st.Mongo.SaveImpactBatch(ctx, batch); err != nil {
		return err
	}
	return errors.New("injected storage failure after insert")
}
func TestImpactTransactionRollsBackRealInsert(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1); err != nil {
		t.Fatal(err)
	}
	data, err := s.ImpactTemplate(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := excelize.OpenReader(bytes.NewReader(data))
	row := []any{"x", "MAT", 100, "persen", 100}
	f.SetSheetRow("Data", "A2", &row)
	buf, _ := f.WriteToBuffer()
	f.Close()
	preview, err := s.PreviewImpact(ctx, p.ID, buf.Bytes())
	if err != nil || !preview.Valid {
		t.Fatalf("preview %+v %v", preview, err)
	}
	failing := New(failImpactAfterInsert{st}, nil, "")
	if _, err = failing.ApplyImpact(ctx, p.ID, "admin", buf.Bytes(), preview.Binding); err == nil {
		t.Fatal("expected storage failure")
	}
	rows, err := s.ImpactResults(ctx, p.ID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("transaction leaked inserted batch: %+v %v", rows, err)
	}
}
func TestEvaluationAggregationKeepsConstructsSeparateAndAccessScoped(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	if _, err := s.SaveEvaluation(ctx, "pic", domain.Evaluation{Confidence: 5, Training: 5, TimeAdequacy: 5}); err == nil {
		t.Fatal("PIC evaluation accepted")
	}
	p.Subjects[0].PanelistUserIDs = append(p.Subjects[0].PanelistUserIDs, "p3")
	p.Subjects[0].PanelistCount = 3
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertUsers(ctx, []domain.User{{ID: "p3", Username: "qa3", ProjectID: p.ID, SubjectCode: "MAT", Role: domain.RolePanelist, Active: true}}); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"p1", "p2", "p3"} {
		if _, err := s.SaveEvaluation(ctx, id, domain.Evaluation{Confidence: i + 1, Training: 5, TimeAdequacy: 4, Comment: "private"}); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := s.EvaluationSummary(ctx, p.ID)
	if err != nil || summary.SmallPanel || summary.Invited != 3 || summary.Responded != 3 || summary.Missing != 0 || len(summary.Questions) != 3 {
		t.Fatalf("summary %+v %v", summary, err)
	}
	if summary.Questions[0].Median != 2 || summary.Questions[1].Median != 5 || summary.Questions[2].Median != 4 || summary.Questions[0].Distribution != [5]int{1, 1, 1, 0, 0} {
		t.Fatalf("wrong constructs %+v", summary)
	}
	csv, err := s.EvaluationCSV(ctx, p.ID, "id")
	if err != nil || bytes.Contains(csv, []byte("private")) || bytes.Contains(csv, []byte("qa3")) {
		t.Fatalf("privacy %q %v", csv, err)
	}
	p.Status = domain.ProjectCompleted
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveEvaluation(ctx, "p1", domain.Evaluation{Version: 1, Confidence: 5, Training: 5, TimeAdequacy: 5}); err != nil {
		t.Fatal("completed project must allow evaluation", err)
	}
}
