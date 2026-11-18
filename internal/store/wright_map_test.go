package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"standard-setting/backend/internal/domain"
)

func TestWrightMapGridFSPersistenceReplacementCAS(t *testing.T) {
	ctx, m := securityMongo(t)
	if _, e := m.projects.InsertOne(ctx, domain.Project{ID: "p"}); e != nil {
		t.Fatal(e)
	}
	authorize := func(tx context.Context) error { return m.TouchProject(tx, "p") }
	v := domain.WrightMap{FileName: "map.txt", Text: "<script>literal</script>\n+1.0", UpdatedAt: time.Now().UTC()}
	if got, e := m.GetWrightMap(ctx, "p", "MAT"); e != nil || got != nil {
		t.Fatal(got, e)
	}
	first, e := m.ReplaceWrightMap(ctx, "p", "MAT", 0, v, authorize)
	if e != nil || first.Version != 1 {
		t.Fatal(first, e)
	}
	if n, e := m.database.Collection("wright_maps.files").CountDocuments(ctx, bson.M{}); e != nil || n != 1 {
		t.Fatal("not persisted in GridFS", n, e)
	}
	v.Text = "replacement"
	if _, e = m.ReplaceWrightMap(ctx, "p", "MAT", 0, v, authorize); !errors.Is(e, domain.ErrWrightMapConflict) {
		t.Fatal(e)
	}
	if _, e = m.ReplaceWrightMap(ctx, "p", "MAT", 1, v, authorize); e != nil {
		t.Fatal(e)
	}
	got, e := m.GetWrightMap(ctx, "p", "MAT")
	if e != nil || got.Text != "replacement" || got.Version != 2 {
		t.Fatal(got, e)
	}
	if n, e := m.database.Collection("wright_maps.files").CountDocuments(ctx, bson.M{}); e != nil || n != 1 {
		t.Fatal("replacement left stale GridFS files", n, e)
	}
	if n, e := m.database.Collection("wright_map_current").CountDocuments(ctx, bson.M{}); e != nil || n != 1 {
		t.Fatal("multiple current files", n, e)
	}
}
