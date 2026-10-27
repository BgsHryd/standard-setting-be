package service

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/xuri/excelize/v2"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
	"standard-setting/backend/internal/domain"
)

type projectWorkbookStore struct {
	Store
	writes int
}

func (st *projectWorkbookStore) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (st *projectWorkbookStore) ListSubjects(context.Context) ([]domain.Subject, error) {
	return []domain.Subject{{Code: "MAT", Name: "Matematika"}}, nil
}
func (st *projectWorkbookStore) GetItem(_ context.Context, id string) (domain.Item, error) {
	if id == "MAT-001" {
		return domain.Item{ID: id, SubjectCode: "MAT", UsageStatus: domain.ItemAvailable}, nil
	}
	return domain.Item{}, mongo.ErrNoDocuments
}
func (st *projectWorkbookStore) FindByUsername(_ context.Context, username string) (domain.User, error) {
	if username == "existing.user" {
		return domain.User{ID: "existing"}, nil
	}
	return domain.User{}, mongo.ErrNoDocuments
}
func (st *projectWorkbookStore) SaveProject(context.Context, domain.Project) error {
	st.writes++
	return errors.New("unexpected write")
}

func filledProjectWorkbook(t *testing.T, s *Service) *excelize.File {
	t.Helper()
	data, err := s.ProjectWorkbookTemplate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	for sheet, rows := range map[string][][]any{
		"Proyek":   {{"Impor QA", "Deskripsi", "2026-10-26", "2026-10-26", "ceil_1pp", "false"}},
		"Mapel":    {{"MAT", "Kandidat mampu menyelesaikan soal dasar."}},
		"Butir":    {{"MAT", "MAT-001"}},
		"Personel": {{"MAT", "pic_mapel", "PIC baru", ""}, {"MAT", "panelis", "Panelis baru", "custom.panelis"}},
	} {
		for r, row := range rows {
			cell, _ := excelize.CoordinatesToCellName(1, r+2)
			if err := f.SetSheetRow(sheet, cell, &row); err != nil {
				t.Fatal(err)
			}
		}
	}
	return f
}
func workbookBytes(t *testing.T, f *excelize.File) []byte {
	t.Helper()
	b, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestProjectWorkbookPreviewValidatesWithoutWriting(t *testing.T) {
	st := &projectWorkbookStore{}
	s := New(st, nil, "12345*")
	f := filledProjectWorkbook(t, s)
	got, err := s.ImportProjectWorkbook(context.Background(), workbookBytes(t, f), false, "admin")
	if err != nil || !got.Valid || got.Applied || got.Project == nil {
		t.Fatalf("preview %+v, %v", got, err)
	}
	if got.Project.ID != "" || got.Project.Status != domain.ProjectDraft || len(got.Project.Subjects) != 1 || got.Project.Subjects[0].PanelistCount != 1 || st.writes != 0 {
		t.Fatalf("preview wrote or malformed: %+v", got)
	}
	if got.Project.EndDate.Sub(got.Project.StartDate).Hours() != 24 {
		t.Fatal("same-day period must cover the full Jakarta day")
	}
}
func TestProjectWorkbookReportsAllRowsAndRejectsApplyWithoutWrites(t *testing.T) {
	st := &projectWorkbookStore{}
	s := New(st, nil, "12345*")
	f := filledProjectWorkbook(t, s)
	f.SetCellValue("Butir", "B2", "missing-1")
	f.SetCellValue("Butir", "A3", "UNKNOWN")
	f.SetCellValue("Butir", "B3", "missing-2")
	f.SetCellValue("Personel", "D3", "existing.user")
	f.SetCellValue("Personel", "B4", "pic_mapel")
	f.SetCellValue("Personel", "A4", "MAT")
	f.SetCellValue("Personel", "C4", "PIC ganda")
	got, err := s.ImportProjectWorkbook(context.Background(), workbookBytes(t, f), true, "admin")
	if err != nil || got.Valid || got.Applied || len(got.Errors) < 4 || st.writes != 0 {
		t.Fatalf("invalid workbook partially applied: %+v %v", got, err)
	}
	sheets := map[string]bool{}
	for _, issue := range got.Errors {
		sheets[issue.Sheet] = true
	}
	if !sheets["Butir"] || !sheets["Personel"] {
		t.Fatalf("missing row provenance: %+v", got.Errors)
	}
}
func TestProjectWorkbookRejectsFormulaAndUnexpectedColumns(t *testing.T) {
	st := &projectWorkbookStore{}
	s := New(st, nil, "12345*")
	f := filledProjectWorkbook(t, s)
	f.SetCellFormula("Personel", "C3", "1+1")
	f.SetCellValue("Butir", "C2", "extra")
	got, err := s.ImportProjectWorkbook(context.Background(), workbookBytes(t, f), false, "admin")
	if err != nil || got.Valid || len(got.Errors) < 2 {
		t.Fatalf("formula/extra accepted %+v %v", got, err)
	}
}
func TestProjectWorkbookCreatesOnlyDraftAndAllocatesAccountsAtomically(t *testing.T) {
	ctx, s, st, old := fixture(t, false)
	f := filledProjectWorkbook(t, s)
	got, err := s.ImportProjectWorkbook(ctx, workbookBytes(t, f), true, "admin")
	if err != nil || !got.Applied || got.Project == nil {
		t.Fatalf("import failed %+v %v", got, err)
	}
	p := got.Project
	if p.ID == old.ID || p.Status != domain.ProjectDraft || !p.AccountsGenerated || p.WizardStep != 5 {
		t.Fatalf("wrong draft %+v", p)
	}
	users, err := st.ListUsers(ctx, p.ID)
	if err != nil || len(users) != 2 {
		t.Fatalf("wrong users %+v %v", users, err)
	}
	for _, u := range users {
		if !u.MustChangePassword || u.PasswordHash == "" || !u.Active {
			t.Fatal("account password policy missing")
		}
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("12345*")) != nil {
			t.Fatal("account does not use configured initial password")
		}
	}
	item, _ := st.GetItem(ctx, "MAT-001")
	if item.ProjectID != p.ID || item.UsageStatus != domain.ItemAllocated {
		t.Fatal("item not allocated")
	}
	rounds, _ := st.ListRounds(ctx, p.ID, "MAT")
	if len(rounds) != 0 {
		t.Fatal("draft activated rounds")
	}
	unchanged, _ := st.GetProject(ctx, old.ID)
	if unchanged.Version != old.Version || unchanged.Name != old.Name {
		t.Fatal("existing project modified")
	}
	again, err := s.ImportProjectWorkbook(ctx, workbookBytes(t, f), true, "admin")
	if err != nil || again.Valid || again.Applied {
		t.Fatalf("repeated import should reject allocated item %+v %v", again, err)
	}
	projects, _ := st.ListProjects(ctx)
	if len(projects) != 2 {
		t.Fatal("invalid second import left a partial project")
	}
}

type failingWorkbookAccounts struct{ Store }

func (st failingWorkbookAccounts) ReplaceProjectUsers(context.Context, string, []domain.User) error {
	return errors.New("forced account storage failure")
}

func TestProjectWorkbookAccountFailureRollsBackDraftAndItemAllocation(t *testing.T) {
	ctx, original, st, _ := fixture(t, false)
	s := New(failingWorkbookAccounts{st}, nil, "12345*")
	f := filledProjectWorkbook(t, original)
	result, err := s.ImportProjectWorkbook(ctx, workbookBytes(t, f), true, "admin")
	if err == nil || result.Applied || result.Project != nil {
		t.Fatalf("storage failure returned a created project: %+v %v", result, err)
	}
	projects, _ := st.ListProjects(ctx)
	item, _ := st.GetItem(ctx, "MAT-001")
	if len(projects) != 1 || item.ProjectID != "" || item.UsageStatus != domain.ItemAvailable {
		t.Fatal("account failure left a partial project or item allocation")
	}
}
