package domain

import (
	"strings"
	"testing"
)

func TestActivationRejectsEmptyFormattedCandidateDefinition(t *testing.T) {
	p := Project{FeatureVersion: 1, Subjects: []ProjectSubject{{Name: "Matematika", CandidateDefinition: "<!--standard-setting-rich-text:v1--><ul><li><p><br></p></li></ul>"}}}
	if !strings.Contains(strings.Join(ValidateActivation(p), "|"), "deskripsi kandidat batas lulus Matematika wajib diisi") {
		t.Fatal("empty formatting must not count as a candidate definition")
	}
}
