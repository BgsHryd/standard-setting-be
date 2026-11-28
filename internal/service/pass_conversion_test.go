package service

import (
	"context"
	"math"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
	"standard-setting/backend/internal/domain"
)

func passConversionWorkbook(t *testing.T, rows ...[]any) []byte {
	t.Helper()
	book := excelize.NewFile()
	defer book.Close()
	if err := book.SetSheetRow("Sheet1", "A1", &passConversionHeaders); err != nil {
		t.Fatal(err)
	}
	for index, row := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, index+2)
		if err := book.SetSheetRow("Sheet1", cell, &row); err != nil {
			t.Fatal(err)
		}
	}
	buffer, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestPassConversionMatchesRoundAndSubject(t *testing.T) {
	conversion, err := parsePassConversion(passConversionWorkbook(t,
		[]any{"APR", 56, 23.45}, []any{"SEJ", 56, 0}, []any{"APR", 55, 46.9},
	))
	if err != nil || conversion.RowCount != 3 || conversion.SubjectCount != 2 {
		t.Fatal(conversion, err)
	}
	raw := 55.5 - 1e-12
	stats := domain.ItemResult{Cutoff: 50, PanelistCount: 2, ScoringMethod: domain.ScoringWeighted}
	for subject, want := range map[string]float64{"apr": 23.45, "SEJ": 0} {
		got := conversion.ProjectedPassPercentage(subject, &stats, &raw, 112)
		if got == nil || *got != want {
			t.Fatalf("%s projection = %v, expected %g", subject, got, want)
		}
	}
	if got := conversion.ProjectedPassPercentage("APR", &stats, nil, 112); got == nil || *got != 23.45 {
		t.Fatal("legacy weighted cutoff did not map", got)
	}
	if missing := conversion.ProjectedPassPercentage("APR", &stats, nil, 100); missing != nil {
		t.Fatal("a missing exact key was filled", missing)
	}
	for _, rows := range [][][]any{
		{{"APR", 1, 50}, {"apr", 1, 20}},
		{{"APR", 1.2, 10}},
		{{"APR", 1, 120}},
		{{"APR", 1}},
	} {
		if _, err := parsePassConversion(passConversionWorkbook(t, rows...)); err == nil {
			t.Fatalf("invalid workbook accepted: %v", rows)
		}
	}
}

func TestPassConversionSuppliedWorkbook(t *testing.T) {
	path := os.Getenv("QA_PASS_CONVERSION_SAMPLE")
	if path == "" {
		t.Skip("set QA_PASS_CONVERSION_SAMPLE to the supplied example workbook")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	conversion, err := parsePassConversion(data)
	if err != nil || conversion.RowCount != 222 || conversion.SubjectCount != 2 {
		t.Fatal(conversion, err)
	}
	if conversion.Percentages["APR"]["0"] != 100 || conversion.Percentages["SEJ"]["110"] != 0 {
		t.Fatal("example scores or units were read incorrectly")
	}
}

func TestPassConversionInRoundSummaryWorkbookAndReview(t *testing.T) {
	s, st := roundWorkbookFixture()
	conversion, err := parsePassConversion(passConversionWorkbook(t, []any{"MAT", 2, 36.58}))
	if err != nil {
		t.Fatal(err)
	}
	st.project.PassConversion = conversion
	archive, _, err := s.ExportRoundWorkbooks(context.Background(), st.project.ID, "MAT", true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for path, book := range openRoundWorkbooks(t, archive) {
		if !strings.Contains(path, "rekap_") {
			continue
		}
		rows, err := book.GetRows("Summary")
		if err != nil {
			t.Fatal(err)
		}
		column := slices.Index(rows[0], "Proyeksi kelulusan (%)")
		if column < 0 || len(rows[1]) <= column || rows[1][column] != "36.58%" {
			t.Fatal("missing workbook projection:", rows)
		}
		found = true
	}
	if !found {
		t.Fatal("summary workbook missing from ZIP")
	}
	rounds := st.rounds["MAT"]
	review, outcomes, err := s.roundResults(context.Background(), "a", st.project, st.project.Subjects[0], rounds[1], rounds)
	if err != nil || review == nil || len(outcomes) != 1 || review.ProjectionPercent == nil || math.Abs(*review.ProjectionPercent-36.58) > 1e-10 {
		t.Fatal(review, outcomes, err)
	}
}

func TestPassConversionUploadAcrossProjectStatuses(t *testing.T) {
	ctx, s, st, project := fixture(t, false)
	for _, status := range []domain.ProjectStatus{domain.ProjectDraft, domain.ProjectActive, domain.ProjectCompleted} {
		project.Status = status
		if err := st.SaveProject(ctx, project); err != nil {
			t.Fatal(err)
		}
		upload := passConversionWorkbook(t, []any{"MAT", 3, 36.58})
		saved, err := s.SaveProjectPassConversion(ctx, project.ID, "admin", "konversi.xlsx", upload)
		if err != nil || saved.Filename != "konversi.xlsx" {
			t.Fatal(saved, err)
		}
		if _, err = s.SaveProjectPassConversion(ctx, project.ID, "admin", "salah.xlsx", []byte("invalid")); err == nil {
			t.Fatal("invalid replacement accepted")
		}
		project, err = st.GetProject(ctx, project.ID)
		if err != nil || project.Status != status || project.PassConversion == nil ||
			project.PassConversion.Filename != "konversi.xlsx" || project.PassConversion.Percentages["MAT"]["3"] != 36.58 {
			t.Fatal(project, err)
		}
	}
}

func TestPassConversionReplacementUpdatesFinalReportAndMonitoring(t *testing.T) {
	ctx, s, _, project := fixture(t, true)
	fillRound(t, ctx, s, project, 1, 0)
	if _, err := s.RoundAction(ctx, project.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1); err != nil {
		t.Fatal(err)
	}
	before, err := s.ProjectReport(ctx, project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	original := before.Subjects[0]
	rawScore := domain.RoundRawPoints(*original.AngoffReview.MeanRaw)
	for _, want := range []float64{36.58, 0} {
		data := passConversionWorkbook(t, []any{"MAT", rawScore, want}, []any{"SEJ", rawScore, 100})
		if _, err := s.SaveProjectPassConversion(ctx, project.ID, "admin", "conversion.xlsx", data); err != nil {
			t.Fatal(err)
		}
		report, err := s.ProjectReport(ctx, project.ID, 1)
		if err != nil {
			t.Fatal(err)
		}
		row := report.Subjects[0]
		if row.SnapshotID != original.SnapshotID || row.Statistics.Cutoff != original.Statistics.Cutoff ||
			row.Statistics.ProjectionPercent == nil || *row.Statistics.ProjectionPercent != want {
			t.Fatalf("unexpected report after replacement: %+v", row)
		}
		monitoring, err := s.monitoringRound(ctx, project.ID, "MAT", 1)
		if err != nil || monitoring.Statistics == nil || monitoring.Statistics.ProjectionPercent == nil ||
			*monitoring.Statistics.ProjectionPercent != want {
			t.Fatalf("unexpected monitoring projection: %+v, %v", monitoring.Statistics, err)
		}
	}
}
