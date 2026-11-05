package service

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/xuri/excelize/v2"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
)

type catalogImportMemory struct {
	Store
	subjects []domain.Subject
	items    map[string]domain.Item
}

func (st *catalogImportMemory) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (st *catalogImportMemory) ListSubjects(context.Context) ([]domain.Subject, error) {
	return st.subjects, nil
}
func (st *catalogImportMemory) GetItem(_ context.Context, id string) (domain.Item, error) {
	if item, ok := st.items[id]; ok {
		return item, nil
	}
	return domain.Item{}, mongo.ErrNoDocuments
}
func (st *catalogImportMemory) CreateSubject(_ context.Context, sub domain.Subject) error {
	st.subjects = append(st.subjects, sub)
	return nil
}
func (st *catalogImportMemory) InsertCatalogItems(_ context.Context, items []domain.Item) error {
	for _, item := range items {
		st.items[item.ID] = item
	}
	return nil
}

func catalogWorkbook(t *testing.T, kind string, rows ...[]any) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	f.SetSheetName("Sheet1", "Data")
	headers := []any{"kode_mapel", "mata_pelajaran"}
	if kind == "items" {
		headers = []any{"id_butir", "kode_mapel", "bentuk_soal", "skor_maksimum", "kj", "measure", "rubrik"}
	}
	f.SetSheetRow("Data", "A1", &headers)
	for n, row := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, n+2)
		f.SetSheetRow("Data", cell, &row)
	}
	return workbookBytes(t, f)
}

