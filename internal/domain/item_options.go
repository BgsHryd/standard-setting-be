package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var optionLabelPattern = regexp.MustCompile(`^[A-Z]+$`)
var bsKeyPattern = regexp.MustCompile(`^[BS]+$`)

func (kind QuestionType) RequiresOptionCount() bool {
	return kind == QuestionTypePG || kind == QuestionTypeComplexMCMA || kind == QuestionTypeComplexBS
}

// ValidateItemOptions leaves legacy missing counts untouched when not required.
// Option count is independent of the score scale and must never be inferred from it.
func ValidateItemOptions(kind QuestionType, count *int, key string, required bool) error {
	if !kind.RequiresOptionCount() {
		return nil
	}
	if count == nil {
		if required {
			return errors.New("jumlah opsi wajib diisi untuk Pilihan Ganda, MCMA, dan BS")
		}
		return nil
	}
	if *count < 1 || *count > 100 {
		return errors.New("jumlah opsi harus bilangan bulat 1–100")
	}
	key = strings.TrimSpace(key)
	if kind == QuestionTypeComplexBS {
		if !bsKeyPattern.MatchString(key) || len(key) != *count {
			return errors.New("KJ BS harus berisi B atau S tanpa pemisah, sebanyak jumlah opsi")
		}
		return nil
	}
	labels := strings.Split(key, ",")
	if kind == QuestionTypePG && len(labels) != 1 {
		return errors.New("KJ Pilihan Ganda harus satu label opsi")
	}
	seen := map[string]bool{}
	for _, label := range labels {
		if !optionLabelPattern.MatchString(label) || seen[label] {
			return errors.New("KJ harus berisi label opsi unik dalam huruf besar; MCMA dipisahkan koma tanpa spasi")
		}
		index := 0
		for _, letter := range label {
			index = index*26 + int(letter-'A') + 1
			if index > *count {
				return fmt.Errorf("KJ %s di luar jumlah opsi (%d)", label, *count)
			}
		}
		seen[label] = true
	}
	return nil
}
