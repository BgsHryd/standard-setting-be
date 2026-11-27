package domain

import (
	"errors"
	"html"
	"strconv"
	"strings"
	"time"
	"unicode"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var ErrInstructionConflict = errors.New("petunjuk sudah diperbarui; muat versi terbaru sebelum menyimpan")

const DefaultReviewRangeThreshold = 20

func ValidReviewRangeThreshold(value int) bool { return value >= 1 && value <= 100 }

func DefaultPGKRecode() []int { return []int{0, 0, 1, 1, 2, 2} }

func ValidPGKRecode(scores []int) bool {
	if len(scores) != 6 || scores[0] != 0 || scores[5] != 2 {
		return false
	}
	hasMiddle := false
	for i := 1; i < len(scores); i++ {
		if scores[i] < scores[i-1] {
			return false
		}
		hasMiddle = hasMiddle || scores[i] == 1
	}
	return hasMiddle
}

type AnswerInstruction struct {
	Title       string `json:"title" bson:"title"`
	Description string `json:"description" bson:"description"`
}

func DefaultAnswerInstructions() map[QuestionType]AnswerInstruction {
	result := make(map[QuestionType]AnswerInstruction)
	for kind := range DefaultQuestionInstructions() {
		result[kind] = AnswerInstruction{Title: "Skor kandidat batas lulus", Description: "Masukkan satu angka bulat dari 0 sampai skor maksimum butir."}
	}
	result[QuestionTypePG] = AnswerInstruction{Title: "Nilai kelayakan kandidat", Description: "Masukkan angka bulat 0–100."}
	return result
}

func ValidAnswerInstruction(value AnswerInstruction) bool {
	meaningful := func(s string) bool {
		for _, r := range s {
			if !unicode.IsSpace(r) && !unicode.Is(unicode.Cf, r) {
				return true
			}
		}
		return false
	}
	return meaningful(value.Title) && meaningful(value.Description) && len([]rune(value.Title)) <= 200 && len([]rune(value.Description)) <= 1000
}

type QuestionInstructionConfig struct {
	PGKRecode            []int                              `json:"pgkRecode" bson:"pgkRecode"`
	AnswerInstructions   map[QuestionType]AnswerInstruction `json:"answerInstructions" bson:"answerInstructions"`
	ReviewRangeThreshold int                                `json:"reviewRangeThreshold" bson:"reviewRangeThreshold"`
	ID                   string                             `json:"-" bson:"_id"`
	Version              int                                `json:"version" bson:"version"`
	Instructions         map[QuestionType]string            `json:"instructions" bson:"instructions"`
	ShowExamples         map[QuestionType]bool              `json:"tampilkan_soal_contoh" bson:"tampilkan_soal_contoh"`
	UpdatedAt            time.Time                          `json:"updatedAt" bson:"updatedAt"`
}

func DefaultQuestionExamples() map[QuestionType]bool {
	return map[QuestionType]bool{QuestionTypeComplexMCMA: false, QuestionTypeComplexBS: false}
}

func DefaultQuestionInstructions() map[QuestionType]string {
	return map[QuestionType]string{
		QuestionTypePG:          `<p>Kunci berisi <strong>satu opsi benar</strong>. Bayangkan 100 kandidat borderline mengerjakan butir ini: berapa orang yang memilih kunci, dan berapa yang terkecoh pengecoh?</p><p>Tulis <strong>banyak kandidat yang menjawab benar</strong> (0–100).</p>`,
		QuestionTypeComplexMCMA: `<p>Kunci dapat berisi <strong>lebih dari satu opsi benar</strong>. Bayangkan 100 kandidat batas lulus mengerjakan butir ini.</p><p>Masukkan <strong>satu skor bulat dari 0 sampai skor maksimum butir</strong> sesuai rubrik.</p>`,
		QuestionTypeComplexBS:   `<p>Bayangkan 100 kandidat batas lulus menilai pernyataan <strong>Benar–Salah</strong> pada butir ini.</p><p>Masukkan <strong>satu skor bulat dari 0 sampai skor maksimum butir</strong> sesuai rubrik.</p>`,
		QuestionTypeEssay:       `<p>Gunakan <strong>rubrik penskoran</strong> untuk memperkirakan skor rata-rata peserta batas lulus.</p><p>Masukkan satu angka bulat dari 0 sampai skor maksimum butir.</p>`,
		QuestionTypeComplex:     `<p>Bayangkan kandidat batas lulus mengerjakan butir ini.</p><p>Masukkan <strong>satu skor bulat dari 0 sampai skor maksimum butir</strong> sesuai rubrik.</p>`,
	}
}

func SanitizeQuestionInstruction(value string) (string, error) {
	if len(value) > 100000 {
		return "", errors.New("petunjuk maksimal 100 KB")
	}
	nodes, err := xhtml.ParseFragment(strings.NewReader(value), &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return "", err
	}
	var output strings.Builder
	textCount, meaningful := 0, false
	var write func(*xhtml.Node)
	write = func(node *xhtml.Node) {
		if node.Type == xhtml.TextNode {
			output.WriteString(html.EscapeString(node.Data))
			for _, r := range node.Data {
				textCount++
				if !unicode.IsSpace(r) && !unicode.Is(unicode.Cf, r) {
					meaningful = true
				}
			}
			return
		}
		if node.Type != xhtml.ElementNode {
			return
		}
		tag := node.Data
		switch tag {
		case "script", "style", "textarea", "option", "iframe", "object", "svg", "math", "template":
			return
		case "b":
			tag = "strong"
		case "i":
			tag = "em"
		}
		allowed := false
		switch tag {
		case "p", "br", "strong", "em", "u", "ul", "ol", "li", "h1", "h2", "h3", "table", "thead", "tbody", "tfoot", "tr", "th", "td":
			allowed = true
		}
		if allowed {
			output.WriteString("<" + tag)
			if tag == "td" || tag == "th" {
				for _, attr := range node.Attr {
					if attr.Key == "colspan" || attr.Key == "rowspan" {
						if number, e := strconv.Atoi(attr.Val); e == nil && number >= 1 && number <= 20 {
							output.WriteString(" " + attr.Key + `="` + strconv.Itoa(number) + `"`)
						}
					}
				}
			}
			output.WriteString(">")
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			write(child)
		}
		if allowed && tag != "br" {
			output.WriteString("</" + tag + ">")
		}
	}
	for _, node := range nodes {
		write(node)
	}
	if !meaningful {
		return "", errors.New("petunjuk wajib berisi teks")
	}
	if textCount > 20000 || output.Len() > 100000 {
		return "", errors.New("petunjuk maksimal 20.000 karakter teks dan 100 KB")
	}
	return output.String(), nil
}
