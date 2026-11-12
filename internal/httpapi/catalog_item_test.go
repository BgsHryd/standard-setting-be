package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
)

type catalogMetadataStore struct {
	service.Store
	items    []domain.Item
	projects []domain.Project
	touched  []string
}

func (st *catalogMetadataStore) ListProjects(context.Context) ([]domain.Project, error) {
	return st.projects, nil
}
func (st *catalogMetadataStore) TouchProject(_ context.Context, id string) error {
	st.touched = append(st.touched, id)
	return nil
}

func (st *catalogMetadataStore) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (st *catalogMetadataStore) ListSubjects(context.Context) ([]domain.Subject, error) {
	return []domain.Subject{{Code: "MAT", Name: "Matematika"}, {Code: "BIN", Name: "Bahasa Indonesia"}}, nil
}

func TestAllocatedItemMetadataEditableWithoutChangingAllocation(t *testing.T) {
	now := time.Now().UTC().Add(-time.Hour)
	old := domain.Item{ID: "MAT-001", SubjectCode: "MAT", SubjectName: "Matematika", QuestionType: domain.QuestionTypePG, MaxScore: 1, KJ: "A", UsageStatus: domain.ItemAllocated, ProjectID: "p1", ProjectName: "Proyek pertama", ProjectHistory: []domain.ItemProjectUsage{{ProjectID: "p1"}, {ProjectID: "p2"}}, CreatedAt: now, FinalResult: &domain.ItemResult{Mean: 65}}
	st := &catalogMetadataStore{items: []domain.Item{old}, projects: []domain.Project{
		{ID: "p1", Subjects: []domain.ProjectSubject{{Code: "MAT", ItemIDs: []string{old.ID}}}},
		{ID: "p2", Subjects: []domain.ProjectSubject{{Code: "MAT", ItemIDs: []string{old.ID}}}},
		{ID: "other", Subjects: []domain.ProjectSubject{{Code: "MAT", ItemIDs: []string{"OTHER"}}}},
	}}
	h := &Handler{service: service.New(st, nil, "")}
	app := fiber.New()
	app.Post("/items", h.saveItems)
	body := `{"items":[{"id":"MAT-001","subjectCode":"MAT","subjectName":"Matematika","questionType":"pilihan_ganda_kompleks_bs","maxScore":3,"optionCount":3,"kj":"BSB","measure":-1.25,"measureCategory":"Sukar","rubric":["Rubrik baru"],"usageStatus":"available","projectId":"forged"}]}`
	req := httptest.NewRequest("POST", "/items", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("allocated metadata rejected: status %d", res.StatusCode)
	}
	got := st.items[0]
	if got.QuestionType != domain.QuestionTypeComplexBS || got.MaxScore != 3 || got.KJ != "BSB" || got.Measure == nil || *got.Measure != -1.25 || got.MeasureCategory != "Sukar" || len(got.Rubric) != 1 || got.Rubric[0] != "Rubrik baru" {
		t.Fatal("metadata not saved", got)
	}
	if got.ID != old.ID || got.SubjectCode != old.SubjectCode || got.UsageStatus != old.UsageStatus || got.ProjectID != old.ProjectID || got.ProjectName != old.ProjectName || !reflect.DeepEqual(got.ProjectHistory, old.ProjectHistory) || !reflect.DeepEqual(got.FinalResult, old.FinalResult) || !got.CreatedAt.Equal(old.CreatedAt) || !got.UpdatedAt.After(old.CreatedAt) {
		t.Fatal("allocation/lifecycle changed", got)
	}
	if !reflect.DeepEqual(st.touched, []string{"p1", "p2"}) {
		t.Fatal("shared projects were not serialized with grading writes", st.touched)
	}
}

