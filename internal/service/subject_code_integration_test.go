package service

import (
	"bytes"
	"github.com/xuri/excelize/v2"
	"standard-setting/backend/internal/domain"
	"strings"
	"testing"
)

func TestSubjectCustomCodeIsNormalizedUniqueAndRequired(t *testing.T) {
	ctx, s, st, _ := fixture(t, false)
	created, err := s.CreateSubject(ctx, "  Sejarah  Dunia ", " sej ")
	if err != nil || created.Code != "SEJ" || created.Name != "Sejarah Dunia" {
		t.Fatalf("custom code: %+v %v", created, err)
	}
	if _, err = s.CreateSubject(ctx, "Sejarah Lain", "sej"); err == nil {
		t.Fatal("duplicate case-insensitive code accepted")
	}
	if _, err = s.CreateSubject(ctx, "sejarah dunia", "SEJ2"); err == nil {
		t.Fatal("duplicate name accepted")
	}
	before, _ := st.ListSubjects(ctx)
	for _, code := range []string{"", "A", "123", "SEJ AR", "SEJ!", "ABCDEFGHIJKLMNOPQRSTUVWXYZ"} {
		if _, err = s.CreateSubject(ctx, "Tidak Disimpan", code); err == nil {
			t.Fatalf("invalid code accepted: %q", code)
		}
	}
	after, _ := st.ListSubjects(ctx)
	if len(before) != len(after) {
		t.Fatal("invalid input wrote a catalog record")
	}
}
func TestWorkbookItemsAcceptsCustomSubjectCodeOrName(t *testing.T) {
	for _, mapel := range []string{"SEJ", "sej", "Sejarah Dunia"} {
		t.Run(mapel, func(t *testing.T) {
			ctx, s, st, p := fixture(t, false)
			if _, err := s.CreateSubject(ctx, "Sejarah Dunia", "SEJ"); err != nil {
				t.Fatal(err)
			}
			if err := st.UpsertItems(ctx, []domain.Item{{ID: "SEJ-001", SubjectCode: "SEJ", SubjectName: "Sejarah Dunia", QuestionType: domain.QuestionTypePG, MaxScore: 1, UsageStatus: domain.ItemAvailable}}); err != nil {
				t.Fatal(err)
			}
			data, err := s.Template(ctx, p.ID, 2)
			if err != nil {
				t.Fatal(err)
			}
			f, err := excelize.OpenReader(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			rows, _ := f.GetRows("Data")
			for n := len(rows); n >= 2; n-- {
				f.RemoveRow("Data", n)
			}
			f.SetCellStr("Data", "A2", mapel)
			f.SetCellStr("Data", "B2", "SEJ-001")
			buf, err := f.WriteToBuffer()
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			preview, err := s.ImportWorkbook(ctx, p.ID, 2, buf.Bytes(), false)
			if err != nil || !preview.Valid || preview.Count != 1 {
				t.Fatalf("custom identifier rejected: %+v %v", preview, err)
			}
			if _, err = s.ImportWorkbook(ctx, p.ID, 2, buf.Bytes(), true); err != nil {
				t.Fatal(err)
			}
			got, _ := st.GetProject(ctx, p.ID)
			found := false
			for _, sub := range got.Subjects {
				if sub.Code == "SEJ" && len(sub.ItemIDs) == 1 && sub.ItemIDs[0] == "SEJ-001" {
					found = true
				}
			}
			if !found {
				t.Fatalf("wrong subject allocation: %+v", got.Subjects)
			}
		})
	}
}
func TestWorkbookRejectsAmbiguousSubjectNameAndCode(t *testing.T) {
	ctx, s, _, p := fixture(t, false)
	if _, err := s.CreateSubject(ctx, "Mata Baru", "XX"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSubject(ctx, "XX", "YY"); err != nil {
		t.Fatal(err)
	}
	data, err := s.Template(ctx, p.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := excelize.OpenReader(bytes.NewReader(data))
	rows, _ := f.GetRows("Data")
	for n := len(rows); n >= 2; n-- {
		f.RemoveRow("Data", n)
	}
	f.SetCellStr("Data", "A2", "XX")
	f.SetCellStr("Data", "B2", "MAT-001")
	buf, _ := f.WriteToBuffer()
	f.Close()
	preview, err := s.ImportWorkbook(ctx, p.ID, 2, buf.Bytes(), false)
	if err != nil {
		t.Fatal(err)
	}
	ambiguous := false
	for _, issue := range preview.Errors {
		if strings.Contains(issue.Message, "ambigu") {
			ambiguous = true
		}
	}
	if preview.Valid || !ambiguous {
		t.Fatalf("ambiguous subject accepted: %+v", preview)
	}
}
