package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"standard-setting/backend/internal/domain"
)

// Thresholds describe observations only and do not evaluate answer quality.
const TelemetrySlowRequestMs = 1500
const TelemetryFastAnswerSeconds = 5
const TelemetryMinAnswerSamples = 5

type TelemetryInput struct {
	RoundNumber         int       `json:"roundNumber"`
	RoundRevision       int       `json:"roundRevision"`
	State               string    `json:"state"`
	Failures            int       `json:"failures"`
	MedianMs            float64   `json:"medianMs"`
	TimingSamples       int       `json:"timingSamples"`
	OfflineSeconds      float64   `json:"offlineSeconds"`
	AnswerTimingSeconds []float64 `json:"answerTimingSeconds,omitempty"`
}
type telemetryStore interface {
	SaveTelemetry(context.Context, domain.PanelistTelemetry) error
	ListTelemetry(context.Context, string, string, int, int) ([]domain.PanelistTelemetry, error)
}

func finiteRange(n, min, max float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= min && n <= max
}
func medianTelemetry(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}
func (s *Service) RecordTelemetry(ctx context.Context, userID string, input TelemetryInput) (domain.PanelistTelemetry, error) {
	var result domain.PanelistTelemetry
	if input.RoundNumber < 1 || input.RoundNumber > 20 || input.RoundRevision < 1 || input.Failures < 0 || input.Failures > 20 || input.TimingSamples < 0 || input.TimingSamples > 20 || !finiteRange(input.MedianMs, 0, 120000) || !finiteRange(input.OfflineSeconds, 0, 86400) || (input.State != "online" && input.State != "degraded" && input.State != "offline") || len(input.AnswerTimingSeconds) > 20 {
		return result, errors.New("metrik telemetri tidak valid")
	}
	for _, v := range input.AnswerTimingSeconds {
		if !finiteRange(v, 0.2, 86400) {
			return result, errors.New("durasi isian tidak valid")
		}
	}
	st, ok := s.store.(telemetryStore)
	if !ok {
		return result, errors.New("penyimpanan telemetri tidak tersedia")
	}
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, err := s.store.FindByID(tx, userID)
		if err != nil {
			return err
		}
		if !u.Active || u.Removed || u.Role != domain.RolePanelist {
			return errors.New("panelis aktif wajib ditugaskan")
		}
		p, err := s.store.GetProject(tx, u.ProjectID)
		if err != nil {
			return err
		}
		sub, err := subjectOf(p, u.SubjectCode)
		if err != nil || !contains(sub.PanelistUserIDs, u.ID) {
			return errors.New("panelis tidak ditugaskan pada proyek/mapel ini")
		}
		if sub.PracticeStatus == "open" || sub.FinalNBK != nil {
			return errors.New("telemetri hanya tersedia pada penilaian nyata yang aktif")
		}
		now := time.Now().UTC()
		if !domain.ProjectWritable(p, now) {
			return ErrPeriodClosed
		}
		r, err := s.store.CurrentRound(tx, p.ID, u.SubjectCode)
		if err != nil {
			return err
		}
		if r.Status != "active" || r.Number != input.RoundNumber || domain.RoundRevision(r) != input.RoundRevision {
			return ErrRevisionConflict
		}
		for _, ex := range r.ExcludedPanelists {
			if ex.PanelistID == u.ID {
				return errors.New("panelis dikeluarkan dari putaran ini")
			}
		}
		// Serialize with round transitions; the transaction prevents an old scope from racing rollback.
		if err = s.store.TouchRound(tx, r); err != nil {
			return err
		}
		trailingFast := 0
		for _, v := range input.AnswerTimingSeconds {
			if v <= TelemetryFastAnswerSeconds {
				trailingFast++
			} else {
				trailingFast = 0
			}
		}
		result = domain.PanelistTelemetry{
			ID:                  fmt.Sprintf("telemetry:%d:%s:%d:%s:%d:%d:%s", len(p.ID), p.ID, len(u.SubjectCode), u.SubjectCode, r.Number, domain.RoundRevision(r), u.ID),
			UserID:              u.ID,
			ProjectID:           p.ID,
			SubjectCode:         u.SubjectCode,
			RoundNumber:         r.Number,
			RoundRevision:       domain.RoundRevision(r),
			State:               input.State,
			Failures:            input.Failures,
			MedianMs:            input.MedianMs,
			TimingSamples:       input.TimingSamples,
			OfflineSeconds:      input.OfflineSeconds,
			ConnectionWarning:   input.TimingSamples >= 3 && input.MedianMs >= TelemetrySlowRequestMs,
			AnswerTimingSamples: len(input.AnswerTimingSeconds),
			MedianAnswerSeconds: medianTelemetry(input.AnswerTimingSeconds),
			SpeedWarning:        trailingFast >= TelemetryMinAnswerSamples,
			LastActive:          now,
		}
		return st.SaveTelemetry(tx, result)
	})
	return result, err
}
func (s *Service) TelemetryForRound(ctx context.Context, project, subject string, number, revision int) (map[string]domain.PanelistTelemetry, error) {
	result := map[string]domain.PanelistTelemetry{}
	st, ok := s.store.(telemetryStore)
	if !ok {
		return result, nil
	}
	rows, err := st.ListTelemetry(ctx, project, subject, number, revision)
	for _, row := range rows {
		result[row.UserID] = row
	}
	return result, err
}
