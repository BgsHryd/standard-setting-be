package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
)

type workbookHTTPStore struct {
	service.Store
	project domain.Project
}

func (st *workbookHTTPStore) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (st *workbookHTTPStore) GetProject(context.Context, string) (domain.Project, error) {
	return st.project, nil
}
func (st *workbookHTTPStore) ListUsers(context.Context, string) ([]domain.User, error) {
	return nil, nil
}
func (st *workbookHTTPStore) ListItems(context.Context, string, string, bool) ([]domain.Item, error) {
	return nil, nil
}
func (st *workbookHTTPStore) ListRounds(context.Context, string, string) ([]domain.Round, error) {
	return nil, nil
}

func TestRoundWorkbookHTTPConfirmationAndZIP(t *testing.T) {
	st := &workbookHTTPStore{project: domain.Project{ID: "p", Code: "QA", Status: domain.ProjectActive, Subjects: []domain.ProjectSubject{{Code: "MAT", Name: "Matematika"}}}}
	h := &Handler{service: service.New(st, nil, "")}
	app := fiber.New()
	app.Get("/projects/:id/round-workbooks", h.roundWorkbooks)
	for _, query := range []string{"", "?confirmed=false"} {
		res, err := app.Test(httptest.NewRequest("GET", "/projects/p/round-workbooks"+query, nil))
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		json.NewDecoder(res.Body).Decode(&payload)
		res.Body.Close()
		if res.StatusCode != 409 || payload["code"] != "EXPORT_CONFIRMATION_REQUIRED" {
			t.Fatal("missing confirmation", res.StatusCode, payload)
		}
	}
	for _, query := range []string{"?confirmed=yes", "?confirmed=1", "?confirmed=true&subjectCode=UNKNOWN"} {
		res, err := app.Test(httptest.NewRequest("GET", "/projects/p/round-workbooks"+query, nil))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 422 {
			t.Fatal("invalid export query accepted", query, res.StatusCode)
		}
	}
	for _, completed := range []bool{false, true} {
		query := "?confirmed=true&subjectCode=all"
		if completed {
			st.project.Status = domain.ProjectCompleted
			query = "?subjectCode=MAT"
		}
		res, err := app.Test(httptest.NewRequest("GET", "/projects/p/round-workbooks"+query, nil), 5000)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/zip" || res.Header.Get("Cache-Control") != "no-store" || !strings.Contains(res.Header.Get("Content-Disposition"), ".zip") {
			t.Fatal("invalid download response", res.StatusCode, res.Header, string(body))
		}
		z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil || len(z.File) != 1 {
			t.Fatal("ZIP unreadable", err)
		}
	}
}