func TestAllocatedItemSubjectAndTestedMetadataRemainLocked(t *testing.T) {
	for _, kind := range []string{"subject", "tested", "readOnly"} {
		t.Run(kind, func(t *testing.T) {
			old := domain.Item{ID: "MAT-001", SubjectCode: "MAT", SubjectName: "Matematika", QuestionType: domain.QuestionTypePG, MaxScore: 1, KJ: "A", UsageStatus: domain.ItemAllocated}
			if kind == "tested" {
				old.UsageStatus = domain.ItemTested
			}
			if kind == "readOnly" {
				old.ReadOnly = true
			}
			st := &catalogMetadataStore{items: []domain.Item{old}}
			updated := old
			updated.KJ = "B"
			if kind == "subject" {
				updated.SubjectCode = "BIN"
				updated.SubjectName = "Bahasa Indonesia"
			}
			if err := service.New(st, nil, "").UpsertItems(context.Background(), []domain.Item{updated}); err == nil {
				t.Fatal("locked item edit accepted", kind)
			}
			if !reflect.DeepEqual(st.items[0], old) {
				t.Fatal("rejected edit changed item")
			}
		})
	}
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
		{"negative", `"kj":" A ","measure":-1.25,"optionCount":4`, 200, "A", -1.25},
		{"zero", `"kj":"B","measure":0,"optionCount":4`, 200, "B", 0.0},
		{"absent measure", `"kj":"A","optionCount":4`, 200, "A", nil},
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

func TestCatalogOptionCountRequiredAndValidatesKeys(t *testing.T) {
	for _, tc := range []struct {
		name, kind, key, count string
		existing               bool
		status                 int
	}{
		{"new PG missing", "pilihan_ganda", "A", "", false, 409},
		{"new MCMA missing", "pilihan_ganda_kompleks_mcma", "13", "", false, 409},
		{"new BS missing", "pilihan_ganda_kompleks_bs", "BSB", "", false, 409},
		{"PG valid", "pilihan_ganda", "D", `,"optionCount":4`, false, 200},
		{"PG extended label", "pilihan_ganda", "CV", `,"optionCount":100`, false, 200},
		{"PG past extended limit", "pilihan_ganda", "CW", `,"optionCount":100`, false, 409},
		{"PG lowercase", "pilihan_ganda", "a", `,"optionCount":4`, false, 409},
		{"PG out of range", "pilihan_ganda", "E", `,"optionCount":4`, false, 409},
		{"PG multiple", "pilihan_ganda", "A,C", `,"optionCount":4`, false, 409},
		{"MCMA valid independent score", "pilihan_ganda_kompleks_mcma", "15", `,"optionCount":5`, false, 200},
		{"MCMA old letters", "pilihan_ganda_kompleks_mcma", "A,C,E", `,"optionCount":5`, false, 409},
		{"MCMA duplicate", "pilihan_ganda_kompleks_mcma", "11", `,"optionCount":5`, false, 409},
		{"MCMA out of range", "pilihan_ganda_kompleks_mcma", "16", `,"optionCount":5`, false, 409},
		{"BS valid independent score", "pilihan_ganda_kompleks_bs", "SBSBS", `,"optionCount":5`, false, 200},
		{"BS wrong length", "pilihan_ganda_kompleks_bs", "SBSB", `,"optionCount":5`, false, 409},
		{"BS separators", "pilihan_ganda_kompleks_bs", "B,S,B", `,"optionCount":3`, false, 409},
		{"zero", "pilihan_ganda", "A", `,"optionCount":0`, false, 409},
		{"negative", "pilihan_ganda", "A", `,"optionCount":-1`, false, 409},
		{"over limit", "pilihan_ganda", "A", `,"optionCount":101`, false, 409},
		{"fraction", "pilihan_ganda", "A", `,"optionCount":2.5`, false, 422},
		{"essay no options", "essay", "Rubrik", "", false, 200},
		{"legacy PG untouched", "pilihan_ganda", "legacy-key", "", true, 200},
		{"legacy MCMA untouched", "pilihan_ganda_kompleks_mcma", "legacy-key", "", true, 200},
		{"legacy BS untouched", "pilihan_ganda_kompleks_bs", "legacy-key", "", true, 200},
		{"legacy opting in validated", "pilihan_ganda", "E", `,"optionCount":4`, true, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &catalogMetadataStore{}
			if tc.existing {
				st.items = []domain.Item{{ID: "OPT", SubjectCode: "MAT", QuestionType: domain.QuestionType(tc.kind), MaxScore: 1, KJ: "old", UsageStatus: domain.ItemAvailable}}
			}
			h := &Handler{service: service.New(st, nil, "")}
			app := fiber.New()
			app.Post("/items", h.saveItems)
			app.Get("/items/:id", h.itemDetail)
			body := `{"items":[{"id":"OPT","subjectCode":"MAT","subjectName":"Matematika","questionType":"` + tc.kind + `","maxScore":1,"kj":"` + tc.key + `"` + tc.count + `}]}`
			req := httptest.NewRequest("POST", "/items", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.status {
				t.Fatalf("status %d, want %d", res.StatusCode, tc.status)
			}
			if tc.status == 200 {
				res, _ := app.Test(httptest.NewRequest("GET", "/items/OPT", nil))
				defer res.Body.Close()
				var payload struct {
					Data map[string]any `json:"data"`
				}
				json.NewDecoder(res.Body).Decode(&payload)
				if tc.count != "" && payload.Data["optionCount"] == nil {
					t.Fatal("optionCount lost on round trip")
				}
				if tc.count == "" && payload.Data["optionCount"] != nil {
					t.Fatal("legacy/essay acquired fallback optionCount")
				}
			}
		})
	}
}
