package service

import (
	"bytes"
	"encoding/csv"
	"math"
	"os"
	"strconv"
	"testing"

	"github.com/xuri/excelize/v2"
	"standard-setting/backend/internal/domain"
)

// Calculation!F3:J82 are the simulator inputs. Expected outputs come from K83:O92.
func TestMixWorkbook2026(t *testing.T) {
	f, err := os.Open("testdata/mix_angoff_2026.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	ids := records[0][3:]
	data := workbookRound{policy: "nearest_raw_point", values: map[string]map[string]float64{}}
	for _, id := range ids {
		data.panelists = append(data.panelists, workbookPanelist{id, id})
		data.values[id] = map[string]float64{}
	}
	for _, r := range records[1:] {
		max, err := strconv.Atoi(r[2])
		if err != nil {
			t.Fatal(err)
		}
		kind := map[string]domain.QuestionType{"PG": domain.QuestionTypePG, "BS": domain.QuestionTypeComplexBS, "MCMA": domain.QuestionTypeComplexMCMA}[r[1]]
		item := domain.Item{ID: r[0], MaxScore: max, QuestionType: kind}
		data.items = append(data.items, item)
		for j, id := range ids {
			x, err := strconv.Atoi(r[3+j])
			if err != nil {
				t.Fatal(err)
			}
			data.values[id][item.ID] = responseValue(domain.Answer{Value: &x, MaxScore: max, QuestionType: kind}, max)
		}
	}
	data.review = domain.ComputeAngoffReview(data.items, ids, data.values)
	assert := func(label string, v *float64, want float64) {
		t.Helper()
		if v == nil || math.Abs(*v-want) > 1e-8 {
			t.Fatalf("%s: got %v, want %g", label, v, want)
		}
	}
	mix := data.review.Mix
	assert("K84", mix.ModifiedMean, .44608)
	assert("K86", mix.ExtendedMean, 2.6)
	assert("K87", mix.CutoffPoints, 100)
	assert("L87 (%)", mix.CutoffPercent, 50)
	for j, want := range []float64{100.17, 95.15, 104.2, 102.85, 99.15} {
		assert(ids[j], data.review.Rows[j].RawPoints, want)
	}
	assert("K89", mix.SDPoints, 3.520451676)
	assert("K90", mix.SEJPoints, 1.574393852)
	sem := 5.81
	data.uncertainty = domain.RoundStandardSettingError(&sem, nil, nil, data.review, data.policy)
	assert("K92", data.uncertainty.SeSS, 6)
	var scores []float64
	for _, id := range ids {
		score, err := testScoreForItems(data.items, data.values[id], domain.ScoringWeighted)
		if err != nil {
			t.Fatal(err)
		}
		scores = append(scores, score)
	}
	stats := domain.ComputeTestStatistics(scores, data.policy, data.items)
	data.statistics = &stats
	assert("operational decision", &stats.OperationalCutoff, 50)
	summary, err := renderRoundSummary(workbookSubject{rounds: []workbookRound{data}})
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(summary))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	for cell, want := range map[string]float64{"B2": 100, "C2": 3.520451676, "G2": 50} {
		val, err := strconv.ParseFloat(workbookCell(t, book, cell), 64)
		if err != nil {
			t.Fatal(err)
		}
		assert(cell, &val, want)
	}
	body, err := renderRoundResponses(data)
	if err != nil {
		t.Fatal(err)
	}
	sheet, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer sheet.Close()
	flags := 0
	for row := 2; row <= 81; row++ {
		if workbookCell(t, sheet, "I"+strconv.Itoa(row)) == "*" {
			flags++
		}
	}
	if flags != 39 {
		t.Fatalf("tag diskusi: got %d, want 39", flags)
	}
	for cell, want := range map[string]float64{"B2": .49, "B82": .4634, "B83": 2.566666667, "F84": 99.15} {
		val, err := strconv.ParseFloat(workbookCell(t, sheet, cell), 64)
		if err != nil {
			t.Fatal(err)
		}
		assert(cell, &val, want)
	}
}

