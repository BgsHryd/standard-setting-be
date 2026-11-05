package domain

import (
	"strings"
	"testing"
)

func TestQuestionInstructionsPreserveFormattingAndRemoveUnsafeHTML(t *testing.T) {
	clean, err := SanitizeQuestionInstruction(`<h1 onclick="bad()">Judul</h1><h2>Bagian</h2><p><b>Tebal</b> <i>Miring</i> <u>Garis</u></p><table style="width:9000px"><tr><th colspan="2">Skor</th><td rowspan="999">Isi</td></tr></table><script>alert(1)</script><img src=x onerror=bad()><a href="javascript:bad()">teks</a>`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<h1>Judul</h1>", "<h2>Bagian</h2>", "<strong>Tebal</strong>", "<em>Miring</em>", "<u>Garis</u>", `<th colspan="2">Skor</th>`, "<td>Isi</td>"} {
		if !strings.Contains(clean, want) {
			t.Fatalf("lost %q in %s", want, clean)
		}
	}
	for _, bad := range []string{"onclick", "style=", "script", "alert(1)", "onerror", "javascript:", "rowspan"} {
		if strings.Contains(clean, bad) {
			t.Fatalf("unsafe content %q: %s", bad, clean)
		}
	}
}

func TestQuestionInstructionsRejectEmptyAndOversizedContent(t *testing.T) {
	for _, value := range []string{"", "<p>&nbsp;</p>", "<script>bad()</script>", strings.Repeat("x", 20001), strings.Repeat(" ", 100001)} {
		if _, err := SanitizeQuestionInstruction(value); err == nil {
			t.Fatalf("accepted invalid length=%d", len(value))
		}
	}
	defaults := DefaultQuestionInstructions()
	if len(defaults) != 5 || !strings.Contains(defaults[QuestionTypePG], "satu opsi benar") {
		t.Fatal("missing defaults")
	}
	defaults[QuestionTypePG] = "changed"
	if DefaultQuestionInstructions()[QuestionTypePG] == "changed" {
		t.Fatal("shared mutable defaults")
	}
}
