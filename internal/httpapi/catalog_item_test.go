package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
)

type catalogMetadataStore struct {
	service.Store
	items []domain.Item
}

func (st *catalogMetadataStore) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (st *catalogMetadataStore) ListSubjects(context.Context) ([]domain.Subject, error) {
	return []domain.Subject{{Code: "MAT", Name: "Matematika"}}, nil
}
func (st *catalogMetadataStore) GetItem(_ context.Context, id string) (domain.Item, error) {
	for _, item := range st.items {
		if item.ID == id {
			return item, nil
		}
	}
	return domain.Item{}, mongo.ErrNoDocuments
}
func (st *catalogMetadataStore) UpsertItems(_ context.Context, items []domain.Item) error {
	st.items = items
	return nil
}
func (st *catalogMetadataStore) ListItems(context.Context, string, string, bool) ([]domain.Item, error) {
	return st.items, nil
}

func TestAdminItemMetadataRequiresKJAndRoundTripsNumericMeasure(t *testing.T) {
	for _, tc := range []struct {
		name, metadata string
		status         int
		wantKey        string
		wantMeasure    any
	}{
		{"negative", `"kj":" A,C ","measure":-1.25`, 200, "A,C", -1.25},
		{"zero", `"kj":"0","measure":0`, 200, "0", 0.0},
		{"absent measure", `"kj":"A"`, 200, "A", nil},
		{"missing key", `"measure":2`, 409, "", nil},
		{"blank key", `"kj":"  ","measure":-2`, 409, "", nil},
		{"non-numeric measure", `"kj":"A","measure":"abc"`, 422, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &catalogMetadataStore{}
			h := &Handler{service: service.New(st, nil, "")}
			app := fiber.New()
			app.Post("/items", h.saveItems)
			app.Get("/items", h.listItems)
			app.Get("/items/:id", h.itemDetail)
			body := `{"items":[{"id":"MAT-NEW","subjectCode":"MAT","subjectName":"Matematika","questionType":"pilihan_ganda","maxScore":1,` + tc.metadata + `}]}`
			request := httptest.NewRequest("POST", "/items", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("status %d, want %d", response.StatusCode, tc.status)
			}
			if tc.status != 200 {
				if len(st.items) != 0 {
					t.Fatal("invalid item was stored")
				}
				return
			}
			for _, path := range []string{"/items", "/items/MAT-NEW"} {
				res, _ := app.Test(httptest.NewRequest("GET", path, nil))
				var payload struct {
					Data json.RawMessage `json:"data"`
				}
				json.NewDecoder(res.Body).Decode(&payload)
				res.Body.Close()
				var item map[string]any
				if path == "/items" {
					var items []map[string]any
					json.Unmarshal(payload.Data, &items)
					if len(items) != 1 {
						t.Fatalf("bad list: %s", payload.Data)
					}
					item = items[0]
				} else {
					json.Unmarshal(payload.Data, &item)
				}
				if item["kj"] != tc.wantKey || item["measure"] != tc.wantMeasure {
					t.Fatalf("metadata lost on %s: %+v", path, item)
				}
			}
		})
	}
}
