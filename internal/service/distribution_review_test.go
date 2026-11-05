package service

import (
	"encoding/json"
	"math"
	"reflect"
	"standard-setting/backend/internal/domain"
	"strings"
	"testing"
)

func distributionSnapshot() domain.RoundSnapshot {
	snap := domain.RoundSnapshot{RoundNumber: 1, Revision: 1, PanelistIDs: []string{"panel-a", "panel-b", "panel-c"}, ItemStatistics: map[string]domain.ItemResult{"item-x": {PanelistCount: 3}}}
	for i, counts := range [][]int{{10, 20, 30, 40}, {0, 20, 50, 30}, {20, 10, 40, 30}, {100, 0, 0, 0}} {
		id := []string{"panel-a", "panel-b", "panel-c", "excluded-panel"}[i]
		snap.Submissions = append(snap.Submissions, domain.Submission{ItemID: "item-x", PanelistID: id, Status: "saved", Answer: domain.Answer{QuestionType: domain.QuestionTypeComplex, Distribution: counts}})
	}
	return snap
}
func TestDistributionReviewReturnsFrozenMeanAndOwnOnly(t *testing.T) {
	snap := distributionSnapshot()
	row := snapshotReviewItem(snap, "item-x", "panel-a")
	if !reflect.DeepEqual(row.OwnDistribution, []int{10, 20, 30, 40}) {
		t.Fatalf("wrong own distribution: %v", row.OwnDistribution)
	}
	expected := []float64{10, 50.0 / 3, 40, 100.0 / 3}
	if len(row.PanelDistribution) != len(expected) {
		t.Fatal("missing panel distribution", row.PanelDistribution)
	}
	for i, want := range expected {
		if math.Abs(row.PanelDistribution[i]-want) > 1e-10 {
			t.Fatalf("wrong mean[%d]=%v want%v", i, row.PanelDistribution[i], want)
		}
	}
	encoded, _ := json.Marshal(row)
	for _, private := range []string{"panel-a", "panel-b", "panel-c", "excluded-panel", "submissions", "panelistIds"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("identity leaked", private)
		}
	}
	row.OwnDistribution[0] = 99
	if snap.Submissions[0].Answer.Distribution[0] != 10 {
		t.Fatal("response mutated frozen snapshot")
	}
}
func TestDistributionReviewWithholdsSmallOrIncompletePanel(t *testing.T) {
	for _, test := range []string{"small", "missing", "duplicate", "invalid", "shape", "mixed"} {
		t.Run(test, func(t *testing.T) {
			snap := distributionSnapshot()
			switch test {
			case "small":
				snap.ItemStatistics["item-x"] = domain.ItemResult{PanelistCount: 2}
				snap.PanelistIDs = snap.PanelistIDs[:2]
			case "missing":
				snap.Submissions = append(snap.Submissions[:2], snap.Submissions[3:]...)
			case "duplicate":
				snap.Submissions = append(snap.Submissions, snap.Submissions[1])
			case "invalid":
				snap.Submissions[1].Answer.Distribution = []int{0, 0, 0, 99}
			case "shape":
				snap.Submissions[1].Answer.Distribution = []int{0, 0, 100}
			case "mixed":
				snap.Submissions[1].Answer.QuestionType = domain.QuestionTypeEssay
			}
			row := snapshotReviewItem(snap, "item-x", "panel-a")
			if len(row.PanelDistribution) != 0 {
				t.Fatal("unsafe/incomplete aggregate exposed", row.PanelDistribution)
			}
			if len(row.OwnDistribution) != 4 {
				t.Fatal("own valid snapshot answer lost")
			}
		})
	}
}
func TestDistributionReviewDoesNotInventOwnOrPGDistributions(t *testing.T) {
	snap := distributionSnapshot()
	if len(snapshotReviewItem(snap, "item-x", "unassigned").OwnDistribution) != 0 {
		t.Fatal("another user's own response exposed")
	}
	value := 60
	for i := range snap.Submissions {
		snap.Submissions[i].Answer = domain.Answer{QuestionType: domain.QuestionTypePG, Value: &value}
	}
	row := snapshotReviewItem(snap, "item-x", "panel-a")
	if len(row.OwnDistribution) != 0 || len(row.PanelDistribution) != 0 {
		t.Fatal("PG distribution invented")
	}
}
