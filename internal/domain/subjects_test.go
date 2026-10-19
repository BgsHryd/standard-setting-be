package domain

import "testing"

func TestNormalizeSubjectName(t *testing.T) {
	if NormalizeSubjectName("  Bahasa   INDONESIA \t") != "bahasa indonesia" {
		t.Fatal("subject normalization must collapse whitespace and case")
	}
}
