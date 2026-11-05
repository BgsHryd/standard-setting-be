package service

import (
	"errors"
	"testing"

	"standard-setting/backend/internal/domain"
)

func TestPanelistDisplayOwnScopeAndStaleVersion(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	p.Subjects = append(p.Subjects, domain.ProjectSubject{Code: "BIN", Name: "Bahasa Indonesia"})
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	input := PanelistDisplayInput{ProjectVersion: p.Version, ShowKJ: true}
	view, err := s.SetPanelistDisplay(ctx, "pic", input)
	if err != nil || !view.ShowKJ || view.ShowMeasure || view.ProjectVersion != p.Version+1 {
		t.Fatalf("PIC setting was not persisted: %+v %v", view, err)
	}
	after, _ := st.GetProject(ctx, p.ID)
	if !after.Subjects[0].ShowKJ || after.Subjects[0].ShowMeasure || after.Subjects[1].ShowKJ || after.Subjects[1].ShowMeasure || !after.AccountsGenerated {
		t.Fatal("configuration changed another subject or account readiness")
	}
	if _, err := s.SetPanelistDisplay(ctx, "pic", input); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale project version accepted")
	}
	input = PanelistDisplayInput{ProjectVersion: view.ProjectVersion, ShowMeasure: true}
	view, err = s.SetPanelistDisplay(ctx, "pic", input)
	if err != nil || view.ShowKJ || !view.ShowMeasure {
		t.Fatal("independent toggle update failed", err)
	}
	form, err := s.PanelistForm(ctx, "p1", p.ID, "MAT")
	if err != nil || form.Subject.ShowKJ || !form.Subject.ShowMeasure {
		t.Fatal("panelist did not receive updated visibility", err)
	}
	after, _ = st.GetProject(ctx, p.ID)
	after.Status = domain.ProjectCompleted
	if err := st.SaveProject(ctx, after); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetPanelistDisplay(ctx, "pic", PanelistDisplayInput{ProjectVersion: after.Version}); err == nil {
		t.Fatal("completed project settings were changed")
	}
}

func TestPanelistDisplayRejectsUnassignedOrInactiveActors(t *testing.T) {
	for _, kind := range []string{"panelist", "inactive", "removed", "unassigned", "wrong-project", "wrong-subject"} {
		t.Run(kind, func(t *testing.T) {
			ctx, s, st, p := fixture(t, true)
			id := "pic"
			u, _ := st.FindByID(ctx, id)
			switch kind {
			case "panelist":
				id = "p1"
			case "inactive":
				u.Active = false
			case "removed":
				u.Removed = true
			case "unassigned":
				p.Subjects[0].PICUserID = "different-pic"
				if err := st.SaveProject(ctx, p); err != nil {
					t.Fatal(err)
				}
			case "wrong-project":
				u.ProjectID = "other-project"
			case "wrong-subject":
				u.SubjectCode = "BIN"
			}
			if err := st.SaveUser(ctx, u); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SetPanelistDisplay(ctx, id, PanelistDisplayInput{ProjectVersion: p.Version, ShowKJ: true, ShowMeasure: true}); err == nil {
				t.Fatal("unauthorized display update accepted")
			}
			after, _ := st.GetProject(ctx, p.ID)
			if after.Version != p.Version || after.Subjects[0].ShowKJ || after.Subjects[0].ShowMeasure {
				t.Fatal("rejected update mutated project")
			}
		})
	}
}

func TestAdminProjectSavePreservesPICDisplaySettings(t *testing.T) {
	ctx, s, st, p := fixture(t, false)
	p.Subjects[0].ShowKJ, p.Subjects[0].ShowMeasure = true, true
	if err := st.SaveProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	// Older admin forms can omit both fields. Only the PIC endpoint controls them.
	p.Subjects[0].ShowKJ, p.Subjects[0].ShowMeasure = false, false
	saved, err := s.SaveProject(ctx, p, "admin")
	if err != nil || !saved.Subjects[0].ShowKJ || !saved.Subjects[0].ShowMeasure {
		t.Fatal("admin save reset PIC display settings", err)
	}
}
