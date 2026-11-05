package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
	"golang.org/x/crypto/bcrypt"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/domain"
)

type workbookPasswordSessions struct{}

func (workbookPasswordSessions) Save(context.Context, string, string, time.Duration) error {
	return nil
}
func (workbookPasswordSessions) Consume(context.Context, string) (string, error) {
	return "", errors.New("unused")
}

func TestAccountWorkbookCustomPasswordAndExistingProtection(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	old, _ := st.FindByID(ctx, "p1")
	old.PasswordHash, _ = auth.HashPassword("kept-password")
	old.MustChangePassword = true
	old.AuthVersion = 4
	old.Active = false
	if err := st.SaveUser(ctx, old); err != nil {
		t.Fatal(err)
	}
	p.Subjects[0].PanelistCount = 3
	p.Subjects[0].PanelistNames = []string{"A", "B", "New Custom"}
	p.CollectContactPanelis = true
	if _, err := s.SaveProject(ctx, p, "admin"); err != nil {
		t.Fatal(err)
	}
	data, err := s.Template(ctx, p.ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	header, _ := book.GetCellValue("Data", "E1")
	if header != "password" {
		t.Fatal("step 4 password header missing")
	}
	for _, cell := range []string{"E2", "E3", "E4", "E5"} {
		v, _ := book.GetCellValue("Data", cell)
		if v != "" {
			t.Fatal("password prefilled")
		}
	}
	password := "Import-only-2026*"
	book.SetCellStr("Data", "D5", "custom.password.panel")
	book.SetCellStr("Data", "E5", password)
	b, _ := book.WriteToBuffer()
	preview, err := s.ImportWorkbook(ctx, p.ID, 4, b.Bytes(), false)
	if err != nil || !preview.Valid || preview.Applied {
		t.Fatal("custom password preview failed")
	}
	encoded, _ := json.Marshal(preview)
	if bytes.Contains(encoded, []byte(password)) {
		t.Fatal("password exposed in preview")
	}
	result, err := s.ImportWorkbook(ctx, p.ID, 4, b.Bytes(), true)
	if err != nil || !result.Applied {
		t.Fatal("custom password import failed", err)
	}
	user, err := st.FindByUsername(ctx, "custom.password.panel")
	if err != nil || user.MustChangePassword || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		t.Fatal("new account did not use custom password without forced change")
	}
	existing, _ := st.FindByID(ctx, "p1")
	if existing.PasswordHash != old.PasswordHash || existing.MustChangePassword != old.MustChangePassword || existing.AuthVersion != 4 || existing.Active {
		t.Fatal("import modified existing credentials")
	}
	login := auth.NewService(st, workbookPasswordSessions{}, "workbook-password-regression-secret", time.Hour, time.Hour)
	logged, err := login.Login(ctx, user.Username, password)
	if err != nil || logged.User.MustChangePassword {
		t.Fatal("custom password login requires password change", err)
	}
	if !logged.User.ContactRequired {
		t.Fatal("custom password login bypassed project contact requirement")
	}
	tokens, err := login.ParseAccessToken(logged.AccessToken)
	if err != nil || tokens.MustChangePassword {
		t.Fatal("login token incorrectly forces password change")
	}
	// Reusing the new configuration template must not become a password-reset endpoint.
	data, err = s.Template(ctx, p.ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := excelize.OpenReader(bytes.NewReader(data))
	defer next.Close()
	next.SetCellStr("Data", "E3", "do-not-overwrite*")
	b, _ = next.WriteToBuffer()
	bad, err := s.ImportWorkbook(ctx, p.ID, 4, b.Bytes(), true)
	if err != nil || bad.Valid || bad.Applied {
		t.Fatal("existing password overwrite accepted")
	}
	hasRow := false
	for _, issue := range bad.Errors {
		if issue.Row == 3 && issue.Column == "password" && issue.Value == "" {
			hasRow = true
		}
	}
	if !hasRow {
		t.Fatal("existing password error lacks redacted row")
	}
	existing, _ = st.FindByID(ctx, "p1")
	if existing.PasswordHash != old.PasswordHash {
		t.Fatal("failed import modified existing password")
	}
	// Rotating the process initial password and regenerating existing accounts preserves credentials.
	rotated := New(st, nil, "rotated-default-2026*")
	rows, _ := rotated.AccountRows(ctx, p.ID)
	inputs := []AccountInput{}
	for _, row := range rows {
		if row.Status != "removed" {
			inputs = append(inputs, AccountInput{SlotID: row.SlotID, Name: row.Name, Role: row.Role, SubjectCode: row.SubjectCode, Username: row.Username})
		}
	}
	if _, err = rotated.GenerateAccounts(ctx, p.ID, inputs); err != nil {
		t.Fatal(err)
	}
	same, _ := st.FindByID(ctx, user.ID)
	if same.PasswordHash != user.PasswordHash || same.MustChangePassword {
		t.Fatal("initial password rotation reset existing custom account")
	}
	if err = rotated.ResetPassword(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	reset, _ := st.FindByID(ctx, user.ID)
	if !reset.MustChangePassword || bcrypt.CompareHashAndPassword([]byte(reset.PasswordHash), []byte("rotated-default-2026*")) != nil {
		t.Fatal("manual reset did not use rotated initial password")
	}
}

func TestNewProjectAccountWorkbookPasswordsRequired(t *testing.T) {
	ctx, s, st, p := fixture(t, false)
	p.ID = ""
	p.Code = ""
	p.Version = 0
	p.AccountsGenerated = false
	p.Subjects[0].PICUserID = ""
	p.Subjects[0].PanelistUserIDs = nil
	p.Subjects[0].Slots = nil
	created, err := s.SaveProject(ctx, p, "admin")
	if err != nil {
		t.Fatal(err)
	}
	data, err := s.Template(ctx, created.ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	book, _ := excelize.OpenReader(bytes.NewReader(data))
	defer book.Close()
	passwords := []string{"秘密パスワード", "  Panel-private*  ", "012345"}
	for i, password := range passwords {
		cell, _ := excelize.CoordinatesToCellName(4, i+2)
		book.SetCellStr("Data", cell, []string{"fresh.pic", "fresh.panel.one", "fresh.panel.two"}[i])
		cell, _ = excelize.CoordinatesToCellName(5, i+2)
		book.SetCellStr("Data", cell, password)
	}
	b, _ := book.WriteToBuffer()
	result, err := s.ImportWorkbook(ctx, created.ID, 4, b.Bytes(), true)
	if err != nil || !result.Applied {
		t.Fatal("new project account import failed", err)
	}
	for i, name := range []string{"fresh.pic", "fresh.panel.one", "fresh.panel.two"} {
		user, err := st.FindByUsername(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(passwords[i])) != nil || user.MustChangePassword {
			t.Fatal("new project password or first-login policy incorrect")
		}
		if i == 0 && user.Role != domain.RolePICMapel {
			t.Fatal("PIC role changed")
		}
	}
}

func TestWorkbookPasswordCharacterAndByteLimits(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
		valid    bool
	}{
		{"six characters", "012345", true},
		{"six unicode characters", strings.Repeat("界", 6), true},
		{"fewer than six unicode characters", strings.Repeat("界", 5), false},
		{"72 ascii bytes", strings.Repeat("x", 72), true},
		{"73 ascii bytes", strings.Repeat("x", 73), false},
		{"72 unicode bytes", strings.Repeat("界", 24), true},
		{"75 unicode bytes", strings.Repeat("界", 25), false},
		{"unicode whitespace", strings.Repeat("\u2003", 6), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateWorkbookPassword(tc.password); (err == nil) != tc.valid {
				t.Fatal("unexpected password validity")
			}
		})
	}
}

func TestAccountWorkbookBadPasswordIsAtomicAndRedacted(t *testing.T) {
	for _, kind := range []string{"blank", "short", "long", "spaces", "formula"} {
		t.Run(kind, func(t *testing.T) {
			ctx, s, st, p := fixture(t, false)
			p.Subjects[0].PanelistCount = 4
			p.Subjects[0].PanelistNames = []string{"A", "B", "New One", "New Two"}
			if _, err := s.SaveProject(ctx, p, "admin"); err != nil {
				t.Fatal(err)
			}
			before, _ := st.GetProject(ctx, p.ID)
			data, err := s.Template(ctx, p.ID, 4)
			if err != nil {
				t.Fatal(err)
			}
			book, _ := excelize.OpenReader(bytes.NewReader(data))
			defer book.Close()
			book.SetCellStr("Data", "D5", "atomic.new.one")
			book.SetCellStr("Data", "D6", "atomic.new.two")
			book.SetCellStr("Data", "E5", "valid-secret*")
			secret := "short"
			switch kind {
			case "blank":
				secret = ""
			case "long":
				secret = strings.Repeat("s", 73)
			case "spaces":
				secret = "      "
			case "formula":
				secret = "formula-secret"
			}
			if kind == "formula" {
				book.SetCellFormula("Data", "E6", `"`+secret+`"`)
			} else {
				book.SetCellStr("Data", "E6", secret)
			}
			b, _ := book.WriteToBuffer()
			result, err := s.ImportWorkbook(ctx, p.ID, 4, b.Bytes(), true)
			if err != nil || result.Valid || result.Applied {
				t.Fatal("invalid password accepted", kind)
			}
			found := false
			for _, issue := range result.Errors {
				if issue.Row == 6 && issue.Column == "password" && issue.Value == "" {
					found = true
				}
			}
			if !found {
				t.Fatal("password error missing row/redaction", kind)
			}
			encoded, _ := json.Marshal(result)
			if bytes.Contains(encoded, []byte("valid-secret*")) || (kind != "spaces" && kind != "blank" && bytes.Contains(encoded, []byte(secret))) {
				t.Fatal("password leaked in validation output")
			}
			after, _ := st.GetProject(ctx, p.ID)
			users, _ := st.ListUsers(ctx, p.ID)
			if after.Version != before.Version || len(users) != 3 {
				t.Fatal("invalid password import partially mutated accounts")
			}
		})
	}
}
