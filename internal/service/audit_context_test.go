package service

import (
	"context"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
	"strings"
	"testing"
)

type auditContextStore struct {
	Store
	project domain.Project
	rounds  []domain.Round
	reads   int
}

func (st *auditContextStore) GetProject(context.Context, string) (domain.Project, error) {
	st.reads++
	if st.project.ID == "" {
		return domain.Project{}, mongo.ErrNoDocuments
	}
	return st.project, nil
}
func (st *auditContextStore) ListRounds(_ context.Context, project, subject string) ([]domain.Round, error) {
	out := []domain.Round{}
	for _, r := range st.rounds {
		if r.ProjectID == project && r.SubjectCode == subject {
			out = append(out, r)
		}
	}
	return out, nil
}
func TestAuditContextUsesNamesAndRoundNumbers(t *testing.T) {
	st := &auditContextStore{project: domain.Project{ID: "project_internal", Name: "Proyek Literasi", Subjects: []domain.ProjectSubject{{Code: "BIN", Name: "Bahasa Indonesia"}}}, rounds: []domain.Round{{ID: "round_internal", ProjectID: "project_internal", SubjectCode: "BIN", Number: 2}}}
	events := []domain.AuditEvent{{ID: "project", ProjectID: "project_internal", TargetID: "project_internal", Action: "project_configuration_update"}, {ID: "round", ProjectID: "project_internal", TargetID: "round_internal", Action: "round_finalize"}, {ID: "subject", ProjectID: "project_internal", TargetID: "BIN", Action: "candidate_definition_update"}, {ID: "account", ProjectID: "project_internal", TargetID: "user_internal", Action: "password_reset"}}
	labels, err := New(st, nil, "").AuditContexts(context.Background(), events)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"project", "round", "subject"} {
		if strings.Contains(labels[id].TargetLabel, "_internal") || !strings.Contains(labels[id].TargetLabel, "Proyek Literasi") {
			t.Fatalf("technical target: %+v", labels[id])
		}
	}
	if labels["round"].RoundNumber != 2 || labels["round"].SubjectName != "Bahasa Indonesia" || !strings.Contains(labels["round"].TargetLabel, "Putaran 2") {
		t.Fatalf("round context missing: %+v", labels["round"])
	}
	if labels["account"].TargetLabel != "" || labels["account"].ProjectName != "Proyek Literasi" {
		t.Fatalf("account identity overwritten: %+v", labels["account"])
	}
	if st.reads != 1 {
		t.Fatalf("repeated project reads: %d", st.reads)
	}
}
func TestAuditContextDeletedProjectNeverDisplaysInternalID(t *testing.T) {
	s := New(&auditContextStore{}, nil, "")
	rows := []domain.AuditEvent{{ID: "p", ProjectID: "project_deleted", TargetID: "project_deleted", Action: "project_copy"}, {ID: "r", ProjectID: "project_deleted", TargetID: "round_deleted", Action: "round_rollback"}}
	labels, err := s.AuditContexts(context.Background(), rows)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if labels[r.ID].TargetLabel == "" || strings.Contains(labels[r.ID].TargetLabel, "_deleted") {
			t.Fatalf("deleted record leaked identifier: %+v", labels[r.ID])
		}
	}
}

func TestWorkflowAuditTargetsUseSubjectAndSnapshotNames(t *testing.T) {
	st := &auditContextStore{project: domain.Project{ID: "p", Name: "Proyek QA", Subjects: []domain.ProjectSubject{{Code: "MAT", Name: "Matematika"}}}, rounds: []domain.Round{{ID: "round_internal", ProjectID: "p", SubjectCode: "MAT", Number: 1, FinalSnapshot: &domain.RoundSnapshot{ID: "snapshot_internal"}}}}
	events := []domain.AuditEvent{{ID: "submit", ProjectID: "p", TargetID: "round_internal", Action: "panelist_round_submit"}, {ID: "reopen", ProjectID: "p", TargetID: "round_internal", Action: "panelist_round_reopen"}, {ID: "nbk", ProjectID: "p", TargetID: "snapshot_internal", Action: "nbk_final_lock"}, {ID: "practice", ProjectID: "p", TargetID: "MAT", Action: "practice_end"}}
	labels, err := New(st, nil, "").AuditContexts(context.Background(), events)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		label := labels[e.ID].TargetLabel
		if !strings.Contains(label, "Matematika") || !strings.Contains(label, "Proyek QA") || strings.Contains(label, "internal") {
			t.Fatalf("workflow target missing: %s %+v", e.ID, labels[e.ID])
		}
	}
}
