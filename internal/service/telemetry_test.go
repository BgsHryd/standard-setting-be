package service

import (
	"context"
	"errors"
	"math"
	"standard-setting/backend/internal/domain"
	"testing"
	"time"
)

type telemetryMemory struct {
	Store
	user    domain.User
	project domain.Project
	round   domain.Round
	rows    map[string]domain.PanelistTelemetry
}

func (m *telemetryMemory) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (m *telemetryMemory) FindByID(context.Context, string) (domain.User, error) { return m.user, nil }
func (m *telemetryMemory) GetProject(context.Context, string) (domain.Project, error) {
	return m.project, nil
}
func (m *telemetryMemory) CurrentRound(context.Context, string, string) (domain.Round, error) {
	return m.round, nil
}
func (m *telemetryMemory) TouchRound(context.Context, domain.Round) error { return nil }
func (m *telemetryMemory) SaveTelemetry(_ context.Context, row domain.PanelistTelemetry) error {
	m.rows[row.ID] = row
	return nil
}
func (m *telemetryMemory) ListTelemetry(_ context.Context, p, sub string, n, rev int) ([]domain.PanelistTelemetry, error) {
	result := []domain.PanelistTelemetry{}
	for _, r := range m.rows {
		if r.ProjectID == p && r.SubjectCode == sub && r.RoundNumber == n && r.RoundRevision == rev {
			result = append(result, r)
		}
	}
	return result, nil
}
func telemetryFixture() (*Service, *telemetryMemory) {
	now := time.Now().UTC()
	m := &telemetryMemory{user: domain.User{ID: "u", Role: domain.RolePanelist, Active: true, ProjectID: "p", SubjectCode: "MAT"}, project: domain.Project{ID: "p", Status: domain.ProjectActive, StartDate: now.Add(-time.Hour), EndDate: now.Add(time.Hour), Subjects: []domain.ProjectSubject{{Code: "MAT", PanelistUserIDs: []string{"u"}}}}, round: domain.Round{ID: "r", Number: 1, Revision: 1, Status: "active"}, rows: map[string]domain.PanelistTelemetry{}}
	return New(m, nil, ""), m
}
func TestTelemetryRecordsServerReceiptAndDerivedWarnings(t *testing.T) {
	s, _ := telemetryFixture()
	before := time.Now().UTC()
	row, err := s.RecordTelemetry(context.Background(), "u", TelemetryInput{RoundNumber: 1, RoundRevision: 1, State: "online", TimingSamples: 3, MedianMs: 1600, AnswerTimingSeconds: []float64{1, 2, 3, 4, 5}})
	if err != nil {
		t.Fatal(err)
	}
	if row.LastActive.Before(before) || row.UserID != "u" || row.ProjectID != "p" || !row.ConnectionWarning || !row.SpeedWarning || row.MedianAnswerSeconds != 3 {
		t.Fatalf("unexpected telemetry: %+v", row)
	}
	rows, err := s.TelemetryForRound(context.Background(), "p", "MAT", 1, 1)
	if err != nil || len(rows) != 1 || rows["u"].ID != row.ID {
		t.Fatalf("lookup: %+v %v", rows, err)
	}
	other, _ := s.TelemetryForRound(context.Background(), "p", "MAT", 1, 2)
	if len(other) != 0 {
		t.Fatal("telemetry crossed revision")
	}
}
func TestTelemetryRejectsStaleAndUnassignedScope(t *testing.T) {
	for _, change := range []func(*telemetryMemory){func(m *telemetryMemory) { m.user.Active = false }, func(m *telemetryMemory) { m.user.Removed = true }, func(m *telemetryMemory) { m.project.Subjects[0].PanelistUserIDs = nil }, func(m *telemetryMemory) { m.round.Revision = 2 }, func(m *telemetryMemory) { m.round.Number = 2 }, func(m *telemetryMemory) { m.round.Status = "finalized" }, func(m *telemetryMemory) { m.project.Status = domain.ProjectCompleted }, func(m *telemetryMemory) { m.project.Subjects[0].PracticeStatus = "open" }, func(m *telemetryMemory) { m.project.Subjects[0].FinalNBK = &domain.NBKDecision{} }} {
		s, m := telemetryFixture()
		change(m)
		if _, err := s.RecordTelemetry(context.Background(), "u", TelemetryInput{RoundNumber: 1, RoundRevision: 1, State: "online"}); err == nil || len(m.rows) != 0 {
			t.Fatal("invalid scope persisted")
		}
	}
	s, _ := telemetryFixture()
	_, err := s.RecordTelemetry(context.Background(), "u", TelemetryInput{RoundNumber: 1, RoundRevision: 2, State: "online"})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expected revision conflict, got %v", err)
	}
}
func TestTelemetryBoundsAndEvidenceMinimum(t *testing.T) {
	base := TelemetryInput{RoundNumber: 1, RoundRevision: 1, State: "online"}
	for _, change := range []func(*TelemetryInput){func(i *TelemetryInput) { i.Failures = -1 }, func(i *TelemetryInput) { i.Failures = 21 }, func(i *TelemetryInput) { i.MedianMs = math.NaN() }, func(i *TelemetryInput) { i.OfflineSeconds = math.Inf(1) }, func(i *TelemetryInput) { i.State = "verified" }, func(i *TelemetryInput) { i.TimingSamples = 21 }, func(i *TelemetryInput) { i.AnswerTimingSeconds = []float64{0.1} }, func(i *TelemetryInput) { i.AnswerTimingSeconds = make([]float64, 21) }} {
		s, _ := telemetryFixture()
		i := base
		change(&i)
		if _, err := s.RecordTelemetry(context.Background(), "u", i); err == nil {
			t.Fatalf("accepted invalid input: %+v", i)
		}
	}
	for _, durations := range [][]float64{nil, {1, 1, 1, 1}, {1, 1, 1, 1, 6}, {1, 1, 1, 1, 6, 1, 1, 1, 1}} {
		s, _ := telemetryFixture()
		i := base
		i.TimingSamples = 2
		i.MedianMs = 2000
		i.AnswerTimingSeconds = durations
		r, err := s.RecordTelemetry(context.Background(), "u", i)
		if err != nil || r.ConnectionWarning || r.SpeedWarning {
			t.Fatalf("insufficient evidence: %+v %v", r, err)
		}
	}
}

