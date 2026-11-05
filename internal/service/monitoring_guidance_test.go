package service

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"standard-setting/backend/internal/domain"
)

// Decode the public payload so these tests also cover null ranges and the additive contract.
type guidancePayload struct {
	ItemID string   `json:"itemId"`
	Range  *float64 `json:"range"`
	State  string   `json:"state"`
}

func monitoringGuidancePayload(t *testing.T, result Monitoring) []guidancePayload {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ItemGuidance []guidancePayload `json:"itemGuidance"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ItemGuidance == nil {
		t.Fatal("monitoring must return itemGuidance as an array")
	}
	return payload.ItemGuidance
}

func TestMonitoringGuidanceClassifiesUnroundedFinalRanges(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value float64
		state string
	}{
		{"zero", 0, "agreement"},
		{"below threshold", 19.999, "agreement"},
		{"at threshold", 20, "priority"},
		{"above threshold", 20.001, "priority"},
		{"full scale", 100, "priority"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, st := monitoringHistoryFixture()
			st.rounds[0].FinalSnapshot.ItemStatistics = map[string]domain.ItemResult{
				"i": {Minimum: 0, Maximum: tc.value, PanelistCount: 2},
			}
			result, err := s.MonitoringAtRound(context.Background(), "p", "MAT", 1)
			if err != nil {
				t.Fatal(err)
			}
			rows := monitoringGuidancePayload(t, result)
			if len(rows) != 1 || rows[0].ItemID != "i" || rows[0].State != tc.state || rows[0].Range == nil || *rows[0].Range != tc.value {
				t.Fatalf("unrounded range %v: got %+v", tc.value, rows)
			}
		})
	}
}

func TestMonitoringGuidanceWaitingOverridesStaleSnapshot(t *testing.T) {
	s, st := monitoringHistoryFixture()
	st.rounds[1].FinalSnapshot = &domain.RoundSnapshot{ItemStatistics: map[string]domain.ItemResult{
		"i": {Minimum: 0, Maximum: 100, PanelistCount: 2},
	}}
	result, err := s.MonitoringAtRound(context.Background(), "p", "MAT", 2)
	if err != nil {
		t.Fatal(err)
	}
	rows := monitoringGuidancePayload(t, result)
	if len(rows) != 1 || rows[0].State != "waiting" || rows[0].Range != nil {
		t.Fatalf("active rollback must wait despite old snapshot: %+v", rows)
	}
}

func TestMonitoringGuidanceUnavailableForMissingOrInvalidFinalStatistics(t *testing.T) {
	for _, tc := range []struct {
		name     string
		snapshot *domain.RoundSnapshot
	}{
		{"missing snapshot", nil},
		{"missing statistics", &domain.RoundSnapshot{}},
		{"missing item", &domain.RoundSnapshot{ItemStatistics: map[string]domain.ItemResult{"other": {PanelistCount: 2}}}},
		{"zero count", guidanceSnapshot(domain.ItemResult{Minimum: 20, Maximum: 30})},
		{"negative count", guidanceSnapshot(domain.ItemResult{Minimum: 20, Maximum: 30, PanelistCount: -1})},
		{"negative minimum", guidanceSnapshot(domain.ItemResult{Minimum: -1, Maximum: 30, PanelistCount: 2})},
		{"maximum above 100", guidanceSnapshot(domain.ItemResult{Minimum: 20, Maximum: 101, PanelistCount: 2})},
		{"reversed limits", guidanceSnapshot(domain.ItemResult{Minimum: 40, Maximum: 30, PanelistCount: 2})},
		{"minimum NaN", guidanceSnapshot(domain.ItemResult{Minimum: math.NaN(), Maximum: 30, PanelistCount: 2})},
		{"maximum NaN", guidanceSnapshot(domain.ItemResult{Minimum: 20, Maximum: math.NaN(), PanelistCount: 2})},
		{"minimum infinity", guidanceSnapshot(domain.ItemResult{Minimum: math.Inf(-1), Maximum: 30, PanelistCount: 2})},
		{"maximum infinity", guidanceSnapshot(domain.ItemResult{Minimum: 20, Maximum: math.Inf(1), PanelistCount: 2})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, st := monitoringHistoryFixture()
			st.rounds[0].FinalSnapshot = tc.snapshot
			result, err := s.MonitoringAtRound(context.Background(), "p", "MAT", 1)
			if err != nil {
				t.Fatal(err)
			}
			rows := monitoringGuidancePayload(t, result)
			if len(rows) != 1 || rows[0].State != "unavailable" || rows[0].Range != nil {
				t.Fatalf("invalid frozen statistics must be neutral: %+v", rows)
			}
		})
	}
}

func guidanceSnapshot(stats domain.ItemResult) *domain.RoundSnapshot {
	return &domain.RoundSnapshot{ItemStatistics: map[string]domain.ItemResult{"i": stats}}
}

func TestMonitoringGuidanceUsesOrderedFrozenItemStatisticsWithoutExposingSnapshot(t *testing.T) {
	s, st := monitoringHistoryFixture()
	st.project.Subjects[0].ItemIDs = []string{"second", "i", "missing"}
	st.items = append(st.items, domain.Item{ID: "second", QuestionType: domain.QuestionTypePG, MaxScore: 1}, domain.Item{ID: "missing", QuestionType: domain.QuestionTypePG, MaxScore: 1})
	st.rounds[0].FinalSnapshot.ItemStatistics = map[string]domain.ItemResult{
		"i":      {Minimum: 30, Maximum: 50, PanelistCount: 2},
		"second": {Minimum: 65, Maximum: 65, PanelistCount: 1},
	}
	result, err := s.MonitoringAtRound(context.Background(), "p", "MAT", 1)
	if err != nil {
		t.Fatal(err)
	}
	rows := monitoringGuidancePayload(t, result)
	if len(rows) != 3 || rows[0].ItemID != "second" || rows[1].ItemID != "i" || rows[2].ItemID != "missing" {
		t.Fatalf("guidance must match ordered project items: %+v", rows)
	}
	// Live submission is 99 and frozen submission is 65: neither can replace the per-item 30..50 statistics.
	if rows[0].Range == nil || *rows[0].Range != 0 || rows[0].State != "agreement" || rows[1].Range == nil || *rows[1].Range != 20 || rows[1].State != "priority" || rows[2].Range != nil || rows[2].State != "unavailable" {
		t.Fatalf("guidance must use frozen per-item statistics: %+v", rows)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"finalSnapshot", "snapshots", "itemStatistics", "submissions", "roster"} {
		if strings.Contains(string(data), `"`+private+`"`) {
			t.Fatalf("raw snapshot field %q exposed in monitoring", private)
		}
	}
}

func TestMonitoringGuidanceDoesNotRequireFrozenSubmissions(t *testing.T) {
	s, st := monitoringHistoryFixture()
	st.rounds[0].FinalSnapshot = guidanceSnapshot(domain.ItemResult{Minimum: 10, Maximum: 35, PanelistCount: 2})
	result, err := s.MonitoringAtRound(context.Background(), "p", "MAT", 1)
	if err != nil {
		t.Fatal(err)
	}
	rows := monitoringGuidancePayload(t, result)
	if len(rows) != 1 || rows[0].Range == nil || *rows[0].Range != 25 || rows[0].State != "priority" {
		t.Fatalf("valid final item statistics must not depend on submission availability: %+v", rows)
	}
}

func TestMonitoringGuidanceFutureRoundReturnsEmptyArray(t *testing.T) {
	s, _ := monitoringHistoryFixture()
	result, err := s.MonitoringAtRound(context.Background(), "p", "MAT", 3)
	if err != nil {
		t.Fatal(err)
	}
	if rows := monitoringGuidancePayload(t, result); len(rows) != 0 {
		t.Fatalf("future guidance must match its empty items: %+v", rows)
	}
}
