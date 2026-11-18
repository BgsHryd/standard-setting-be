package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
)

type wrightHTTPStore struct {
	service.Store
	user    domain.User
	current *domain.WrightMap
}

func (st *wrightHTTPStore) FindByID(context.Context, string) (domain.User, error) {
	return st.user, nil
}
func (st *wrightHTTPStore) GetProject(context.Context, string) (domain.Project, error) {
	return domain.Project{ID: "p", Subjects: []domain.ProjectSubject{{Code: "MAT", PICUserID: "pic", PanelistUserIDs: []string{"panel"}}}}, nil
}
func (st *wrightHTTPStore) TouchProject(context.Context, string) error { return nil }
func (st *wrightHTTPStore) GetWrightMap(context.Context, string, string) (*domain.WrightMap, error) {
	return st.current, nil
}
func (st *wrightHTTPStore) ReplaceWrightMap(ctx context.Context, p, s string, expected int, v domain.WrightMap, check func(context.Context) error) (*domain.WrightMap, error) {
	if e := check(ctx); e != nil {
		return nil, e
	}
	current := 0
	if st.current != nil {
		current = st.current.Version
	}
	if expected != current {
		return nil, domain.ErrWrightMapConflict
	}
	v.Version = current + 1
	st.current = &v
	return st.current, nil
}
func TestWrightMapHTTP(t *testing.T) {
	st := &wrightHTTPStore{user: domain.User{ID: "admin", Role: domain.RoleAdminSuper, Active: true}}
	h := &Handler{service: service.New(st, nil, "")}
	app := fiber.New()
	routes := app.Group("", func(c *fiber.Ctx) error {
		c.Locals("claims", &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: st.user.ID}, Role: st.user.Role})
		return c.Next()
	})
	h.registerWrightMapRoutes(routes)
	request := func(method, path, body string, status int) map[string]any {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, e := app.Test(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		var result map[string]any
		if e = json.NewDecoder(res.Body).Decode(&result); e != nil {
			t.Fatal(e)
		}
		if res.StatusCode != status {
			t.Fatalf("%s %s status %d wanted %d: %+v", method, path, res.StatusCode, status, result)
		}
		return result
	}
	path := "/projects/p/wright-maps/MAT"
	if got := request("GET", path, "", 200); got["data"] != nil {
		t.Fatal(got)
	}
	request("PUT", path, `{"fileName":"map.txt","text":"literal <b>x</b>","version":0}`, 200)
	request("PUT", path, `{"fileName":"map.txt","text":"stale","version":0}`, 409)
	request("PUT", path, `{"fileName":"map.txt","text":"x","version":null}`, 422)
	request("PUT", path, `{"fileName":"map.exe","text":"x","version":1}`, 422)
	request("PUT", path, `{"fileName":"map.txt","text":"a\u0000b","version":1}`, 422)
	st.user = domain.User{ID: "panel", Role: domain.RolePanelist, Active: true, ProjectID: "p", SubjectCode: "MAT"}
	request("GET", path, "", 200)
	request("GET", "/projects/another/wright-maps/MAT", "", 403)
	request("GET", "/projects/p/wright-maps/BIN", "", 403)
	request("PUT", path, `{"fileName":"map.txt","text":"x","version":1}`, 403)
	st.user.Active = false
	request("GET", path, "", 403)
}