func TestCatalogWorkbookPreviewDoesNotWriteAndApplyOnlyAdds(t *testing.T) {
	st := &catalogImportMemory{subjects: []domain.Subject{{Code: "MAT", Name: "Matematika"}}, items: map[string]domain.Item{}}
	s := New(st, nil, "")
	ctx := context.Background()
	data := catalogWorkbook(t, "items", []any{"001", " mat ", "PG Kompleks", "3", "A,C", "", "skor nol\nskor satu"})
	preview, err := s.ImportCatalogWorkbook(ctx, "items", data, false)
	if err != nil || !preview.Valid || preview.Applied || preview.Count != 1 || len(st.items) != 0 {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	result, err := s.ImportCatalogWorkbook(ctx, "items", data, true)
	item := st.items["001"]
	if err != nil || !result.Applied || item.SubjectCode != "MAT" || item.SubjectName != "Matematika" || item.QuestionType != domain.QuestionTypeComplex || item.MaxScore != 3 || len(item.Rubric) != 2 || item.ProjectID != "" || item.UsageStatus != domain.ItemAvailable || item.CreatedAt.IsZero() {
		t.Fatalf("apply: %+v item %+v %v", result, item, err)
	}
	again, err := s.ImportCatalogWorkbook(ctx, "items", data, true)
	if err != nil || again.Valid || again.Applied || len(st.items) != 1 || again.Errors[0].Row != 2 {
		t.Fatalf("existing item overwritten: %+v %v", again, err)
	}
}

func TestCatalogWorkbookRejectsEveryInvalidRowWithoutPartialWrites(t *testing.T) {
	st := &catalogImportMemory{subjects: []domain.Subject{{Code: "MAT", Name: "Matematika"}}, items: map[string]domain.Item{}}
	s := New(st, nil, "")
	data := catalogWorkbook(t, "items",
		[]any{"NEW-001", "MAT", "Pilihan Ganda", "1", "A", "", ""},
		[]any{},
		[]any{"NEW-002", "MISSING", "essay", "0", "Kunci", "", ""},
		[]any{"NEW-001", "MAT", "tidak valid", "1.5", "A", "", ""},
		[]any{"NEW-003", "MAT", "pilihan_ganda", "2", "B", "", ""},
	)
	result, err := s.ImportCatalogWorkbook(context.Background(), "items", data, true)
	if err != nil || result.Valid || result.Applied || result.Count != 4 || len(st.items) != 0 {
		t.Fatalf("invalid import: %+v %v", result, err)
	}
	rows := map[int]int{}
	for _, issue := range result.Errors {
		rows[issue.Row]++
	}
	if rows[4] < 2 || rows[5] < 3 || rows[6] != 1 {
		t.Fatalf("Excel row numbers lost: %+v", result.Errors)
	}
}

func TestCatalogWorkbookSubjectsNormalizeAndRejectDuplicateCodeOrName(t *testing.T) {
	st := &catalogImportMemory{subjects: []domain.Subject{{Code: "MAT", Name: "Matematika"}}}
	s := New(st, nil, "")
	ctx := context.Background()
	valid := catalogWorkbook(t, "subjects", []any{" sej ", "  Sejarah   Dunia "})
	preview, err := s.ImportCatalogWorkbook(ctx, "subjects", valid, false)
	if err != nil || !preview.Valid || len(st.subjects) != 1 {
		t.Fatalf("preview wrote: %+v %v", preview, err)
	}
	result, err := s.ImportCatalogWorkbook(ctx, "subjects", valid, true)
	if err != nil || !result.Applied || len(st.subjects) != 2 || st.subjects[1].Code != "SEJ" || st.subjects[1].Name != "Sejarah Dunia" || st.subjects[1].NormalizedName != "sejarah dunia" {
		t.Fatalf("normalization: %+v %v", result, err)
	}
	invalid := catalogWorkbook(t, "subjects",
		[]any{"BIO", "Biologi"}, []any{"bio", "Biologi lain"},
		[]any{"XX", "biologi"}, []any{"mat", "Matematika Baru"},
		[]any{"NEW", "  sejarah  DUNIA "}, []any{"123", ""},
	)
	result, err = s.ImportCatalogWorkbook(ctx, "subjects", invalid, true)
	if err != nil || result.Valid || len(result.Errors) < 6 || len(st.subjects) != 2 {
		t.Fatalf("duplicate subjects accepted: %+v %v", result, err)
	}
}

func TestCatalogWorkbookRejectsEmptyMalformedFormulaAndUnexpectedHeaders(t *testing.T) {
	s := New(&catalogImportMemory{}, nil, "")
	for _, data := range [][]byte{nil, []byte("not Excel"), bytes.Repeat([]byte("x"), 5*1024*1024+1)} {
		if _, err := s.ImportCatalogWorkbook(context.Background(), "subjects", data, false); err == nil {
			t.Fatal("malformed workbook accepted")
		}
	}
	empty, err := s.ImportCatalogWorkbook(context.Background(), "subjects", catalogWorkbook(t, "subjects"), false)
	if err != nil || empty.Valid || len(empty.Errors) == 0 {
		t.Fatalf("empty workbook accepted: %+v %v", empty, err)
	}
	f, _ := excelize.OpenReader(bytes.NewReader(catalogWorkbook(t, "subjects", []any{"SEJ", "Sejarah"})))
	defer f.Close()
	f.SetCellFormula("Data", "B2", `"Sejarah"`)
	f.SetCellValue("Data", "C1", "tambahan")
	result, err := s.ImportCatalogWorkbook(context.Background(), "subjects", workbookBytes(t, f), true)
	if err != nil || result.Valid || len(result.Errors) < 2 {
		t.Fatalf("formula/header accepted: %+v %v", result, err)
	}
}

func TestCatalogWorkbookRejectsFormulaWithoutCachedValue(t *testing.T) {
	s := New(&catalogImportMemory{}, nil, "")
	f, _ := excelize.OpenReader(bytes.NewReader(catalogWorkbook(t, "subjects", []any{"SEJ", ""})))
	defer f.Close()
	f.SetCellFormula("Data", "B2", `"Sejarah"`)
	result, err := s.ImportCatalogWorkbook(context.Background(), "subjects", workbookBytes(t, f), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range result.Errors {
		if issue.Row == 2 && issue.Column == "mata_pelajaran" && strings.Contains(issue.Message, "formula") {
			return
		}
	}
	t.Fatalf("uncached formula not reported: %+v", result)
}

type catalogFailingStore struct{ Store }

func (st catalogFailingStore) CreateSubject(ctx context.Context, sub domain.Subject) error {
	if sub.Code == "GEO" {
		return errors.New("forced second-row failure")
	}
	return st.Store.CreateSubject(ctx, sub)
}

type catalogFailingItemsStore struct{ Store }

func (st catalogFailingItemsStore) InsertCatalogItems(ctx context.Context, items []domain.Item) error {
	inserter := st.Store.(interface {
		InsertCatalogItems(context.Context, []domain.Item) error
	})
	if err := inserter.InsertCatalogItems(ctx, items); err != nil {
		return err
	}
	return errors.New("forced failure before commit")
}

func TestCatalogWorkbookMongoRollsBackInsertedItemsOnFailure(t *testing.T) {
	ctx, _, st, _ := fixture(t, false)
	s := New(catalogFailingItemsStore{st}, nil, "")
	data := catalogWorkbook(t, "items", []any{"IMP-001", "MAT", "essay", "4", "Kunci", "", ""}, []any{"IMP-002", "MAT", "essay", "3", "Kunci", "", ""})
	result, err := s.ImportCatalogWorkbook(ctx, "items", data, true)
	if err == nil || result.Applied {
		t.Fatalf("write failure accepted: %+v %v", result, err)
	}
	for _, id := range []string{"IMP-001", "IMP-002"} {
		if _, err := st.GetItem(ctx, id); !errors.Is(err, mongo.ErrNoDocuments) {
			t.Fatalf("item survived failed transaction: %s %v", id, err)
		}
	}
}

func TestCatalogWorkbookMongoRollsBackLaterFailure(t *testing.T) {
	ctx, _, st, _ := fixture(t, false)
	s := New(catalogFailingStore{st}, nil, "")
	data := catalogWorkbook(t, "subjects", []any{"SEJ", "Sejarah"}, []any{"GEO", "Geografi"})
	result, err := s.ImportCatalogWorkbook(ctx, "subjects", data, true)
	if err == nil || result.Applied {
		t.Fatalf("write failure accepted: %+v %v", result, err)
	}
	subjects, _ := st.ListSubjects(ctx)
	for _, sub := range subjects {
		if sub.Code == "SEJ" || sub.Code == "GEO" {
			t.Fatal("transaction left partially imported subjects")
		}
	}
}

func TestCatalogWorkbookTemplateRoundTripAndStalePreview(t *testing.T) {
	ctx, s, st, _ := fixture(t, false)
	if _, err := s.CreateSubject(ctx, "Mata Impor", "IMP"); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"items", "subjects"} {
		data, err := s.CatalogWorkbookTemplate(ctx, kind)
		if err != nil {
			t.Fatal(err)
		}
		f, err := excelize.OpenReader(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		row := []any{"NEW", "Mata Baru"}
		if kind == "items" {
			row = []any{"IMP-001", "IMP", "essay", "4", "Kunci", "", ""}
		}
		f.SetSheetRow("Data", "A2", &row)
		filled := workbookBytes(t, f)
		f.Close()
		preview, err := s.ImportCatalogWorkbook(ctx, kind, filled, false)
		if err != nil || !preview.Valid || preview.Applied {
			t.Fatalf("template preview: %+v %v", preview, err)
		}
		result, err := s.ImportCatalogWorkbook(ctx, kind, filled, true)
		if err != nil || !result.Applied {
			t.Fatalf("template apply: %+v %v", result, err)
		}
		again, err := s.ImportCatalogWorkbook(ctx, kind, filled, true)
		if err != nil || again.Valid || again.Applied {
			t.Fatalf("stale preview applied: %+v %v", again, err)
		}
	}
	item, err := st.GetItem(ctx, "IMP-001")
	if err != nil || item.UsageStatus != domain.ItemAvailable || item.ProjectID != "" {
		t.Fatalf("imported item: %+v %v", item, err)
	}
}

func TestCatalogWorkbookConcurrentImportsCannotOverwriteOrLeavePartialItems(t *testing.T) {
	ctx, s, st, _ := fixture(t, false)
	files := [][]byte{
		catalogWorkbook(t, "items", []any{"BATCH-A", "MAT", "essay", "2", "Kunci", "", ""}, []any{"SHARED-ID", "MAT", "essay", "4", "Kunci", "", ""}),
		catalogWorkbook(t, "items", []any{"BATCH-B", "MAT", "essay", "3", "Kunci", "", ""}, []any{"SHARED-ID", "MAT", "essay", "5", "Kunci", "", ""}),
	}
	results := make([]CatalogWorkbookPreview, 2)
	start := make(chan struct{})
	var done sync.WaitGroup
	for i := range files {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			<-start
			results[i], _ = s.ImportCatalogWorkbook(ctx, "items", files[i], true)
		}(i)
	}
	close(start)
	done.Wait()
	winner := -1
	for i, result := range results {
		if result.Applied {
			if winner != -1 {
				t.Fatal("both conflicting files applied")
			}
			winner = i
		}
	}
	if winner == -1 {
		t.Fatalf("no import succeeded: %+v", results)
	}
	shared, err := st.GetItem(ctx, "SHARED-ID")
	if err != nil || shared.MaxScore != []int{4, 5}[winner] {
		t.Fatalf("winner overwritten: %+v %v", shared, err)
	}
	for i, id := range []string{"BATCH-A", "BATCH-B"} {
		_, err := st.GetItem(ctx, id)
		if (i == winner && err != nil) || (i != winner && !errors.Is(err, mongo.ErrNoDocuments)) {
			t.Fatalf("partial batch %s: winner %d, error %v", id, winner, err)
		}
	}
}
