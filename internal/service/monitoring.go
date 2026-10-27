package service

import (
	"context"
	"errors"
	"go.mongodb.org/mongo-driver/mongo"
	"time"

	"standard-setting/backend/internal/domain"
)

type MonitoringPanelist struct {
	Telemetry    *domain.PanelistTelemetry `json:"telemetry,omitempty"`
	SubmittedAt  *time.Time                `json:"submittedAt"`
	ID           string                    `json:"id"`
	Name         string                    `json:"name"`
	Username     string                    `json:"username"`
	Done         int                       `json:"done"`
	Total        int                       `json:"total"`
	Status       string                    `json:"status"`
	LastActive   *time.Time                `json:"lastActive"`
	WhatsApp     string                    `json:"whatsapp"`
	Email        string                    `json:"email"`
	ContactValid bool                      `json:"contactValid"`
	NeedsReview  int                       `json:"needsReview"`
}

var errRoundUnavailable = errors.New("putaran tidak ditemukan")

type MonitoringExclusion struct {
	domain.RoundExclusion
	Name string `json:"name"`
}
type MonitoringNote struct {
	PanelistID   string    `json:"panelistId"`
	PanelistName string    `json:"panelistName"`
	ItemID       string    `json:"itemId"`
	Note         string    `json:"note"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
type Monitoring struct {
	FinalNBKChoices []FinalNBKChoice      `json:"finalNBKChoices"`
	Project         domain.Project        `json:"project"`
	Subject         domain.ProjectSubject `json:"subject"`
	Round           domain.Round          `json:"round"`
	History         []domain.Round        `json:"history"`
	Panelists       []MonitoringPanelist  `json:"panelists"`
	Items           []domain.Item         `json:"items"`
	Statistics      *domain.ItemResult    `json:"statistics"`
	TotalDone       int                   `json:"totalDone"`
	TotalItems      int                   `json:"totalItems"`
	CanFinalize     bool                  `json:"canFinalize"`
	CanRollback     bool                  `json:"canRollback"`
	Excluded        []MonitoringExclusion `json:"excluded"`
	Notes           []MonitoringNote      `json:"notes"`
}

func (s *Service) displayRound(ctx context.Context, p domain.Project, subjectCode string, history []domain.Round) (domain.Round, error) {
	if p.Status == domain.ProjectActive {
		r, err := s.store.CurrentRound(ctx, p.ID, subjectCode)
		if err == nil {
			return r, nil
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return r, err
		}
	}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Status == "finalized" {
			return history[i], nil
		}
	}
	return domain.Round{}, errors.New("belum ada putaran penilaian pada proyek")
}

func (s *Service) Monitoring(ctx context.Context, projectID, subjectCode string) (Monitoring, error) {
	return s.monitoringRound(ctx, projectID, subjectCode, 0)
}
func (s *Service) monitoringRound(ctx context.Context, projectID, subjectCode string, requested int) (Monitoring, error) {
	p, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return Monitoring{}, err
	}
	if p.Status == domain.ProjectDraft {
		return Monitoring{}, errors.New("proyek masih Draft")
	}
	subject, err := subjectOf(p, subjectCode)
	if err != nil {
		return Monitoring{}, err
	}
	history, err := s.store.ListRounds(ctx, projectID, subjectCode)
	if err != nil {
		return Monitoring{}, err
	}
	round, err := s.displayRound(ctx, p, subjectCode, history)
	if requested > 0 {
		err = errRoundUnavailable
		for _, candidate := range history {
			if candidate.Number == requested {
				round = candidate
				err = nil
				break
			}
		}
	}
	if err != nil {
		return Monitoring{}, err
	}
	users, err := s.store.ListUsers(ctx, projectID)
	if err != nil {
		return Monitoring{}, err
	}
	panelists, err := effectivePanelists(p, subject, users, round)
	if err != nil {
		return Monitoring{}, err
	}
	items, err := s.store.ListItems(ctx, subjectCode, projectID, false)
	if err != nil {
		return Monitoring{}, err
	}
	byItem := map[string]domain.Item{}
	ordered := []domain.Item{}
	for _, item := range items {
		if contains(subject.ItemIDs, item.ID) {
			byItem[item.ID] = item
		}
	}
	for _, id := range subject.ItemIDs {
		if item, ok := byItem[id]; ok {
			ordered = append(ordered, item)
		}
	}
	rows, err := s.store.ListSubmissions(ctx, projectID, subjectCode, round.Number, "")
	if err != nil {
		return Monitoring{}, err
	}
	byUser := map[string]map[string]domain.Submission{}
	for _, row := range rows {
		item, ok := byItem[row.ItemID]
		if !ok || row.Status != "saved" || validateItemAnswer(item, row.Answer) != nil {
			continue
		}
		if byUser[row.PanelistID] == nil {
			byUser[row.PanelistID] = map[string]domain.Submission{}
		}
		byUser[row.PanelistID][row.ItemID] = row
	}
	result := Monitoring{Project: p, Subject: subject, Round: round, History: history, Panelists: []MonitoringPanelist{}, Items: ordered, TotalItems: len(subject.ItemIDs), Excluded: []MonitoringExclusion{}, Notes: []MonitoringNote{}}
	result.FinalNBKChoices = []FinalNBKChoice{}
	for _, r := range history {
		if r.Status == "finalized" && r.FinalSnapshot != nil {
			result.FinalNBKChoices = append(result.FinalNBKChoices, FinalNBKChoice{SnapshotID: r.FinalSnapshot.ID, RoundNumber: r.Number, Revision: domain.RoundRevision(r), Statistics: r.FinalSnapshot.Statistics})
		}
	}
	for _, exclusion := range round.ExcludedPanelists {
		for _, u := range users {
			if u.ID == exclusion.PanelistID && u.Active && !u.Removed {
				result.Excluded = append(result.Excluded, MonitoringExclusion{exclusion, u.Name})
			}
		}
	}
	telemetry, err := s.TelemetryForRound(ctx, projectID, subjectCode, round.Number, domain.RoundRevision(round))
	if err != nil {
		return Monitoring{}, err
	}
	scores := []float64{}
	for _, user := range panelists {
		responses := byUser[user.ID]
		panelist := MonitoringPanelist{ID: user.ID, Name: user.Name, Username: user.Username, Done: 0, Total: len(subject.ItemIDs), Status: "Belum Menilai"}
		if row, ok := telemetry[user.ID]; ok {
			panelist.Telemetry = &row
			if panelist.LastActive == nil || row.LastActive.After(*panelist.LastActive) {
				at := row.LastActive
				panelist.LastActive = &at
			}
		}
		if st, ok := s.store.(contactStore); ok {
			c, e := st.GetContact(ctx, user.ID)
			if e == nil {
				panelist.WhatsApp, panelist.Email = c.WhatsApp, c.Email
				_, _, invalid := domain.NormalizeContact(c.WhatsApp, c.Email)
				panelist.ContactValid = invalid == nil
			}
		}
		for _, row := range responses {
			if domain.SubmissionReviewed(row, round) && (p.FeatureVersion < 1 || row.CandidateVersion == subject.CandidateVersion) {
				panelist.Done++
			} else {
				panelist.NeedsReview++
			}
			if row.Note != "" {
				result.Notes = append(result.Notes, MonitoringNote{user.ID, user.Name, row.ItemID, row.Note, row.UpdatedAt})
			}
			if panelist.LastActive == nil || row.UpdatedAt.After(*panelist.LastActive) {
				at := row.UpdatedAt
				panelist.LastActive = &at
			}
		}
		receipt := receiptFor(round, user.ID, subject.CandidateVersion)
		if receipt != nil {
			panelist.SubmittedAt = receipt.SubmittedAt
		}
		if panelist.Done == panelist.Total && panelist.Total > 0 {
			panelist.Status = "Selesai"
			if p.WorkflowVersion >= 1 && receipt == nil && round.Status == "active" {
				panelist.Status = "Belum Dikirim"
			}
			weighted, total := 0.0, 0.0
			for _, item := range ordered {
				weighted += responseValue(responses[item.ID].Answer, item.MaxScore) * float64(item.MaxScore)
				total += float64(item.MaxScore)
			}
			if total > 0 {
				scores = append(scores, weighted/total)
			}
		} else if panelist.NeedsReview > 0 {
			panelist.Status = "Perlu Ditinjau"
		} else if panelist.Done > 0 {
			panelist.Status = "Menilai"
			if panelist.LastActive != nil && time.Since(*panelist.LastActive) > 24*time.Hour {
				panelist.Status = "Perlu Perhatian"
			}
		}
		result.TotalDone += panelist.Done
		result.Panelists = append(result.Panelists, panelist)
	}
	ready := len(scores) == len(panelists) && len(scores) > 0
	if ready {
		stats := domain.ComputeStatistics(scores, projectRoundingPolicy(p))
		stats.RoundNumber = round.Number
		stats.Revision = domain.RoundRevision(round)
		result.Statistics = &stats
	}
	if round.Status == "finalized" && round.FinalSnapshot != nil {
		stats := round.FinalSnapshot.Statistics
		result.Statistics = &stats
	}
	result.Round.FinalSnapshot = nil
	result.Round.Snapshots = nil
	result.Round.Revision = domain.RoundRevision(round)
	for i := range result.History {
		result.History[i].FinalSnapshot = nil
		result.History[i].Snapshots = nil
		result.History[i].Revision = domain.RoundRevision(result.History[i])
	}
	submitted := true
	if p.WorkflowVersion >= 1 {
		for _, u := range result.Panelists {
			if u.SubmittedAt == nil {
				submitted = false
			}
		}
	}
	result.CanFinalize = submitted && !(p.SimulationEnabled && subject.PracticeStatus == "open") && ready && p.Status == domain.ProjectActive && round.Status == "active"
	result.CanRollback = p.Status == domain.ProjectActive && subject.FinalNBK == nil && !(p.SimulationEnabled && subject.PracticeStatus == "open") && ((round.Number > 1 && round.Status == "active") || (p.PlannedRounds > 0 && round.Status == "finalized"))
	return result, nil
}

func (s *Service) PreviewPanelist(ctx context.Context, projectID, subjectCode string, round int, panelistID string) ([]domain.Submission, error) {
	if round < 1 {
		return nil, errors.New("nomor putaran tidak valid")
	}
	p, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	subject, err := subjectOf(p, subjectCode)
	if err != nil {
		return nil, err
	}
	u, err := s.store.FindByID(ctx, panelistID)
	if err != nil || !u.Active || u.Removed || u.Role != domain.RolePanelist || u.ProjectID != p.ID || u.SubjectCode != subjectCode || !contains(subject.PanelistUserIDs, panelistID) {
		return nil, errors.New("panelis tidak ditugaskan pada proyek/mapel ini")
	}
	return s.store.ListSubmissions(ctx, projectID, subjectCode, round, panelistID)
}

func responseValue(answer domain.Answer, maxScore int) float64 {
	if answer.Value != nil {
		return float64(*answer.Value)
	}
	if maxScore < 1 {
		return 0
	}
	weighted := 0
	for score, count := range answer.Distribution {
		weighted += score * count
	}
	return float64(weighted) / float64(maxScore)
}
func calculateStatistics(input []float64) domain.ItemResult {
	return domain.ComputeStatistics(input, "none")
}

type AllMonitoring struct {
	Project  domain.Project `json:"project"`
	Subjects []Monitoring   `json:"subjects"`
}

func (s *Service) MonitoringAll(ctx context.Context, id string, round int) (AllMonitoring, error) {
	var result AllMonitoring
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		p, err := s.store.GetProject(tx, id)
		if err != nil {
			return err
		}
		result = AllMonitoring{Project: p, Subjects: []Monitoring{}}
		for _, subject := range p.Subjects {
			m, err := s.monitoringRound(tx, id, subject.Code, round)
			if errors.Is(err, errRoundUnavailable) {
				history, historyErr := s.store.ListRounds(tx, id, subject.Code)
				if historyErr != nil {
					return historyErr
				}
				m = Monitoring{Project: p, Subject: subject, Round: domain.Round{Number: round, Status: "unavailable"}, History: history, Panelists: []MonitoringPanelist{}, Items: []domain.Item{}}
				err = nil
			}
			if err != nil {
				return err
			}
			result.Subjects = append(result.Subjects, m)
		}
		return nil
	})
	return result, err
}
