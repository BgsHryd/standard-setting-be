package store

import (
	"context"

	"standard-setting/backend/internal/domain"
)

// InsertCatalogItems is deliberately insert-only: an import must never replace
// an existing item, even when another writer adds that ID after validation.
func (m *Mongo) InsertCatalogItems(ctx context.Context, items []domain.Item) error {
	documents := make([]any, len(items))
	for i, item := range items {
		documents[i] = item
	}
	_, err := m.items.InsertMany(ctx, documents)
	return err
}
