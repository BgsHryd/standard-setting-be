package httpapi

import "standard-setting/backend/internal/domain"

// The shared domain Item omits these fields from JSON. Only admin endpoints
// explicitly add them. Panelist forms use a separate DTO gated by PIC settings.
type catalogItemDTO struct {
	domain.Item
	KJ              string   `json:"kj"`
	Measure         *float64 `json:"measure"`
	MeasureCategory string   `json:"measureCategory"`
}

func adminItem(item domain.Item) catalogItemDTO {
	return catalogItemDTO{Item: item, KJ: item.KJ, Measure: item.Measure, MeasureCategory: item.MeasureCategory}
}

func adminItems(items []domain.Item) []catalogItemDTO {
	result := make([]catalogItemDTO, len(items))
	for i, item := range items {
		result[i] = adminItem(item)
	}
	return result
}