// Exercise the same 400 simulator judgments through persistence, finalization and export.
func TestMixWorkbookMongoRoundTrip(t *testing.T) {
	ctx, svc, st, p := fixture(t, true)
	sem := 5.81
	sub := &p.Subjects[0]
	sub.SEM = &sem
	sub.ItemIDs = nil
	sub.PanelistUserIDs = []string{"p1", "p2", "p3", "p4", "p5"}
	sub.PanelistNames = []string{"A", "B", "C", "D", "E"}
	sub.PanelistCount = 5
	p.ScoringMethod = domain.ScoringWeighted
	p.RoundingPolicy = domain.NearestRawPoint
	p.PlannedRounds = 1
	for _, id := range sub.PanelistUserIDs[2:] {
		if err := st.InsertUsers(ctx, []domain.User{{ID: id, Username: "qa.mix." + id, Name: id, Role: domain.RolePanelist, Active: true, SubjectCode: sub.Code, ProjectID: p.ID}}); err != nil {
			t.Fatal(err)
		}
	}
	file, err := os.Open("testdata/mix_angoff_2026.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	items := []domain.Item{}
	for _, r := range rows[1:] {
		max, err := strconv.Atoi(r[2])
		if err != nil {
			t.Fatal(err)
		}
		kind := map[string]domain.QuestionType{"PG": domain.QuestionTypePG, "BS": domain.QuestionTypeComplexBS, "MCMA": domain.QuestionTypeComplexMCMA}[r[1]]
		item := domain.Item{ID: "mix-" + r[0], MaxScore: max, QuestionType: kind, SubjectCode: sub.Code, ProjectID: p.ID, UsageStatus: domain.ItemAllocated}
		items = append(items, item)
		sub.ItemIDs = append(sub.ItemIDs, item.ID)
	}
	if err := st.UpsertItems(ctx, items); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	for i, r := range rows[1:] {
		for j, id := range sub.PanelistUserIDs {
			x, err := strconv.Atoi(r[3+j])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.SaveSubmission(ctx, id, id, p.ID, sub.Code, 1, items[i].ID, domain.Answer{QuestionType: items[i].QuestionType, Value: &x}); err != nil {
				t.Fatal(err)
			}
		}
	}
	assert := func(label string, got *float64, want float64) {
		t.Helper()
		if got == nil || math.Abs(*got-want) > 1e-8 {
			t.Fatalf("%s: got %v, want %g", label, got, want)
		}
	}
	for _, final := range []bool{false, true} {
		if final {
			if _, err := svc.RoundAction(ctx, p.ID, sub.Code, "finalize", "FINALISASI PUTARAN", "pic", 1); err != nil {
				t.Fatal(err)
			}
		}
		m, err := svc.MonitoringAtRound(ctx, p.ID, sub.Code, 1)
		if err != nil {
			t.Fatal(err)
		}
		if m.Statistics == nil || m.Statistics.PanelistCount != 5 || m.AngoffReview.MaxPoints != 200 {
			t.Fatal("incomplete round statistics")
		}
		assert("operational percent", &m.Statistics.OperationalCutoff, 50)
		assert("cutoff points", m.AngoffReview.Mix.CutoffPoints, 100)
		assert("modified", m.AngoffReview.Mix.ModifiedMean, .44608)
		assert("extended", m.AngoffReview.Mix.ExtendedMean, 2.6)
		assert("SD", m.AngoffReview.Mix.SDPoints, 3.520451676)
		assert("SEJ", m.StandardSettingError.SEJ, 1.574393852)
		assert("combined", m.StandardSettingError.SeSS, 6)
	}
	body, _, err := svc.ExportRoundWorkbooks(ctx, p.ID, sub.Code, true)
	if err != nil {
		t.Fatal(err)
	}
	files := openRoundWorkbooks(t, body)
	if len(files) != 2 {
		t.Fatalf("expected two workbooks, got %d", len(files))
	}
	for _, book := range files {
		if book.GetSheetList()[0] == "Summary" && (workbookCell(t, book, "B2") != "100" || workbookCell(t, book, "G2") != "50") {
			t.Fatal("wrong exported cutoff")
		}
	}
}
