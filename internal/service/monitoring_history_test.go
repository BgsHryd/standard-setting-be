package service

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
)

type monitoringHistoryStore struct {
	Store
	project domain.Project
	rounds  []domain.Round
	users   []domain.User
	items   []domain.Item
	rows    map[int][]domain.Submission
}

func (st *monitoringHistoryStore) GetProject(context.Context, string) (domain.Project, error) {
	return st.project, nil
}
func (st *monitoringHistoryStore) ListUsers(context.Context, string) ([]domain.User, error) {
	return st.users, nil
}
func (st *monitoringHistoryStore) ListRounds(context.Context, string, string) ([]domain.Round, error) {
	return append([]domain.Round(nil), st.rounds...), nil
}
func (st *monitoringHistoryStore) CurrentRound(context.Context, string, string) (domain.Round, error) {
	for _, r := range st.rounds {
		if r.Status == "active" {
			return r, nil
		}
	}
	return domain.Round{}, mongo.ErrNoDocuments
}
func (st *monitoringHistoryStore) ListItems(context.Context, string, string, bool) ([]domain.Item, error) {
	return st.items, nil
}
func (st *monitoringHistoryStore) ListSubmissions(_ context.Context, _, _ string, number int, user string) ([]domain.Submission, error) {
	rows := []domain.Submission{}
	for _, row := range st.rows[number] {
		if user == "" || row.PanelistID == user {
			rows = append(rows, row)
		}
	}
	return rows, nil
}
func (st *monitoringHistoryStore) FindByID(_ context.Context, id string) (domain.User, error) {
	for _, u := range st.users {
		if u.ID == id {
			return u, nil
		}
	}
	return domain.User{}, mongo.ErrNoDocuments
}

func monitoringHistoryFixture() (*Service, *monitoringHistoryStore) {
	frozen := domain.Submission{ID: "saved", ProjectID: "p", SubjectCode: "MAT", RoundNumber: 1, PanelistID: "u", ItemID: "i", Status: "saved", CandidateVersion: 1, RoundRevision: 1, ReviewRevision: 1, Answer: domain.Answer{QuestionType: domain.QuestionTypePG, Value: intValue(65)}}
	live := frozen
	live.RoundNumber, live.CandidateVersion, live.Answer.Value = 2, 2, intValue(75)
	changed := frozen
	changed.Answer.Value = intValue(99)
	st := &monitoringHistoryStore{
		project: domain.Project{ID: "p", Status: domain.ProjectActive, PlannedRounds: 3, FeatureVersion: 1, Subjects: []domain.ProjectSubject{{Code: "MAT", Name: "Matematika", CandidateVersion: 2, CandidateDefinition: "Definisi sekarang", ItemIDs: []string{"i"}, PanelistUserIDs: []string{"u", "new"}}}},
		rounds: []domain.Round{
			{ID: "r1", Number: 1, Revision: 1, Status: "finalized", FinalSnapshot: &domain.RoundSnapshot{CandidateVersion: 1, CandidateDefinition: "Definisi saat finalisasi", PanelistIDs: []string{"u"}, Submissions: []domain.Submission{frozen}, Statistics: domain.ItemResult{Cutoff: 65, Mean: 65}}},
			{ID: "r2", Number: 2, Revision: 1, Status: "active"},
		},
		users: []domain.User{{ID: "u", Name: "Panelis lama", Role: domain.RolePanelist, ProjectID: "p", SubjectCode: "MAT", Active: true}, {ID: "new", Name: "Panelis baru", Role: domain.RolePanelist, ProjectID: "p", SubjectCode: "MAT", Active: true}},
		items: []domain.Item{{ID: "i", QuestionType: domain.QuestionTypePG, MaxScore: 1}},
		rows:  map[int][]domain.Submission{1: {changed}, 2: {live}},
	}
	return New(st, nil, ""), st
}

func TestMonitoringHistoryShowsFrozenRoundWhileNextRoundIsActive(t *testing.T) {
	s, _ := monitoringHistoryFixture()
	historical, err := s.MonitoringAtRound(context.Background(), "p", "MAT", 1)
	if err != nil {
		t.Fatal(err)
	}
	if historical.Round.Number != 1 || historical.Round.Status != "finalized" || len(historical.Panelists) != 1 || historical.Panelists[0].Done != 1 || historical.Panelists[0].Status != "Selesai" {
		t.Fatalf("historical roster/progress changed: %+v", historical)
	}
	if historical.Subject.CandidateVersion != 1 || historical.Statistics == nil || historical.Statistics.Cutoff != 65 {
		t.Fatal("historical context/statistics not taken from final snapshot")
	}
	preview, err := s.PreviewPanelist(context.Background(), "p", "MAT", 1, "u")
	if err != nil || len(preview) != 1 || *preview[0].Answer.Value != 65 {
		t.Fatal("historical preview did not use final responses", err)
	}
	current, err := s.MonitoringAtRound(context.Background(), "p", "MAT", 0)
	if err != nil || current.Round.Number != 2 || len(current.Panelists) != 2 || current.TotalDone != 1 {
		t.Fatal("latest round monitoring was changed", err)
	}
}

func TestMonitoringHistoryFutureRoundIsEmptyAndBounded(t *testing.T) {
	s, _ := monitoringHistoryFixture()
	future, err := s.MonitoringAtRound(context.Background(), "p", "MAT", 3)
	if err != nil || future.Round.Number != 3 || future.Round.Status != "future" || len(future.Panelists) != 0 || future.Statistics != nil || future.TotalDone != 0 {
		t.Fatal("future round reused live or final data", err)
	}
	for _, n := range []int{-1, 4, 100} {
		if _, err := s.MonitoringAtRound(context.Background(), "p", "MAT", n); err == nil {
			t.Fatal("invalid/unplanned round accepted", n)
		}
	}
}
