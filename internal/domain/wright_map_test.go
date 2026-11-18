package domain

import (
	"strings"
	"testing"
)

func TestValidateWrightMap(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		valid      bool
	}{
		{"map.TXT", "<script>alert(1)</script>\n測定\t1", true},
		{"map.txt", strings.Repeat("a", MaxWrightMapBytes), true},
		{"map.csv", "text", false}, {"../map.txt", "text", false}, {"map.txt", "", false},
		{"map.txt", strings.Repeat("a", MaxWrightMapBytes+1), false},
		{"map.txt", "a\x00b", false}, {"map.txt", "a\x1bb", false}, {"map.txt", string([]byte{0xff}), false},
	} {
		if err := ValidateWrightMap(tc.name, tc.text); (err == nil) != tc.valid {
			t.Errorf("%q (%d bytes): %v", tc.name, len(tc.text), err)
		}
	}
}
