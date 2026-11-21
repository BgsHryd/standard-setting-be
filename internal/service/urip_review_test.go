package service

import (
	"context"
	"math"
	"standard-setting/backend/internal/domain"
	"strings"
	"testing"
)

func TestSinglePGKNormalization(t *testing.T) {
	for _, kind := range []domain.QuestionType{domain.QuestionTypeComplex, domain.QuestionTypeComplexMCMA, domain.QuestionTypeComplexBS} {
		item := domain.Item{QuestionType: kind, MaxScore: 3}
		for _, value := range []int{0, 2, 3} {
			a := domain.Answer{QuestionType: kind, Value: &value}
			if err := validateItemAnswer(item, a); err != nil {
				t.Fatal(err)
			}
			if math.Abs(responseValue(a, 3)-100*float64(value)/3) > 1e-10 {
				t.Fatal("incorrect normalization")
			}
		}
		value := 4
		if validateItemAnswer(item, domain.Answer{QuestionType: kind, Value: &value}) == nil {
			t.Fatal("accepted above max")
		}
		if validateItemAnswer(item, domain.Answer{QuestionType: kind, Distribution: []int{10, 20, 30, 40}}) != nil {
			t.Fatal("legacy rejected")
		}
	}
}
func TestFrozenSinglePGKMetadata(t *testing.T) {
	value := 2
	snap := domain.RoundSnapshot{Revision: 1, PanelistIDs: []string{"p"}, Submissions: []domain.Submission{{PanelistID: "p", ItemID: "i", Status: "saved", RoundRevision: 1, ReviewRevision: 1, Answer: domain.Answer{QuestionType: domain.QuestionTypeComplex, Value: &value, MaxScore: 3}}}}
	items, values, err := frozenScoringResponses(snap)
	if err != nil || items["i"].MaxScore != 3 || math.Abs(values["p"]["i"]-200.0/3) > 1e-10 {
		t.Fatalf("%v %v %v", items, values, err)
	}
}

func TestSaveSinglePGKStampsServerMaximum(t *testing.T) {
	ctx, s, _, p := fixture(t, true)
	value := 2
	answer := domain.Answer{QuestionType: domain.QuestionTypeComplex, Value: &value, MaxScore: 99}
	saved, err := s.SaveSubmission(ctx, "p1", "A", p.ID, "MAT", 1, "MAT-002", answer)
	if err != nil || saved.Answer.MaxScore != 3 {
		t.Fatalf("%+v %v", saved, err)
	}
}

func TestMonitoringReviewUsesCompleteReviewedCohort(t *testing.T) {
	s, st := monitoringHistoryFixture()
	got, err := s.MonitoringAtRound(context.Background(), "p", "MAT", 0)
	if err != nil || got.AngoffReview.ActivePanelistCount != 2 || got.AngoffReview.CompletePanelistCount != 1 || got.AngoffReview.Ready || got.StandardSettingError.SEJ != nil {
		t.Fatalf("%+v %v", got.AngoffReview, err)
	}
	st.rows[2][0].RoundRevision = 2
	got, err = s.MonitoringAtRound(context.Background(), "p", "MAT", 0)
	if err != nil || got.AngoffReview.CompletePanelistCount != 0 {
		t.Fatalf("unreviewed counted %+v %v", got.AngoffReview, err)
	}
}
func TestMonitoringFrozenReviewRetainsRemovedCohort(t *testing.T) {
	s, st := monitoringHistoryFixture()
	st.users[0].Removed = true
	st.rounds[0].FinalSnapshot.ItemStatistics = map[string]domain.ItemResult{"i": {}}
	st.project.Subjects[0].SEM = new(float64)
	got, err := s.MonitoringAtRound(context.Background(), "p", "MAT", 1)
	if err != nil || got.AngoffReview.ActivePanelistCount != 1 || !got.AngoffReview.Ready || got.AngoffReview.Modified.MeanSum == nil || *got.AngoffReview.Modified.MeanSum != .65 || got.StandardSettingError.SEM != nil || !strings.Contains(got.StandardSettingError.Assumption, "SEM historis tidak direkam") {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestExtendedAngoffWriteAndHistoricalRead(t *testing.T) {
	for _, kind := range []domain.QuestionType{domain.QuestionTypeEssay, domain.QuestionTypeComplex, domain.QuestionTypeComplexBS, domain.QuestionTypeComplexMCMA} {
		item := domain.Item{QuestionType: kind, MaxScore: 2}
		legacy := domain.Answer{QuestionType: kind, Distribution: []int{0, 50, 50}}
		if err := validateItemAnswer(item, legacy); err != nil || responseValue(legacy, 2) != 75 {
			t.Fatal("historical expected score lost", kind, err)
		}
		if preparePanelistAnswer(item, &legacy) == nil {
			t.Fatal("new distribution accepted", kind)
		}
		for _, score := range []int{-1, 0, 1, 2, 3} {
			answer := domain.Answer{QuestionType: kind, Value: &score, MaxScore: 99}
			err := preparePanelistAnswer(item, &answer)
			if (err == nil) != (score >= 0 && score <= 2) || answer.MaxScore != 2 {
				t.Fatal(kind, score, err)
			}
		}
	}
}
