package httpapi

import (
	"context"
	"github.com/gofiber/fiber/v2"
	"net/http/httptest"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
	"strings"
	"testing"
)

type subjectInputStore struct {
	service.Store
	created domain.Subject
}

func (st *subjectInputStore) CreateSubject(_ context.Context, subject domain.Subject) error {
	st.created = subject
	return nil
}
func TestCreateSubjectAcceptsManualCodeAndValidatesPayload(t *testing.T) {
	for _, tc := range []struct {
		name, body, code string
		status           int
	}{
		{"custom", `{"name":"Sejarah","kode_mapel":"sej"}`, "SEJ", 200},
		{"alias", `{"name":"Geografi","code":"geo"}`, "GEO", 200},
		{"missing", `{"name":"Sejarah"}`, "", 422},
		{"invalid", `{"name":"Sejarah","kode_mapel":"12 !"}`, "", 422},
		{"conflicting aliases", `{"name":"Sejarah","kode_mapel":"SEJ","code":"IPA"}`, "", 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &subjectInputStore{}
			h := Handler{service: service.New(st, nil, "")}
			app := fiber.New()
			app.Post("/subjects", h.createSubject)
			r := httptest.NewRequest("POST", "/subjects", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			res, err := app.Test(r)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.status || st.created.Code != tc.code {
				t.Fatalf("status%d code%q", res.StatusCode, st.created.Code)
			}
		})
	}
}
