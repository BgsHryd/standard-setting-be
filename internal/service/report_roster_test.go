package service

import (
	"bytes"
	"encoding/csv"
	"standard-setting/backend/internal/domain"
	"testing"
)

func TestRawReportIncludesUnansweredExcludedAndInactiveRoster(t *testing.T) {
	for _, inactive := range []bool{false, true} {
		t.Run(map[bool]string{false: "excluded", true: "inactive"}[inactive], func(t *testing.T) {
			ctx, s, _, p := fixture(t, true)
			answers := []domain.Answer{{QuestionType: domain.QuestionTypePG, Value: intValue(60)}, {QuestionType: domain.QuestionTypeComplex, Distribution: []int{0, 0, 0, 100}}, {QuestionType: domain.QuestionTypeEssay, Distribution: []int{0, 0, 100, 0, 0}}}
			for i, a := range answers {
				if _, err := s.SaveSubmission(ctx, "p1", "", p.ID, "MAT", 1, p.Subjects[0].ItemIDs[i], a); err != nil {
					t.Fatal(err)
				}
			}
			if inactive {
				if err := s.SetUserActive(ctx, "p2", false); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := s.SetRoundExclusion(ctx, "pic", "round-1", "p2", "Berhalangan", 1, true); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
				t.Fatal(err)
			}
			r, err := s.ProjectReport(ctx, p.ID, 1)
			if err != nil || len(r.Subjects) != 1 || len(r.Subjects[0].Responses) != 6 {
				t.Fatalf("raw matrix incomplete: %+v %v", r, err)
			}
			missing := 0
			for _, row := range r.Subjects[0].Responses {
				if row.Submission.PanelistID == "p2" {
					missing++
					if !row.Missing || row.Included || row.Submission.PanelistName != "B" {
						t.Fatalf("incorrect missing flags: %+v", row)
					}
				}
			}
			if missing != 3 {
				t.Fatalf("missing excluded/inactive panelist: %d", missing)
			}
			data, err := s.ExportReport(ctx, p.ID, 1, "standard", "raw")
			if err != nil {
				t.Fatal(err)
			}
			rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))).ReadAll()
			if err != nil || len(rows) != 7 {
				t.Fatalf("CSV matrix: %d %v", len(rows), err)
			}
		})
	}
}
func TestImpactExact58BoundaryAndGenuinelyBelow(t *testing.T) {
	report := impactTestReport()
	report.Subjects[0].Statistics.OperationalCutoff = 58
	data := impactTestFile(t, [][]any{{"eqpercent", "MAT", 58, "persen", 100}, {"eqraw", "MAT", 29, "mentah", 50}, {"below", "MAT", 57.9999999, "persen", 100}})
	r, err := parseImpact(report, data)
	if err != nil || !r.Valid || len(r.Subjects) != 1 || r.Subjects[0].Passed != 2 || r.Subjects[0].Failed != 1 {
		t.Fatalf("inclusive boundary failed: %+v %v", r, err)
	}
}
