package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"standard-setting/backend/internal/domain"
)

type subjectConfigStore struct {
	*picRoundOverviewStore
	audits []domain.AuditEvent
}

func (st *subjectConfigStore) RecordAudit(_ context.Context, event domain.AuditEvent) error {
	st.audits = append(st.audits, event)
	return nil
}

func (st *subjectConfigStore) TouchProject(context.Context, string) error { return nil }
func (st *subjectConfigStore) SaveProject(_ context.Context, p domain.Project) error {
	st.project = p
	return nil
}

func subjectConfigFixture() (*Service, *subjectConfigStore) {
	_, base := picRoundOverviewFixture()
	base.project.Subjects = append(base.project.Subjects, domain.ProjectSubject{Code: "BIN", CandidateDefinition: "Mapel lain"})
	st := &subjectConfigStore{picRoundOverviewStore: base}
	return New(st, nil, ""), st
}

func TestPICSubjectConfigOwnScopeVersionAndFrozenHistory(t *testing.T) {
	s, st := subjectConfigFixture()
	ctx := context.Background()
	initial, err := s.PICSubjectConfig(ctx, "pic")
	if err != nil || initial.SubjectCode != "MAT" || initial.CandidateDefinition != "Definisi sekarang" {
		t.Fatal("wrong own-subject config", initial, err)
	}
	input := PICSubjectConfigInput{CandidateDefinition: domain.CandidateRichTextPrefix + `<p><b>JQC baru</b><script>alert(1)</script></p>`, ProjectVersion: initial.ProjectVersion}
	got, err := s.SavePICSubjectConfig(ctx, "pic", input)
	if err != nil || got.CandidateVersion != initial.CandidateVersion+1 || got.ProjectVersion != initial.ProjectVersion+1 || strings.Contains(got.CandidateDefinition, "script") || !strings.Contains(got.CandidateDefinition, "<strong>JQC baru</strong>") {
		t.Fatal("versioned sanitized save failed", got, err)
	}
	if st.project.Subjects[1].CandidateDefinition != "Mapel lain" || len(st.project.Subjects[0].CandidateHistory) != 2 || st.rounds[0].FinalSnapshot.CandidateDefinition != "Definisi saat finalisasi" {
		t.Fatal("changed another subject or lost candidate history/snapshot")
	}
	if len(st.audits) != 1 || st.audits[0].Action != "candidate_definition_update" || st.audits[0].ActorID != "pic" || st.audits[0].TargetID != "MAT" {
		t.Fatal("missing candidate-definition audit", st.audits)
	}
	if _, err := s.SavePICSubjectConfig(ctx, "pic", input); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale save accepted", err)
	}
	input.CandidateDefinition, input.ProjectVersion = got.CandidateDefinition, got.ProjectVersion
	unchanged, err := s.SavePICSubjectConfig(ctx, "pic", input)
	if err != nil || unchanged.CandidateVersion != got.CandidateVersion || unchanged.ProjectVersion != got.ProjectVersion {
		t.Fatal("unchanged save created a revision", unchanged, err)
	}
}

func TestPICSubjectConfigIntegrationReachesPanelistAndRejectsOldContext(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.FeatureVersion = 1
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	a := domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(60)}
	if _, err := s.SaveSubmissionWithOptions(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", a, SubmissionOptions{RoundRevision: 1, CandidateVersion: 0}); err != nil {
		t.Fatal("blank JQC version-zero context should be usable", err)
	}
	input := PICSubjectConfigInput{CandidateDefinition: "Kandidat memahami konsep dasar", ProjectVersion: p.Version}
	first, err := s.SavePICSubjectConfig(ctx, "pic", input)
	if err != nil {
		t.Fatal(err)
	}
	form, err := s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if err != nil || form.Subject.CandidateDefinition != first.CandidateDefinition || form.Subject.CandidateVersion != first.CandidateVersion {
		t.Fatal("panelist did not receive PIC definition", err)
	}
	if _, err := s.SaveSubmissionWithOptions(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", a, SubmissionOptions{RoundRevision: 1, CandidateVersion: 0, Version: 1}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("old blank-definition context accepted after first JQC save", err)
	}
	if _, err := s.SaveSubmissionWithOptions(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", a, SubmissionOptions{RoundRevision: 1, CandidateVersion: first.CandidateVersion, Version: 1}); err != nil {
		t.Fatal(err)
	}
	input.CandidateDefinition, input.ProjectVersion = "Kandidat memahami konsep dan penerapannya", first.ProjectVersion
	if _, err := s.SavePICSubjectConfig(ctx, "pic", input); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSubmissionWithOptions(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-001", a, SubmissionOptions{RoundRevision: 1, CandidateVersion: first.CandidateVersion, Version: 2}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("answer based on old definition accepted", err)
	}
}

func TestPICSubjectConfigRejectsInvalidDefinitionAndCompletedProject(t *testing.T) {
	for _, definition := range []string{"", "  ", domain.CandidateRichTextPrefix + "<p><br></p>", strings.Repeat("a", 8001)} {
		s, st := subjectConfigFixture()
		if _, err := s.SavePICSubjectConfig(context.Background(), "pic", PICSubjectConfigInput{CandidateDefinition: definition, ProjectVersion: st.project.Version}); err == nil {
			t.Fatal("invalid definition accepted")
		}
	}
	s, st := subjectConfigFixture()
	st.project.Status = domain.ProjectCompleted
	view, err := s.PICSubjectConfig(context.Background(), "pic")
	if err != nil || !view.ReadOnly {
		t.Fatal("completed project cannot be viewed read-only", view, err)
	}
	if _, err := s.SavePICSubjectConfig(context.Background(), "pic", PICSubjectConfigInput{CandidateDefinition: "JQC", ProjectVersion: view.ProjectVersion}); err == nil {
		t.Fatal("completed config changed")
	}
}

func TestPICSubjectConfigRejectsWrongAssignment(t *testing.T) {
	for _, kind := range []string{"inactive", "removed", "role", "project", "subject", "assignment"} {
		t.Run(kind, func(t *testing.T) {
			s, st := subjectConfigFixture()
			u := &st.users[len(st.users)-1]
			switch kind {
			case "inactive":
				u.Active = false
			case "removed":
				u.Removed = true
			case "role":
				u.Role = domain.RolePanelist
			case "project":
				u.ProjectID = "different"
			case "subject":
				u.SubjectCode = "different"
			case "assignment":
				st.project.Subjects[0].PICUserID = "different"
			}
			if _, err := s.PICSubjectConfig(context.Background(), "pic"); err == nil {
				t.Fatal("unauthorized read accepted")
			}
			if _, err := s.SavePICSubjectConfig(context.Background(), "pic", PICSubjectConfigInput{CandidateDefinition: "JQC"}); err == nil {
				t.Fatal("unauthorized write accepted")
			}
		})
	}
}
