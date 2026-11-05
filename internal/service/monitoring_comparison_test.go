package service

import (
	"context"
	"encoding/json"
	"standard-setting/backend/internal/domain"
	"strings"
	"testing"
)

func TestMonitoringItemRoundsUseFrozenAndValidLiveStatistics(t *testing.T) {
	s, st := monitoringHistoryFixture()
	st.rounds[0].FinalSnapshot.ItemStatistics = map[string]domain.ItemResult{"i": {Mean: 60, Minimum: 40, Maximum: 80, PanelistCount: 2, StandardDev: 20}}
	rounds, err := s.monitoringItemRounds(context.Background(), st.project, st.project.Subjects[0], st.rounds, st.items, st.users)
	if err != nil || len(rounds) != 2 {
		t.Fatal(rounds, err)
	}
	first, live := rounds[0].Items[0], rounds[1].Items[0]
	if first.Mean == nil || *first.Mean != 60 || *first.Range != 40 {
		t.Fatal("final statistics replaced by live answers", first)
	}
	if live.Mean == nil || *live.Mean != 75 || live.N != 1 || live.SD != nil {
		t.Fatal("live statistics incorrect", live)
	}
	st.rows[2][0].CandidateVersion = 1
	rounds, err = s.monitoringItemRounds(context.Background(), st.project, st.project.Subjects[0], st.rounds, st.items, st.users)
	if err != nil || rounds[1].Items[0].N != 0 || rounds[1].Items[0].Mean != nil {
		t.Fatal("stale candidate response counted", rounds, err)
	}
}
func TestMonitoringItemRoundsIgnoreRolledBackSnapshotsAndPrivateData(t *testing.T) {
	s, st := monitoringHistoryFixture()
	st.rounds[0].Status = "rolled_back"
	rounds, err := s.monitoringItemRounds(context.Background(), st.project, st.project.Subjects[0], st.rounds, st.items, st.users)
	if err != nil || rounds[0].Items[0].N != 0 || rounds[0].Items[0].Mean != nil {
		t.Fatal(rounds, err)
	}
	encoded, _ := json.Marshal(rounds)
	for _, private := range []string{"panelistId", "submissions", "roster", "snapshots"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("private data leaked", private)
		}
	}
}
