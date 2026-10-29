package domain

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var measurePattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:[.,][0-9]*)?|[.,][0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// Categories are chosen by the administrator, never inferred from the numeric measure.
func NormalizeMeasureCategory(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case "sukar":
		return "Sukar", nil
	case "sedang":
		return "Sedang", nil
	case "mudah":
		return "Mudah", nil
	default:
		return "", errors.New("kategori measure harus Sukar, Sedang, atau Mudah")
	}
}

// ParseMeasure preserves blank versus zero and accepts a decimal comma without
// accepting grouping separators or non-finite floating-point values.
func ParseMeasure(value string) (*float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if !measurePattern.MatchString(value) {
		return nil, errors.New("Measure harus angka bulat atau desimal; nilai negatif diperbolehkan")
	}
	number, err := strconv.ParseFloat(strings.ReplaceAll(value, ",", "."), 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return nil, errors.New("Measure harus angka yang valid dan berhingga")
	}
	return &number, nil
}

func ValidateItemMetadata(key string, measure *float64) error {
	key = strings.TrimSpace(key)
	if key == "" || len([]rune(key)) > 8000 {
		return errors.New("KJ wajib diisi, maksimal 8000 karakter")
	}
	if measure != nil && (math.IsNaN(*measure) || math.IsInf(*measure, 0)) {
		return errors.New("Measure harus angka yang valid dan berhingga")
	}
	return nil
}
