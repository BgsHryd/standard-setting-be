package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"standard-setting/backend/internal/domain"
)

type picResultsStore struct{ *monitoringHistoryStore }

func (st *picResultsStore) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func TestPICItemResultsUsesFrozenValuesAndOwnScope(t *testing.T) {
	s, st := monitoringHistoryFixture()
	s.store = &picResultsStore{st}
	st.project.Subjects[0].PICUserID = "pic"
	st.users = append(st.users, domain.User{ID: "pic", Name: "PIC QA", Role: domain.RolePICMapel, Active: true, ProjectID: "p", SubjectCode: "MAT"})
	result, err := s.PICItemResults(context.Background(), "pic", 1)
	if err != nil || result.Round.Number != 1 || len(result.Panelists) != 1 || len(result.Items) != 1 {
		t.Fatal("missing frozen matrix", result, err)
	}
	if result.Items[0].Values["u"] != 65 {
		t.Fatal("live mutation leaked into final view", result.Items)
	}
	data, _ := json.Marshal(result)
	for _, private := range []string{"password", "whatsapp", "email", "submissions", "snapshots"} {
		if strings.Contains(strings.ToLower(string(data)), private) {
			t.Fatal("unneeded private fields", private)
		}
	}
	for _, id := range []string{"u", "unknown"} {
		if _, err := s.PICItemResults(context.Background(), id, 1); err == nil {
			t.Fatal("wrong actor accepted", id)
		}
	}
	st.users[len(st.users)-1].Active = false
	if _, err := s.PICItemResults(context.Background(), "pic", 1); err == nil {
		t.Fatal("inactive PIC accepted")
	}
}

func TestPICItemResultsFiltersCurrentResponsesAndUnknownRounds(t *testing.T) {
	s, st := monitoringHistoryFixture()
	s.store = &picResultsStore{st}
	st.project.Subjects[0].PICUserID = "pic"
	st.users = append(st.users, domain.User{ID: "pic", Role: domain.RolePICMapel, Active: true, ProjectID: "p", SubjectCode: "MAT"})
	result, err := s.PICItemResults(context.Background(), "pic", 2)
	if err != nil || len(result.Panelists) != 2 || result.Items[0].Values["u"] != 75 {
		t.Fatal("current matrix wrong", result, err)
	}
	st.rows[2][0].CandidateVersion = 1
	result, err = s.PICItemResults(context.Background(), "pic", 2)
	if err != nil || len(result.Items[0].Values) != 0 {
		t.Fatal("stale candidate response counted", result, err)
	}
	st.users[0].Active = false
	result, err = s.PICItemResults(context.Background(), "pic", 2)
	if err != nil || len(result.Panelists) != 1 || result.Panelists[0].ID != "new" {
		t.Fatal("inactive panelist visible", result, err)
	}
	for _, round := range []int{-1, 3, 99} {
		if _, err := s.PICItemResults(context.Background(), "pic", round); err == nil {
			t.Fatal("unavailable round accepted", round)
		}
	}
}

func TestPICItemResultsRejectsOtherPICAndExcludesRoundParticipants(t *testing.T) {
	s, st := monitoringHistoryFixture()
	s.store = &picResultsStore{st}
	st.users = append(st.users, domain.User{ID: "pic", Role: domain.RolePICMapel, Active: true, ProjectID: "p", SubjectCode: "MAT"})
	st.project.Subjects[0].PICUserID = "another-pic"
	if _, err := s.PICItemResults(context.Background(), "pic", 2); err == nil {
		t.Fatal("unassigned PIC accepted")
	}
	st.project.Subjects[0].PICUserID = "pic"
	st.rounds[1].ExcludedPanelists = []domain.RoundExclusion{{PanelistID: "u"}}
	result, err := s.PICItemResults(context.Background(), "pic", 2)
	if err != nil || len(result.Panelists) != 1 || result.Panelists[0].ID != "new" || len(result.Items[0].Values) != 0 {
		t.Fatal("excluded panelist leaked", result, err)
	}
	st.rounds[1].Status = "rolled_back"
	if _, err := s.PICItemResults(context.Background(), "pic", 2); err == nil {
		t.Fatal("rolled back round shown as current")
	}
}

func TestPICItemResultsOmitsInvalidUnstoredAndFutureRevisionValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*domain.Submission)
	}{
		{"draft", func(row *domain.Submission) { row.Status = "draft" }},
		{"invalid answer", func(row *domain.Submission) { row.Answer.Value = intValue(101) }},
		{"future revision", func(row *domain.Submission) { row.RoundRevision = 99 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, st := monitoringHistoryFixture()
			s.store = &picResultsStore{st}
			st.project.Subjects[0].PICUserID = "pic"
			st.users = append(st.users, domain.User{ID: "pic", Role: domain.RolePICMapel, Active: true, ProjectID: "p", SubjectCode: "MAT"})
			tc.mutate(&st.rows[2][0])
			result, err := s.PICItemResults(context.Background(), "pic", 2)
			if err != nil || len(result.Items[0].Values) != 0 {
				t.Fatal("invalid response counted", result, err)
			}
		})
	}
}
