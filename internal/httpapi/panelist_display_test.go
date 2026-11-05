package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
)

func TestPanelistDisplayRouteRequiresPIC(t *testing.T) {
	for _, role := range []domain.Role{domain.RolePICMapel, domain.RolePanelist, domain.RoleAdminSuper} {
		app, _, session := roleApp(t, role, false)
		request := httptest.NewRequest("PUT", "/api/v1/pic/panelist-display", strings.NewReader(`{"showKJ":true,"showMeasure":false,"projectVersion":0}`))
		request.Header.Set("Content-Type", "application/json")
		attachTestSession(request, session)
		response, err := app.Test(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		want := 403
		if role == domain.RolePICMapel {
			want = 503 // Reaches the own-role handler; this fixture has no business service.
		}
		if response.StatusCode != want {
			t.Fatalf("role %s: got %d, want %d", role, response.StatusCode, want)
		}
	}
}

func TestPanelistDisplayRequiresBothBooleansAndVersion(t *testing.T) {
	h := &Handler{service: service.New(nil, nil, "")}
	app := fiber.New()
	app.Put("/settings", h.setPanelistDisplay)
	for _, body := range []string{
		`{}`, `{"showKJ":true,"projectVersion":0}`, `{"showKJ":false,"showMeasure":false}`,
		`{"showKJ":"true","showMeasure":false,"projectVersion":0}`,
		`{"showKJ":true,"showMeasure":null,"projectVersion":0}`,
		`{"showKJ":true,"showMeasure":false,"projectVersion":-1}`,
	} {
		request := httptest.NewRequest("PUT", "/settings", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response, err := app.Test(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 422 {
			t.Fatal("missing or invalid configuration accepted")
		}
	}
}

func TestPanelistFormMetadataRespectsEachVisibilityToggle(t *testing.T) {
	zero := 0.0
	negative := -1.25
	for _, kj := range []bool{false, true} {
		for _, measure := range []bool{false, true} {
			form := service.PanelistForm{
				Subject: domain.ProjectSubject{ShowKJ: kj, ShowMeasure: measure},
				Items: []domain.Item{
					{ID: "one", KJ: "A,C", Measure: &zero},
					{ID: "two", KJ: "essay key", Measure: &negative},
					{ID: "legacy"},
				},
			}
			encoded, err := json.Marshal(panelistFormView(form))
			if err != nil {
				t.Fatal(err)
			}
			var view struct {
				Items []map[string]any `json:"items"`
			}
			if err := json.Unmarshal(encoded, &view); err != nil || len(view.Items) != 3 {
				t.Fatal("bad panelist form payload", err)
			}
			for i, item := range view.Items {
				key, hasKey := item["kj"]
				value, hasMeasure := item["measure"]
				if hasKey != kj || hasMeasure != (measure && i < 2) {
					t.Fatalf("metadata leaked or hidden incorrectly: key=%t measure=%t item=%d", kj, measure, i)
				}
				if kj && key != form.Items[i].KJ {
					t.Fatal("key was changed")
				}
				if measure && i < 2 && value != *form.Items[i].Measure {
					t.Fatal("zero or negative measure was changed")
				}
			}
		}
	}
}
