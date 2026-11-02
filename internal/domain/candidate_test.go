package domain

import (
	"strings"
	"testing"
)

func TestActivationAllowsEmptyCandidateDefinitionForPICToComplete(t *testing.T) {
	p := Project{FeatureVersion: 1, Subjects: []ProjectSubject{{Name: "Matematika", CandidateDefinition: "<!--standard-setting-rich-text:v1--><ul><li><p><br></p></li></ul>"}}}
	if strings.Contains(strings.Join(ValidateActivation(p), "|"), "deskripsi kandidat") {
		t.Fatal("admin must be able to activate a project before the PIC fills JQC")
	}
}
