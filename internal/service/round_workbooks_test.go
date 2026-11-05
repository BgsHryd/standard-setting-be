package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
	"standard-setting/backend/internal/domain"
)

type roundWorkbookStore struct {
	Store
	project       domain.Project
	users         []domain.User
	items         []domain.Item
	rounds        map[string][]domain.Round
	rows          map[string]map[int][]domain.Submission
	inTransaction bool
}

func (st *roundWorkbookStore) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	st.inTransaction = true
	defer func() { st.inTransaction = false }()
	return fn(ctx)
}
func (st *roundWorkbookStore) GetProject(context.Context, string) (domain.Project, error) {
	if !st.inTransaction {
		return domain.Project{}, errors.New("read outside transaction")
	}
	return st.project, nil
}
func (st *roundWorkbookStore) ListUsers(context.Context, string) ([]domain.User, error) {
	return st.users, nil
}
func (st *roundWorkbookStore) ListItems(_ context.Context, subject, _ string, _ bool) ([]domain.Item, error) {
	var items []domain.Item
	for _, item := range st.items {
		if item.SubjectCode == subject {
			items = append(items, item)
		}
	}
	return items, nil
}
func (st *roundWorkbookStore) ListRounds(_ context.Context, _, subject string) ([]domain.Round, error) {
	return st.rounds[subject], nil
}
func (st *roundWorkbookStore) ListSubmissions(_ context.Context, _, subject string, number int, _ string) ([]domain.Submission, error) {
	return st.rows[subject][number], nil
}

func roundWorkbookFixture() (*Service, *roundWorkbookStore) {
	sub := domain.ProjectSubject{Code: "MAT", Name: "Matematika", CandidateVersion: 2, ItemIDs: []string{"001", "ESSAY"}, PanelistUserIDs: []string{"a", "b", "c"}}
	st := &roundWorkbookStore{
		project: domain.Project{ID: "p", Code: "SS-QA", Name: "Proyek ekspor", Status: domain.ProjectActive, FeatureVersion: 1, RoundingPolicy: "none", Subjects: []domain.ProjectSubject{sub, {Code: "BIN", Name: "Bahasa Indonesia"}}},
		users: []domain.User{
			{ID: "a", Name: "Nama diubah", Active: false, Role: domain.RolePanelist, ProjectID: "p", SubjectCode: "MAT"},
			{ID: "b", Name: "Panelis B", Active: true, Role: domain.RolePanelist, ProjectID: "p", SubjectCode: "MAT"},
			{ID: "c", Name: "Panelis C", Active: true, Role: domain.RolePanelist, ProjectID: "p", SubjectCode: "MAT"},
		},
		items:  []domain.Item{{ID: "001", SubjectCode: "MAT", QuestionType: domain.QuestionTypeEssay, MaxScore: 4}, {ID: "ESSAY", SubjectCode: "MAT", QuestionType: domain.QuestionTypeEssay, MaxScore: 4}},
		rounds: map[string][]domain.Round{}, rows: map[string]map[int][]domain.Submission{},
	}
	rows := []domain.Submission{
		{ItemID: "001", PanelistID: "a", PanelistName: "Nama saat dikunci", Status: "saved", RoundNumber: 1, RoundRevision: 1, ReviewRevision: 1, CandidateVersion: 1, Answer: domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(50)}},
		{ItemID: "001", PanelistID: "b", PanelistName: "Panelis B", Status: "saved", RoundNumber: 1, RoundRevision: 1, ReviewRevision: 1, CandidateVersion: 1, Answer: domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(70)}},
		{ItemID: "ESSAY", PanelistID: "a", Status: "saved", RoundNumber: 1, RoundRevision: 1, ReviewRevision: 1, CandidateVersion: 1, Answer: domain.Answer{QuestionType: domain.QuestionTypeEssay, Distribution: []int{0, 100, 0}}},
		{ItemID: "ESSAY", PanelistID: "b", Status: "saved", RoundNumber: 1, RoundRevision: 1, ReviewRevision: 1, CandidateVersion: 1, Answer: domain.Answer{QuestionType: domain.QuestionTypeEssay, Distribution: []int{0, 0, 100}}},
	}
	snapshot := &domain.RoundSnapshot{ID: "snapshot-1", RoundNumber: 1, Revision: 1, CandidateVersion: 1, PanelistIDs: []string{"a", "b"}, Roster: []domain.SnapshotPanelist{{PanelistID: "a", PanelistName: "Nama saat dikunci", Included: true}, {PanelistID: "b", PanelistName: "Panelis B", Included: true}}, Submissions: rows, Statistics: domain.ComputeStatistics([]float64{50, 90}, "none"), ItemStatistics: map[string]domain.ItemResult{"001": domain.ComputeStatistics([]float64{50, 70}, "none"), "ESSAY": domain.ComputeStatistics([]float64{50, 100}, "none")}}
	st.rounds["MAT"] = []domain.Round{{Number: 1, Revision: 1, Status: "finalized", FinalSnapshot: snapshot}, {Number: 2, Revision: 1, Status: "active"}}
	st.rows["MAT"] = map[int][]domain.Submission{1: {{ItemID: "001", PanelistID: "a", Status: "saved", Answer: domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(99)}}}, 2: {{ItemID: "001", PanelistID: "b", Status: "saved", RoundNumber: 2, RoundRevision: 1, ReviewRevision: 1, CandidateVersion: 2, Answer: domain.Answer{QuestionType: domain.QuestionTypeEssay, Distribution: []int{0, 0, 100, 0, 0}}}}}
	return New(st, nil, ""), st
}

