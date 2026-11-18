package service

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
)

type wrightMemoryStore struct {
	Store
	users        map[string]domain.User
	project      domain.Project
	current      *domain.WrightMap
	touched      int
	beforeCommit func()
}

func (st *wrightMemoryStore) FindByID(_ context.Context, id string) (domain.User, error) {
	u, ok := st.users[id]
	if !ok {
		return u, mongo.ErrNoDocuments
	}
	return u, nil
}
func (st *wrightMemoryStore) GetProject(_ context.Context, id string) (domain.Project, error) {
	if id != st.project.ID {
		return domain.Project{}, mongo.ErrNoDocuments
	}
	return st.project, nil
}
func (st *wrightMemoryStore) TouchProject(context.Context, string) error { st.touched++; return nil }
func (st *wrightMemoryStore) GetWrightMap(context.Context, string, string) (*domain.WrightMap, error) {
	return st.current, nil
}
func (st *wrightMemoryStore) ReplaceWrightMap(ctx context.Context, p, s string, expected int, v domain.WrightMap, authorize func(context.Context) error) (*domain.WrightMap, error) {
	if st.beforeCommit != nil {
		st.beforeCommit()
	}
	if err := authorize(ctx); err != nil {
		return nil, err
	}
	version := 0
	if st.current != nil {
		version = st.current.Version
	}
	if version != expected {
		return nil, domain.ErrWrightMapConflict
	}
	v.Version = version + 1
	st.current = &v
	return &v, nil
}
func wrightFixture() (*Service, *wrightMemoryStore) {
	st := &wrightMemoryStore{users: map[string]domain.User{}, project: domain.Project{ID: "p", Subjects: []domain.ProjectSubject{{Code: "MAT", PICUserID: "pic", PanelistUserIDs: []string{"panel"}}}}}
	for _, u := range []domain.User{{ID: "admin", Role: domain.RoleAdminSuper}, {ID: "pic", Role: domain.RolePICMapel, ProjectID: "p", SubjectCode: "MAT"}, {ID: "panel", Role: domain.RolePanelist, ProjectID: "p", SubjectCode: "MAT"}, {ID: "other", Role: domain.RolePanelist, ProjectID: "p", SubjectCode: "MAT"}} {
		u.Active = true
		st.users[u.ID] = u
	}
	return New(st, nil, ""), st
}
func TestWrightMapMissingReplacementAndVersion(t *testing.T) {
	s, st := wrightFixture()
	ctx := context.Background()
	if v, e := s.WrightMap(ctx, "panel", "p", "MAT"); e != nil || v != nil {
		t.Fatal(v, e)
	}
	first, e := s.SaveWrightMap(ctx, "admin", "p", "MAT", WrightMapInput{FileName: "map.txt", Text: "first"})
	if e != nil || first.Version != 1 {
		t.Fatal(first, e)
	}
	if _, e = s.SaveWrightMap(ctx, "admin", "p", "MAT", WrightMapInput{FileName: "map.txt", Text: "stale"}); !errors.Is(e, domain.ErrWrightMapConflict) {
		t.Fatal(e)
	}
	second, e := s.SaveWrightMap(ctx, "admin", "p", "MAT", WrightMapInput{FileName: "new.txt", Text: "replacement", Version: 1})
	if e != nil || second.Version != 2 {
		t.Fatal(second, e)
	}
	got, e := s.WrightMap(ctx, "pic", "p", "MAT")
	if e != nil || got.Text != "replacement" || got.FileName != "new.txt" || st.touched == 0 {
		t.Fatal(got, e)
	}
}
func TestWrightMapAuthorizationAndProjectFreeze(t *testing.T) {
	for _, actor := range []string{"pic", "panel", "other", "missing"} {
		s, _ := wrightFixture()
		if _, e := s.SaveWrightMap(context.Background(), actor, "p", "MAT", WrightMapInput{FileName: "x.txt", Text: "x"}); !errors.Is(e, domain.ErrWrightMapForbidden) {
			t.Fatalf("write %s: %v", actor, e)
		}
	}
	for _, tc := range []struct{ actor, p, sub string }{{"panel", "other", "MAT"}, {"pic", "p", "BIN"}, {"other", "p", "MAT"}, {"missing", "p", "MAT"}} {
		s, _ := wrightFixture()
		if _, e := s.WrightMap(context.Background(), tc.actor, tc.p, tc.sub); !errors.Is(e, domain.ErrWrightMapForbidden) {
			t.Fatalf("read %+v: %v", tc, e)
		}
	}
	s, st := wrightFixture()
	u := st.users["admin"]
	u.Active = false
	st.users["admin"] = u
	if _, e := s.WrightMap(context.Background(), "admin", "p", "MAT"); !errors.Is(e, domain.ErrWrightMapForbidden) {
		t.Fatal(e)
	}
	s, st = wrightFixture()
	st.beforeCommit = func() { st.project.Status = domain.ProjectCompleted }
	if _, e := s.SaveWrightMap(context.Background(), "admin", "p", "MAT", WrightMapInput{FileName: "map.txt", Text: "x"}); e == nil || st.current != nil || st.touched != 0 {
		t.Fatal("completion during upload accepted", e)
	}
}