func TestTelemetryMongoPersistenceRevisionIsolation(t *testing.T) {
	ctx, s, st, p := fixture(t, true)
	input := TelemetryInput{RoundNumber: 1, RoundRevision: 1, State: "online", TimingSamples: 3, MedianMs: 90}
	first, err := s.RecordTelemetry(ctx, "p1", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecordTelemetry(ctx, "p2", input); err != nil {
		t.Fatal(err)
	}
	input.Failures = 1
	input.State = "degraded"
	if _, err = s.RecordTelemetry(ctx, "p1", input); err != nil {
		t.Fatal(err)
	}
	current, err := st.CurrentRound(ctx, p.ID, "MAT")
	if err != nil {
		t.Fatal(err)
	}
	current.Revision = 2
	if err = st.SaveRound(ctx, current); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecordTelemetry(ctx, "p1", input); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale heartbeat accepted: %v", err)
	}
	input.RoundRevision = 2
	latest, err := s.RecordTelemetry(ctx, "p1", input)
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID == first.ID {
		t.Fatal("revision overwrote old record")
	}
	old, err := s.TelemetryForRound(ctx, p.ID, "MAT", 1, 1)
	if err != nil || len(old) != 2 || old["p1"].Failures != 1 {
		t.Fatalf("old revision: %+v %v", old, err)
	}
	fresh, err := s.TelemetryForRound(ctx, p.ID, "MAT", 1, 2)
	if err != nil || len(fresh) != 1 {
		t.Fatalf("new revision: %+v %v", fresh, err)
	}
	elsewhere, _ := s.TelemetryForRound(ctx, "other", "MAT", 1, 1)
	if len(elsewhere) != 0 {
		t.Fatal("cross project telemetry leaked")
	}
}

func TestTelemetryAcceptsConfiguredLaterRound(t *testing.T) {
	s, m := telemetryFixture()
	m.round.Number = 20
	if _, err := s.RecordTelemetry(context.Background(), "u", TelemetryInput{RoundNumber: 20, RoundRevision: 1, State: "online"}); err != nil {
		t.Fatal(err)
	}
}
