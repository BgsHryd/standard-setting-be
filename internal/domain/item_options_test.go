package domain

import "testing"

func TestMCMAKeyUsesCompactOptionNumbers(t *testing.T) {
	for _, tc := range []struct {
		key   string
		count int
		valid bool
	}{
		{"135", 5, true}, {"31", 3, true}, {"9", 9, true}, {" 135 ", 5, true},
		{"", 5, false}, {"0", 5, false}, {"0135", 5, false}, {"113", 5, false}, {"16", 5, false},
		{"1,3,5", 5, false}, {"1 3 5", 5, false}, {"A,C,E", 5, false}, {"ACE", 5, false},
	} {
		t.Run(tc.key, func(t *testing.T) {
			err := ValidateItemOptions(QuestionTypeComplexMCMA, &tc.count, tc.key, true)
			if (err == nil) != tc.valid {
				t.Fatalf("key %q count %d valid=%t: %v", tc.key, tc.count, tc.valid, err)
			}
		})
	}
}