func openRoundWorkbooks(t *testing.T, data []byte) map[string]*excelize.File {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*excelize.File{}
	for _, entry := range z.File {
		if strings.Contains(entry.Name, "..") || strings.HasPrefix(entry.Name, "/") || !strings.HasSuffix(entry.Name, ".xlsx") {
			t.Fatalf("unsafe archive member: %q", entry.Name)
		}
		r, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		f, err := excelize.OpenReader(bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		if len(f.GetSheetList()) != 1 {
			t.Fatal("each Excel must contain exactly one sheet", entry.Name)
		}
		files[entry.Name] = f
	}
	return files
}

func workbookCell(t *testing.T, f *excelize.File, cell string) string {
	t.Helper()
	if f == nil {
		t.Fatal("missing workbook")
	}
	v, err := f.GetCellValue(f.GetSheetList()[0], cell, excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRoundWorkbooksRequireConfirmationUntilProjectCompleted(t *testing.T) {
	s, st := roundWorkbookFixture()
	for _, status := range []domain.ProjectStatus{domain.ProjectDraft, domain.ProjectActive} {
		st.project.Status = status
		if _, _, err := s.ExportRoundWorkbooks(context.Background(), "p", "all", false); !errors.Is(err, ErrExportConfirmationRequired) {
			t.Fatalf("unfinished export bypassed confirmation: %v", err)
		}
	}
	st.project.Status = domain.ProjectCompleted
	if _, _, err := s.ExportRoundWorkbooks(context.Background(), "p", "MAT", false); err != nil {
		t.Fatal(err)
	}
}

func TestRoundWorkbooksSeparateSubjectsAndUseFrozenResponses(t *testing.T) {
	s, _ := roundWorkbookFixture()
	data, name, err := s.ExportRoundWorkbooks(context.Background(), "p", "all", true)
	if err != nil || !strings.HasSuffix(name, ".zip") {
		t.Fatal(name, err)
	}
	files := openRoundWorkbooks(t, data)
	if out := os.Getenv("QA_ROUND_WORKBOOK_OUTPUT"); out != "" {
		if err := os.MkdirAll(out, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "synthetic-round-workbooks.zip"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if len(files) != 4 {
		t.Fatalf("want summary + 2 rounds for MAT, summary for BIN, got %v", files)
	}
	summary := files["01-MAT/rekap_SS-QAMAT"+time.Now().In(time.FixedZone("Asia/Jakarta", 7*3600)).Format("20060102")+".xlsx"]
	if workbookCell(t, summary, "A9") != "Putaran" || workbookCell(t, summary, "B9") != "Cut-off test" || workbookCell(t, summary, "D9") != "ICC" || workbookCell(t, summary, "A10") != "Putaran 1" || workbookCell(t, summary, "B10") != "70" {
		t.Fatal("incorrect vertical summary")
	}
	sd, _ := strconv.ParseFloat(workbookCell(t, summary, "C10"), 64)
	if math.Abs(sd-math.Sqrt(800)) > 1e-10 || workbookCell(t, summary, "D10") != "0" {
		t.Fatal("sample SD or valid zero ICC lost", sd, workbookCell(t, summary, "D10"))
	}
	if workbookCell(t, summary, "B11") != "Belum lengkap" {
		t.Fatal("partial live round presented as complete")
	}
	frozen := files["01-MAT/putaran-1.xlsx"]
	if workbookCell(t, frozen, "A10") != "001" || workbookCell(t, frozen, "B9") != "Panelis 1 - Nama saat dikunci" || workbookCell(t, frozen, "B10") != "50" || workbookCell(t, frozen, "C11") != "100" {
		t.Fatal("snapshot response/name/maximum changed with live data")
	}
	if !strings.Contains(workbookCell(t, frozen, "D10"), "Prioritaskan diskusi") {
		t.Fatal("missing frozen guidance")
	}
	live := files["01-MAT/putaran-2.xlsx"]
	if workbookCell(t, live, "B10") != "50" || workbookCell(t, live, "C10") != "" || workbookCell(t, live, "B11") != "" {
		t.Fatal("missing live responses became zero or stale roster counted")
	}
	if workbookCell(t, live, "D10") != "Menunggu putaran dikunci." {
		t.Fatal("active round has final guidance")
	}
	if workbookCell(t, files["02-BIN/rekap_SS-QABIN"+time.Now().In(time.FixedZone("Asia/Jakarta", 7*3600)).Format("20060102")+".xlsx"], "A10") != "Belum ada putaran" {
		t.Fatal("unstarted subject omitted")
	}
}

func TestRoundWorkbooksFilterSubjectAndKeepSpreadsheetTextSafe(t *testing.T) {
	s, st := roundWorkbookFixture()
	st.project.Subjects[0].Code = "../../MAT"
	st.items[0].SubjectCode = "../../MAT"
	st.items[1].SubjectCode = "../../MAT"
	st.rounds["../../MAT"] = st.rounds["MAT"]
	st.rows["../../MAT"] = st.rows["MAT"]
	for i := range st.users {
		st.users[i].SubjectCode = "../../MAT"
	}
	st.rounds["../../MAT"][0].FinalSnapshot.Roster[0].PanelistName = "=HYPERLINK(\"unsafe\")"
	data, _, err := s.ExportRoundWorkbooks(context.Background(), "p", "../../MAT", true)
	if err != nil {
		t.Fatal(err)
	}
	files := openRoundWorkbooks(t, data)
	if len(files) != 3 {
		t.Fatal("subject filter exported other subjects")
	}
	for name, f := range files {
		if strings.HasSuffix(name, "putaran-1.xlsx") {
			if formula, _ := f.GetCellFormula("Respons", "B9"); formula != "" {
				t.Fatal("name became executable formula")
			}
		}
	}
	if _, _, err = s.ExportRoundWorkbooks(context.Background(), "p", "UNKNOWN", true); err == nil {
		t.Fatal("unknown subject accepted")
	}
}

func TestRawResponseCSVRemovesTechnicalColumnsWithoutLosingAnswers(t *testing.T) {
	s, _ := roundWorkbookFixture()
	data, err := s.ExportReport(context.Background(), "p", 1, "standard", "raw")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))).ReadAll()
	if err != nil || len(rows) != 5 {
		t.Fatal("response rows missing", err, len(rows))
	}
	for _, removed := range []string{"snapshotId", "legacy", "historical", "activeAtSnapshot", "removedAtSnapshot", "status"} {
		for _, header := range rows[0] {
			if header == removed {
				t.Fatal("technical column still exported", removed)
			}
		}
	}
	columns := map[string]int{}
	for i, name := range rows[0] {
		columns[name] = i
	}
	seenPG, seenDistribution := false, false
	for _, row := range rows[1:] {
		if len(row) != len(rows[0]) {
			t.Fatal("header and row widths differ")
		}
		if row[columns["itemId"]] == "'001" && row[columns["panelistId"]] == "a" {
			seenPG = row[columns["value"]] == "50"
		}
		if row[columns["itemId"]] == "ESSAY" && row[columns["panelistId"]] == "b" {
			seenDistribution = row[columns["distributionJSON"]] == "[0,0,100]"
		}
	}
	if !seenPG || !seenDistribution {
		t.Fatal("raw responses were changed while removing columns")
	}
}

func TestRoundWorkbooksActiveWeightedScoresAndExclusions(t *testing.T) {
	s, st := roundWorkbookFixture()
	st.items[0].QuestionType = domain.QuestionTypePG
	st.items[0].MaxScore = 1
	response := func(id, item string, answer domain.Answer) domain.Submission {
		return domain.Submission{PanelistID: id, ItemID: item, Status: "saved", RoundNumber: 2, RoundRevision: 1, ReviewRevision: 1, CandidateVersion: 2, Answer: answer}
	}
	st.rows["MAT"][2] = []domain.Submission{
		response("b", "001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(20)}),
		response("c", "001", domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(40)}),
		response("b", "ESSAY", domain.Answer{QuestionType: domain.QuestionTypeEssay, Distribution: []int{0, 0, 0, 0, 100}}),
		response("c", "ESSAY", domain.Answer{QuestionType: domain.QuestionTypeEssay, Distribution: []int{0, 0, 100, 0, 0}}),
	}
	data, _, err := s.ExportRoundWorkbooks(context.Background(), "p", "MAT", true)
	if err != nil {
		t.Fatal(err)
	}
	files := openRoundWorkbooks(t, data)
	for name, f := range files {
		if strings.Contains(name, "rekap_") {
			if workbookCell(t, f, "B11") != "66" {
				t.Fatal("active cutoff is not the mean of weighted panelist totals", workbookCell(t, f, "B11"))
			}
		}
	}
	st.rounds["MAT"][1].ExcludedPanelists = []domain.RoundExclusion{{PanelistID: "c"}}
	data, _, err = s.ExportRoundWorkbooks(context.Background(), "p", "MAT", true)
	if err != nil {
		t.Fatal(err)
	}
	for name, f := range openRoundWorkbooks(t, data) {
		if strings.Contains(name, "rekap_") && (workbookCell(t, f, "B11") != "84" || workbookCell(t, f, "C11") != "Minimal 2 panelis") {
			t.Fatal("excluded panelist counted or single panelist SD invented")
		}
	}
	// Saved but stale candidate/revision and invalid responses stay absent.
	for _, mutate := range []func(*domain.Submission){
		func(row *domain.Submission) { row.CandidateVersion = 1 },
		func(row *domain.Submission) { row.RoundRevision = 99 },
		func(row *domain.Submission) { row.Answer.Value = intValue(101) },
	} {
		old := st.rows["MAT"][2][0]
		mutate(&st.rows["MAT"][2][0])
		data, _, err = s.ExportRoundWorkbooks(context.Background(), "p", "MAT", true)
		if err != nil {
			t.Fatal(err)
		}
		if workbookCell(t, openRoundWorkbooks(t, data)["01-MAT/putaran-2.xlsx"], "B10") != "" {
			t.Fatal("stale or invalid response exported as current")
		}
		st.rows["MAT"][2][0] = old
	}
}

func TestRoundWorkbooksMongoSnapshotReadIsImmutable(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1); err != nil {
		t.Fatal(err)
	}
	before, _, err := s.ExportRoundWorkbooks(ctx, p.ID, "MAT", true)
	if err != nil {
		t.Fatal(err)
	}
	old := openRoundWorkbooks(t, before)["01-MAT/putaran-1.xlsx"]
	if workbookCell(t, old, "B10") != "60" || workbookCell(t, old, "C10") != "80" {
		t.Fatal("frozen responses not exported")
	}
	u, err := st.FindByID(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	u.Name = "Changed after lock"
	u.Active = false
	if err := st.SaveUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	item, err := st.GetItem(ctx, "MAT-003")
	if err != nil {
		t.Fatal(err)
	}
	item.MaxScore = 2
	if err := st.UpsertItems(ctx, []domain.Item{item}); err != nil {
		t.Fatal(err)
	}
	after, _, err := s.ExportRoundWorkbooks(ctx, p.ID, "MAT", true)
	if err != nil {
		t.Fatal(err)
	}
	fresh := openRoundWorkbooks(t, after)["01-MAT/putaran-1.xlsx"]
	if workbookCell(t, fresh, "B9") != workbookCell(t, old, "B9") || workbookCell(t, fresh, "B12") != "50" {
		t.Fatal("live roster/maximum changed frozen response workbook")
	}
}

func TestRoundWorkbooksFrozenShapeIgnoresExcludedAndStaleResponses(t *testing.T) {
	for _, kind := range []string{"excluded", "old candidate", "future revision"} {
		t.Run(kind, func(t *testing.T) {
			s, st := roundWorkbookFixture()
			old := domain.Submission{PanelistID: "a", ItemID: "ESSAY", Status: "saved", RoundNumber: 1, RoundRevision: 1, ReviewRevision: 1, CandidateVersion: 1, Answer: domain.Answer{QuestionType: domain.QuestionTypeEssay, Distribution: []int{0, 0, 0, 0, 100}}}
			switch kind {
			case "excluded":
				old.PanelistID = "c"
			case "old candidate":
				old.CandidateVersion = 0
			case "future revision":
				old.RoundRevision = 99
				old.ReviewRevision = 99
			}
			snap := st.rounds["MAT"][0].FinalSnapshot
			snap.Submissions = append(snap.Submissions, old)
			data, _, err := s.ExportRoundWorkbooks(context.Background(), "p", "MAT", true)
			if err != nil {
				t.Fatal(err)
			}
			files := openRoundWorkbooks(t, data)
			f := files["01-MAT/putaran-1.xlsx"]
			if workbookCell(t, f, "B11") != "50" || workbookCell(t, f, "C11") != "100" {
				t.Fatal("unrelated response changed frozen item shape")
			}
		})
	}
}
