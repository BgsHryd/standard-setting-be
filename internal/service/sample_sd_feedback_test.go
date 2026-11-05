package service

import (
	"math"
	"standard-setting/backend/internal/domain"
	"testing"
)

func TestHistoricalFeedbackUsesSampleSD(t *testing.T) {
	snap := domain.RoundSnapshot{ItemStatistics: map[string]domain.ItemResult{"item": {PanelistCount: 5, StandardDev: math.Sqrt(26.8 / 5)}}}
	feedback := snapshotFeedback(snap, "item", "user")
	if feedback.StandardDeviation == nil || math.Abs(*feedback.StandardDeviation-math.Sqrt(26.8/4)) > 1e-12 {
		t.Fatal(feedback)
	}
	if snap.ItemStatistics["item"].StandardDev != math.Sqrt(26.8/5) {
		t.Fatal("snapshot mutated")
	}
	snap.ItemStatistics["item"] = domain.ItemResult{PanelistCount: 1}
	if got := snapshotFeedback(snap, "item", "user"); got.StandardDeviation != nil {
		t.Fatal("n1 SD must be unavailable", got)
	}
}
