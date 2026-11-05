package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"standard-setting/backend/internal/domain"
)

type picRoundOverviewStore struct{ *monitoringHistoryStore }

func (st *picRoundOverviewStore) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func picRoundOverviewFixture() (*Service, *picRoundOverviewStore) {
	_, base := monitoringHistoryFixture()
	base.project.Name = "Proyek QA"
	base.project.Subjects[0].PICUserID = "pic"
	base.users = append(base.users, domain.User{ID: "pic", Name: "PIC rahasia", Role: domain.RolePICMapel, Active: true, ProjectID: "p", SubjectCode: "MAT"})
	snap := base.rounds[0].FinalSnapshot
	snap.ID = "snap-1"
	snap.RoundNumber = 1
	snap.Revision = 1
	snap.Statistics = domain.ComputeStatistics([]float64{65}, "ceil_1pp")
	snap.ItemStatistics = map[string]domain.ItemResult{"i": domain.ComputeStatistics([]float64{65}, "ceil_1pp")}
	base.items[0].SubjectCode = "MAT"
	st := &picRoundOverviewStore{base}
	return New(st, nil, ""), st
}

func TestPICRoundOverviewUsesSelectedFrozenResults(t *testing.T) {
	s, st := picRoundOverviewFixture()
	got, err := s.PICRoundOverview(context.Background(), "pic", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Round.Number != 1 || got.RoundReview.RoundNumber != 1 || got.RoundReview.NBKPercent != 65 || len(got.Items) != 1 || len(got.RoundReview.Items) != 1 || got.RoundReview.Items[0].Median != 65 || len(got.RoundOutcomes) != 1 {
		t.Fatal("wrong frozen round overview", got)
	}
	if got.RoundReview.Items[0].OwnValue != nil || len(got.RoundReview.Items[0].Values) != 0 {
		t.Fatal("small panel/own-score policy broken", got.RoundReview.Items)
	}
	encoded, _ := json.Marshal(got)
	for _, secret := range []string{"Panelis lama", "Panelis baru", "PIC rahasia", "submissions", "panelistIds", "password", "snap-1"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("overview leaked identity/raw snapshot", secret)
		}
	}
	st.rounds[0].Status = "active"
	if _, err = s.PICRoundOverview(context.Background(), "pic", 1); err == nil {
		t.Fatal("rollback exposed old snapshot")
	}
}

func TestPICRoundOverviewRejectsInvalidRoundAndWrongAssignment(t *testing.T) {
	for _, number := range []int{-1, 0, 2, 3, 99} {
		s, _ := picRoundOverviewFixture()
		if _, err := s.PICRoundOverview(context.Background(), "pic", number); err == nil {
			t.Fatal("non-final/missing round accepted", number)
		}
	}
	for _, change := range []string{"removed", "inactive", "role", "project", "subject", "assignment", "revision", "snapshot"} {
		t.Run(change, func(t *testing.T) {
			s, st := picRoundOverviewFixture()
			u := &st.users[len(st.users)-1]
			switch change {
			case "removed":
				u.Removed = true
			case "inactive":
				u.Active = false
			case "role":
				u.Role = domain.RolePanelist
			case "project":
				u.ProjectID = "elsewhere"
			case "subject":
				u.SubjectCode = "BIN"
			case "assignment":
				st.project.Subjects[0].PICUserID = "different"
			case "revision":
				st.rounds[0].Revision = 2
			case "snapshot":
				st.rounds[0].FinalSnapshot = nil
			}
			if _, err := s.PICRoundOverview(context.Background(), "pic", 1); err == nil {
				t.Fatal("invalid scope/snapshot accepted")
			}
		})
	}
}

func TestPICRoundOverviewIntegrationUsesFrozenSnapshot(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	fillRound(t, ctx, s, p, 1, 0)
	if _, err := s.RoundAction(ctx, p.ID, "MAT", "finalize", "FINALISASI PUTARAN", "pic", 1, 1); err != nil {
		t.Fatal(err)
	}
	got, err := s.PICRoundOverview(ctx, "pic", 1)
	if err != nil || got.RoundReview == nil || len(got.RoundReview.Items) != 3 || len(got.RoundOutcomes) != 1 || got.RoundReview.Items[0].Median != 70 {
		t.Fatal("wrong frozen overview", got, err)
	}
	history, err := st.ListRounds(ctx, p.ID, "MAT")
	if err != nil {
		t.Fatal(err)
	}
	active, err := st.CurrentRound(ctx, p.ID, "MAT")
	if err != nil || active.Number != 2 {
		t.Fatal("overview changed active round", active, err)
	}
	if len(history) != 2 {
		t.Fatal("overview wrote extra rounds", history)
	}
	if _, err = s.RoundAction(ctx, p.ID, "MAT", "rollback", "ROLLBACK PUTARAN", "pic", 2, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PICRoundOverview(ctx, "pic", 1); err == nil {
		t.Fatal("rollback did not invalidate overview")
	}
}
