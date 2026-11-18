package domain

import (
	"errors"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const MaxWrightMapBytes = 1024 * 1024

var ErrWrightMapConflict = errors.New("Wright map telah berubah; muat ulang sebelum mengganti")
var ErrWrightMapForbidden = errors.New("Anda tidak berhak mengakses Wright map ini")
var ErrWrightMapNotFound = errors.New("proyek atau mata pelajaran tidak ditemukan")

type WrightMap struct {
	FileName  string    `json:"fileName" bson:"fileName"`
	Text      string    `json:"text" bson:"-"`
	UpdatedAt time.Time `json:"updatedAt" bson:"updatedAt"`
	Version   int       `json:"version" bson:"version"`
}

func ValidateWrightMap(name, text string) error {
	if name == "" || len(name) > 255 || strings.ContainsAny(name, "/\\") || !strings.EqualFold(path.Ext(name), ".txt") {
		return errors.New("unggah berkas TXT dengan nama maksimal 255 byte")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("nama berkas tidak valid")
		}
	}
	if len(text) == 0 || len(text) > MaxWrightMapBytes {
		return errors.New("berkas TXT harus berisi teks maksimal 1 MiB")
	}
	if !utf8.ValidString(text) {
		return errors.New("berkas TXT harus menggunakan UTF-8")
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return errors.New("berkas harus berupa teks TXT, bukan data biner")
		}
	}
	return nil
}
