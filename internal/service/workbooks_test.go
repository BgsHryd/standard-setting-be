package service

import (
	"bytes"
	"github.com/xuri/excelize/v2"
	"testing"
)

func TestWorkbookRoundtripAndAtomicBadRow(t *testing.T) {
	ctx, s, st, p := fixture(t, false)
	data, err := s.Template(ctx, p.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	book.SetCellValue("Data", "A2", "Matematika")
	book.SetCellValue("Data", "B2", "MAT-001")
	book.SetCellValue("Data", "A3", "Mapel tidak ada")
	book.SetCellValue("Data", "B3", "UNKNOWN")
	b, _ := book.WriteToBuffer()
	preview, err := s.ImportWorkbook(ctx, p.ID, 2, b.Bytes(), true)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Valid || len(preview.Errors) == 0 || preview.Errors[0].Row != 3 {
		t.Fatalf("missing original row errors: %+v", preview)
	}
	saved, _ := st.GetProject(ctx, p.ID)
	if saved.Version != p.Version || len(saved.Subjects[0].ItemIDs) != 3 {
		t.Fatal("bad workbook partially mutated project")
	}
	book.SetCellValue("Data", "A3", "")
	book.SetCellValue("Data", "B3", "")
	b, _ = book.WriteToBuffer()
	preview, err = s.ImportWorkbook(ctx, p.ID, 2, b.Bytes(), false)
	if err != nil || !preview.Valid {
		t.Fatalf("valid preview: %+v %v", preview, err)
	}
}
func TestNamesTemplateRejectsChangedRoleAndStaleVersion(t *testing.T) {
	ctx, s, _, p := fixture(t, false)
	data, err := s.Template(ctx, p.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	book, _ := excelize.OpenReader(bytes.NewReader(data))
	defer book.Close()
	book.SetCellValue("Data", "B2", "panelis")
	b, _ := book.WriteToBuffer()
	preview, err := s.ImportWorkbook(ctx, p.ID, 3, b.Bytes(), true)
	if err != nil || preview.Valid {
		t.Fatalf("changed role accepted %+v %v", preview, err)
	}
	if _, err = s.SaveProject(ctx, p, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ImportWorkbook(ctx, p.ID, 3, data, false); err == nil {
		t.Fatal("stale workbook accepted")
	}
}
func TestWorkbookRejectsFormulaAndExtraColumn(t *testing.T) {
	ctx, s, _, p := fixture(t, false)
	data, _ := s.Template(ctx, p.ID, 2)
	book, _ := excelize.OpenReader(bytes.NewReader(data))
	defer book.Close()
	book.SetCellFormula("Data", "B2", "1+1")
	book.SetCellValue("Data", "C1", "extra")
	b, _ := book.WriteToBuffer()
	preview, err := s.ImportWorkbook(ctx, p.ID, 2, b.Bytes(), false)
	if err == nil && preview.Valid {
		t.Fatal("formula/extra header accepted")
	}
}

func TestCustomUsernameWorkbookAtomicCollisionThenSuccess(t *testing.T) {
	ctx, s, st, p := fixture(t, false)
	p.Subjects[0].PanelistCount = 3
	p.Subjects[0].PanelistNames = []string{"A", "B", "New Custom"}
	if _, err := s.SaveProject(ctx, p, "admin"); err != nil {
		t.Fatal(err)
	}
	data, err := s.Template(ctx, p.ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	book, _ := excelize.OpenReader(bytes.NewReader(data))
	defer book.Close()
	book.SetCellValue("Data", "D5", "SS-QA.MAT.PIC01")
	b, _ := book.WriteToBuffer()
	before, _ := st.GetProject(ctx, p.ID)
	bad, err := s.ImportWorkbook(ctx, p.ID, 4, b.Bytes(), true)
	if err != nil || bad.Valid || len(bad.Errors) == 0 {
		t.Fatalf("username collision accepted: %+v %v", bad, err)
	}
	after, _ := st.GetProject(ctx, p.ID)
	users, _ := st.ListUsers(ctx, p.ID)
	if after.Version != before.Version || len(users) != 3 {
		t.Fatal("collision partially created accounts")
	}
	book.SetCellValue("Data", "D5", "custom.qa.new")
	b, _ = book.WriteToBuffer()
	good, err := s.ImportWorkbook(ctx, p.ID, 4, b.Bytes(), true)
	if err != nil || !good.Applied {
		t.Fatalf("valid custom username rejected: %+v %v", good, err)
	}
	user, err := st.FindByUsername(ctx, "custom.qa.new")
	if err != nil || user.Name != "New Custom" || !user.MustChangePassword {
		t.Fatal("custom account not created")
	}
}
func TestNameWorkbookAppliesNamesToSameIdentities(t *testing.T) {
	ctx, s, st, p := fixture(t, false)
	data, _ := s.Template(ctx, p.ID, 3)
	book, _ := excelize.OpenReader(bytes.NewReader(data))
	defer book.Close()
	book.SetCellValue("Data", "C3", "Renamed A")
	b, _ := book.WriteToBuffer()
	result, err := s.ImportWorkbook(ctx, p.ID, 3, b.Bytes(), true)
	if err != nil || !result.Applied {
		t.Fatalf("names import failed %+v %v", result, err)
	}
	user, _ := st.FindByID(ctx, "p1")
	if user.Name != "Renamed A" {
		t.Fatal("existing participant name not updated")
	}
}
