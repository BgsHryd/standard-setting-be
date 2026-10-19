package domain

import (
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nyaruka/phonenumbers"
)

var ErrRevisionConflict = errors.New("data atau versi putaran sudah berubah; tinjau ulang sebelum menyimpan")

// NormalizeContact validates each supplied channel; one valid channel is sufficient.
// This validates syntax, not ownership or deliverability.
func NormalizeContact(whatsapp, email string) (string, string, error) {
	whatsapp, email = strings.TrimSpace(whatsapp), strings.TrimSpace(email)
	if whatsapp == "" && email == "" {
		return "", "", errors.New("isi minimal nomor WhatsApp atau email")
	}
	if whatsapp != "" {
		if len(whatsapp) > 40 {
			return "", "", errors.New("nomor WhatsApp terlalu panjang")
		}
		n, err := phonenumbers.Parse(whatsapp, "ID")
		if err != nil || !phonenumbers.IsValidNumber(n) || n.GetExtension() != "" {
			return "", "", errors.New("nomor WhatsApp tidak valid; sertakan kode negara untuk nomor luar Indonesia")
		}
		whatsapp = phonenumbers.Format(n, phonenumbers.E164)
	}
	if email != "" {
		parsed, err := mail.ParseAddress(email)
		parts := strings.Split(email, "@")
		if len(email) > 254 || err != nil || parsed.Address != email || len(parts) != 2 || !strings.Contains(parts[1], ".") {
			return "", "", errors.New("alamat email tidak valid")
		}
	}
	return whatsapp, email, nil
}

func ProjectWritable(p Project, now time.Time) bool {
	return p.Status == ProjectActive && !p.StartDate.IsZero() && !p.EndDate.IsZero() && !now.Before(p.StartDate) && now.Before(p.EndDate)
}

func RoundRevision(r Round) int {
	if r.Revision < 1 {
		return 1
	}
	return r.Revision
}

func SubmissionReviewed(row Submission, round Round) bool {
	revision := RoundRevision(round)
	return row.ReviewRevision == revision || (revision == 1 && row.ReviewRevision == 0 && row.RoundRevision <= 1)
}

func ValidateNote(note string) error {
	if utf8.RuneCountInString(note) > 1000 {
		return errors.New("catatan maksimal 1.000 karakter")
	}
	return nil
}

type PanelistContact struct {
	UserID      string    `json:"userId" bson:"_id"`
	ProjectID   string    `json:"projectId" bson:"projectId"`
	SubjectCode string    `json:"subjectCode" bson:"subjectCode"`
	WhatsApp    string    `json:"whatsapp" bson:"whatsapp"`
	Email       string    `json:"email" bson:"email"`
	Version     int       `json:"version" bson:"version"`
	UpdatedAt   time.Time `json:"updatedAt" bson:"updatedAt"`
}
