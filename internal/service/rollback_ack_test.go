package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"standard-setting/backend/internal/domain"
)

type rollbackAckStore struct {
	Store
	project domain.Project
	user    domain.User
	round   domain.Round
	row     domain.Submission
}

func (st *rollbackAckStore) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (st *rollbackAckStore) FindByID(context.Context, string) (domain.User, error) {
	return st.user, nil
}
func (st *rollbackAckStore) GetProject(context.Context, string) (domain.Project, error) {
	return st.project, nil
}
func (st *rollbackAckStore) CurrentRound(context.Context, string, string) (domain.Round, error) {
	return st.round, nil
}
func (st *rollbackAckStore) ListSubmissions(context.Context, string, string, int, string) ([]domain.Submission, error) {
	return []domain.Submission{st.row}, nil
}
func (st *rollbackAckStore) GetItem(context.Context, string) (domain.Item, error) {
	return domain.Item{ID: "one", ProjectID: "p", SubjectCode: "MAT", QuestionType: domain.QuestionTypePG, MaxScore: 1, UsageStatus: domain.ItemAllocated}, nil
}
func (st *rollbackAckStore) TouchProject(context.Context, string) error { return nil }
func (st *rollbackAckStore) SaveRound(_ context.Context, round domain.Round) error {
	st.round = round
	return nil
}

func TestRollbackAllowsOneRoundSubmissionWithoutItemAcknowledgements(t *testing.T) {
	now := time.Now().UTC()
	value := 65
	st := &rollbackAckStore{
		project: domain.Project{ID: "p", Status: domain.ProjectActive, FeatureVersion: 1, WorkflowVersion: 1,
			StartDate: now.Add(-time.Hour), EndDate: now.Add(time.Hour),
			Subjects: []domain.ProjectSubject{{Code: "MAT", CandidateVersion: 7, ItemIDs: []string{"one"}, PanelistUserIDs: []string{"u"}}}},
		user:  domain.User{ID: "u", Role: domain.RolePanelist, Active: true, ProjectID: "p", SubjectCode: "MAT"},
		round: domain.Round{ID: "r", ProjectID: "p", SubjectCode: "MAT", Number: 1, Revision: 2, Status: "active"},
		row: domain.Submission{ID: "s", ProjectID: "p", SubjectCode: "MAT", PanelistID: "u", ItemID: "one", RoundNumber: 1,
			RoundRevision: 1, ReviewRevision: 1, CandidateVersion: 7, Version: 5, Status: "saved",
			Answer: domain.Answer{QuestionType: domain.QuestionTypePG, Value: &value}},
	}
	s := New(st, nil, "")
	if _, err := s.SaveSubmissionWithOptions(context.Background(), "u", "", "p", "MAT", 1, "one", st.row.Answer,
		SubmissionOptions{RoundRevision: 1, CandidateVersion: 7, Version: 5}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale pre-rollback queue accepted: %v", err)
	}
	receipt, err := s.SubmitPanelistRound(context.Background(), "u", 1, 2, true)
	if err != nil || receipt.SubmittedAt == nil || receipt.RoundRevision != 2 || receipt.Versions["one"] != 5 {
		t.Fatalf("one round submission should accept saved rollback answers: %+v %v", receipt, err)
	}
	if st.row.Version != 5 || *st.row.Answer.Value != 65 {
		t.Fatal("round submission changed saved answer")
	}
	st.row.CandidateVersion = 6
	if _, err := s.SubmitPanelistRound(context.Background(), "u", 1, 2, true); err == nil {
		t.Fatal("outdated candidate context was accepted")
	}
}
