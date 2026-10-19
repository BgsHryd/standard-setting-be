package service

import (
	"standard-setting/backend/internal/domain"
	"testing"
)

func TestAssignmentRejectsDuplicatePanelistIDs(t *testing.T) {
	p := domain.Project{ID: "p"}
	users := []domain.User{{ID: "u1", Role: domain.RolePanelist, ProjectID: "p", SubjectCode: "MAT", Active: true}, {ID: "u2", Role: domain.RolePanelist, ProjectID: "p", SubjectCode: "MAT", Active: true}}
	subject := domain.ProjectSubject{Code: "MAT", PanelistCount: 2, PanelistUserIDs: []string{"u1", "u1"}}
	if _, err := assignedPanelists(p, subject, users); err == nil {
		t.Fatal("duplicate assignment counted as two unique panelists")
	}
	subject.PanelistUserIDs = []string{"u1", "u2"}
	if assigned, err := assignedPanelists(p, subject, users); err != nil || len(assigned) != 2 {
		t.Fatalf("valid unique assignments rejected: %+v %v", assigned, err)
	}
}

func TestInactivePanelistsAreExcludedWithoutBreakingMonitoring(t *testing.T) {
	p := domain.Project{ID: "p"}
	subject := domain.ProjectSubject{Code: "MAT", PanelistCount: 2, PanelistUserIDs: []string{"u1", "u2"}}
	users := []domain.User{{ID: "u1", Role: domain.RolePanelist, ProjectID: "p", SubjectCode: "MAT", Active: true}, {ID: "u2", Role: domain.RolePanelist, ProjectID: "p", SubjectCode: "MAT", Active: false}}
	assigned, err := assignedPanelists(p, subject, users)
	if err != nil || len(assigned) != 1 {
		t.Fatalf("inactive roster must not break monitoring: %v %v", assigned, err)
	}
}
