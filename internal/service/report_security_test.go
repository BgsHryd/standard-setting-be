package service

import (
	"archive/zip"
	"bytes"
	"github.com/xuri/excelize/v2"
	"standard-setting/backend/internal/domain"
	"strings"
	"testing"
)

func impactTestFile(t *testing.T, rows [][]any) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	f.SetSheetName("Sheet1", "Data")
	f.NewSheet("_binding")
	for i, h := range impactHeaders {
		c, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellStr("Data", c, h)
	}
	f.SetCellStr("_binding", "A1", "p")
	f.SetCellInt("_binding", "B1", 1)
	f.SetCellStr("_binding", "C1", `[{"subjectCode":"MAT","snapshotId":"s1","roundNumber":1,"revision":1}]`)
	f.SetCellStr("_binding", "D1", "impact-1")
	for i, row := range rows {
		c, _ := excelize.CoordinatesToCellName(1, i+2)
		f.SetSheetRow("Data", c, &row)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func impactTestReport() ProjectReport {
	return ProjectReport{Project: domain.Project{ID: "p", Version: 1}, Subjects: []ReportSubject{{Subject: domain.ProjectSubject{Code: "MAT"}, Available: true, SnapshotID: "s1", Round: ReportRound{Number: 1, Revision: 1}, Statistics: domain.ItemResult{Cutoff: 64.27, OperationalCutoff: 65}, RoundingPolicy: "ceil_1pp"}}}
}
func TestImpactWorkbookSecurityAndRows(t *testing.T) {
	report := impactTestReport()
	data := impactTestFile(t, [][]any{{"001", "MAT", 65, "persen", 100}, {"002", "MAT", 32.5, "mentah", 50}})
	r, err := parseImpact(report, data)
	if err != nil || !r.Valid || len(r.Scores) != 2 || r.Subjects[0].Passed != 2 {
		t.Fatalf("normalization/inclusive %+v %v", r, err)
	}
	cases := []struct {
		name string
		rows [][]any
	}{{"duplicate", [][]any{{"x", "MAT", 1, "persen", 100}, {"x", "MAT", 2, "persen", 100}}}, {"foreign", [][]any{{"x", "OTHER", 70, "persen", 100}}}, {"nonfinite", [][]any{{"x", "MAT", "NaN", "persen", 100}}}, {"outside", [][]any{{"x", "MAT", 101, "persen", 100}}}, {"scale", [][]any{{"x", "MAT", 65, "percent", 100}}}, {"zero maximum", [][]any{{"x", "MAT", 0, "mentah", 0}}}, {"formula identifier", [][]any{{" =1+1", "MAT", 60, "persen", 100}}}, {"empty", nil}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := parseImpact(report, impactTestFile(t, tc.rows))
			if err != nil || r.Valid || len(r.Errors) == 0 {
				t.Fatalf("accepted: %+v %v", r, err)
			}
		})
	}
	f, _ := excelize.OpenReader(bytes.NewReader(data))
	f.SetCellFormula("Data", "C2", "SUM(60,5)")
	buf, _ := f.WriteToBuffer()
	f.Close()
	if _, err = parseImpact(report, buf.Bytes()); err == nil || !strings.Contains(err.Error(), "formula") {
		t.Fatal("formula accepted", err)
	}
	f, _ = excelize.OpenReader(bytes.NewReader(data))
	f.SetCellHyperLink("Data", "A2", "https://example.invalid", "External")
	buf, _ = f.WriteToBuffer()
	f.Close()
	if _, err = parseImpact(report, buf.Bytes()); err == nil {
		t.Fatal("external relationship accepted")
	}
	report.Project.Version++
	if _, err = parseImpact(report, data); err == nil {
		t.Fatal("stale project accepted")
	}
	report.Project.Version--
	report.Subjects[0].SnapshotID = "s2"
	if _, err = parseImpact(report, data); err == nil {
		t.Fatal("stale snapshot accepted")
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	w, _ := z.Create("xl/vbaProject.bin")
	w.Write([]byte("macro"))
	z.Close()
	if validateImpactArchive(out.Bytes()) == nil {
		t.Fatal("macro accepted")
	}
	if validateImpactArchive(make([]byte, 5*1024*1024+1)) == nil {
		t.Fatal("oversize accepted")
	}
}
func TestReportCSVPrivacyLocaleAndInjection(t *testing.T) {
	if got := csvCell("12345678901234567890", "standard"); got != "'12345678901234567890" {
		t.Fatalf("long numeric identifier loses text type: %q", got)
	}
	data, err := encodeReportCSV([]string{"id", "score", "comment"}, [][]any{{"001", 64.27, " \t=HYPERLINK(\"x\")"}, {"a\"b\nc", -2.5, "normal"}}, "id")
	if err != nil || !bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) || !strings.Contains(string(data), "'001;64,27;") || !strings.Contains(string(data), "' \t=HYPERLINK") || !strings.Contains(string(data), "-2,5") || !strings.HasSuffix(string(data), "\r\n") {
		t.Fatalf("CSV %q %v", data, err)
	}
}

func TestImpactInclusiveBoundaryUsesCorrectedOperationalCutoff(t *testing.T) {
	report := impactTestReport()
	report.Subjects[0].Statistics = domain.ComputeStatistics([]float64{263.0 / 3, 172.0 / 3, 212.0 / 3, 233.0 / 3, 5.0 / 3}, "ceil_1pp")
	data := impactTestFile(t, [][]any{{"equal-percent", "MAT", 59, "persen", 100}, {"equal-raw", "MAT", 177, "mentah", 300}, {"below", "MAT", 58.9999999, "persen", 100}})
	result, err := parseImpact(report, data)
	if err != nil || !result.Valid || len(result.Subjects) != 1 || result.Subjects[0].OperationalCutoff != 59 || result.Subjects[0].Passed != 2 || result.Subjects[0].Failed != 1 {
		t.Fatalf("equality at corrected cutoff must pass: %+v %v", result, err)
	}
	if !result.Scores[0].Passed || !result.Scores[1].Passed || result.Scores[2].Passed {
		t.Fatalf("inclusive decisions %+v", result.Scores)
	}
}
