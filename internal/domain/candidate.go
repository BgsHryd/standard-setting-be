package domain

import (
	stdhtml "html"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const CandidateRichTextPrefix = "<!--standard-setting-rich-text:v1-->"

// NormalizeCandidateDefinition preserves legacy text and limits formatted content
// to paragraphs, lists, bold, italic, and underline without HTML attributes.
func NormalizeCandidateDefinition(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, CandidateRichTextPrefix) {
		return value
	}
	nodes, err := html.ParseFragment(strings.NewReader(strings.TrimPrefix(value, CandidateRichTextPrefix)), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return ""
	}
	var output strings.Builder
	hasText := false
	var write func(*html.Node)
	write = func(node *html.Node) {
		if node.Type == html.TextNode {
			output.WriteString(stdhtml.EscapeString(node.Data))
			for _, character := range node.Data {
				if !unicode.IsSpace(character) && !unicode.Is(unicode.Cf, character) {
					hasText = true
				}
			}
			return
		}
		if node.Type != html.ElementNode {
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
		allowed := tag == "p" || tag == "br" || tag == "strong" || tag == "em" || tag == "u" || tag == "ul" || tag == "ol" || tag == "li"
		if allowed {
			output.WriteString("<" + tag + ">")
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
	if !hasText {
		return ""
	}
	return CandidateRichTextPrefix + output.String()
}
