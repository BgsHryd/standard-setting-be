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

type QuestionInstructionConfig struct {
	ReviewRangeThreshold int                     `json:"reviewRangeThreshold" bson:"reviewRangeThreshold"`
	ID                   string                  `json:"-" bson:"_id"`
	Version              int                     `json:"version" bson:"version"`
	Instructions         map[QuestionType]string `json:"instructions" bson:"instructions"`
	ShowExamples         map[QuestionType]bool   `json:"tampilkan_soal_contoh" bson:"tampilkan_soal_contoh"`
	UpdatedAt            time.Time               `json:"updatedAt" bson:"updatedAt"`
}

func DefaultQuestionExamples() map[QuestionType]bool {
	return map[QuestionType]bool{QuestionTypeComplexMCMA: false, QuestionTypeComplexBS: false}
}

func DefaultQuestionInstructions() map[QuestionType]string {
	return map[QuestionType]string{
		QuestionTypePG:          `<p>Kunci berisi <strong>satu opsi benar</strong>. Bayangkan 100 kandidat borderline mengerjakan butir ini: berapa orang yang memilih kunci, dan berapa yang terkecoh pengecoh?</p><p>Tulis <strong>banyak kandidat yang menjawab benar</strong> (0–100).</p>`,
		QuestionTypeComplexMCMA: `<p>Kunci dapat berisi <strong>lebih dari satu opsi benar</strong>. Bayangkan 100 kandidat batas lulus mengerjakan butir ini.</p><p>Bagikan kandidat ke setiap skor sesuai rubrik. Jumlah seluruh distribusi harus <strong>tepat 100</strong>.</p>`,
		QuestionTypeComplexBS:   `<p>Bayangkan 100 kandidat batas lulus menilai pernyataan <strong>Benar–Salah</strong> pada butir ini.</p><p>Bagikan kandidat ke setiap skor sesuai rubrik. Jumlah seluruh distribusi harus <strong>tepat 100</strong>.</p>`,
		QuestionTypeEssay:       `<p>Bayangkan jawaban 100 kandidat batas lulus, lalu gunakan <strong>rubrik penskoran</strong> untuk memperkirakan distribusi skornya.</p><p>Jumlah kandidat pada seluruh skor harus <strong>tepat 100</strong>.</p>`,
		QuestionTypeComplex:     `<p>Bayangkan 100 kandidat batas lulus mengerjakan butir ini. Bagikan kandidat ke setiap skor sesuai rubrik.</p><p>Jumlah seluruh distribusi harus <strong>tepat 100</strong>.</p>`,
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
