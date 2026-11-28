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
	AngoffReview         domain.AngoffReview         `json:"angoffReview"`
	StandardSettingError domain.StandardSettingError `json:"standardSettingError"`
	ItemRounds           []MonitoringItemRound       `json:"itemRounds"`
	FinalNBKChoices      []FinalNBKChoice            `json:"finalNBKChoices"`
	Project              domain.Project              `json:"project"`
	Subject              domain.ProjectSubject       `json:"subject"`
	Round                domain.Round                `json:"round"`
	History              []domain.Round              `json:"history"`
	Panelists            []MonitoringPanelist        `json:"panelists"`
	Items                []domain.Item               `json:"items"`
	ItemGuidance         []MonitoringItemGuidance    `json:"itemGuidance"`
	Statistics           *domain.ItemResult          `json:"statistics"`
	TotalDone            int                         `json:"totalDone"`
	TotalItems           int                         `json:"totalItems"`
	CanFinalize          bool                        `json:"canFinalize"`
	CanRollback          bool                        `json:"canRollback"`
	Excluded             []MonitoringExclusion       `json:"excluded"`
	Notes                []MonitoringNote            `json:"notes"`
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
	snapshot := finalMonitoringSnapshot(round)
	if snapshot != nil {
		subject.CandidateVersion = snapshot.CandidateVersion
		subject.CandidateDefinition = snapshot.CandidateDefinition
		// The frozen cohort remains authoritative even after accounts are removed
		// or their active assignment changes.
		included := []domain.User{}
		for _, id := range snapshot.PanelistIDs {
			user := domain.User{ID: id}
			for _, candidate := range users {
				if candidate.ID == id {
					user = candidate
					break
				}
			}
			for _, person := range snapshot.Roster {
				if person.PanelistID == id {
					user.Name = person.PanelistName
					break
				}
			}
			included = append(included, user)
		}
		panelists = included
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
	if snapshot != nil {
		frozen, _, err := frozenScoringResponses(*snapshot)
		if err != nil {
			return Monitoring{}, err
		}
		for id, item := range frozen {
			if contains(subject.ItemIDs, id) {
				byItem[id] = item
			}
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
	if snapshot != nil {
		rows = snapshot.Submissions
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
	result.ItemGuidance = monitoringItemGuidance(round, ordered)
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
	discussionNotes := []domain.DiscussionNote{}
	if st, ok := s.store.(discussionNoteStore); ok {
		discussionNotes, err = st.ListDiscussionNotes(ctx, "", projectID, subjectCode, round.Number, domain.RoundRevision(round))
		if err != nil {
			return Monitoring{}, err
		}
	}
	scores := []float64{}
	reviewValues := map[string]map[string]float64{}
	reviewIDs := []string{}
	for _, user := range panelists {
		reviewIDs = append(reviewIDs, user.ID)
		responses := byUser[user.ID]
		for _, note := range discussionNotes {
			if note.UserID == user.ID && note.Text != "" && contains(subject.ItemIDs, note.ItemID) {
				result.Notes = append(result.Notes, MonitoringNote{user.ID, user.Name, note.ItemID, "Catatan diskusi: " + note.Text, note.UpdatedAt})
			}
		}
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
			values := map[string]float64{}
			for _, item := range ordered {
				values[item.ID] = responseValue(responses[item.ID].Answer, item.MaxScore)
			}
			reviewValues[user.ID] = values
			score, err := testScoreForItems(ordered, values, p.ScoringMethod)
			if err != nil {
				return Monitoring{}, err
			}
			scores = append(scores, score)
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
	result.AngoffReview = domain.ComputeAngoffReview(ordered, reviewIDs, reviewValues)
	ready := len(scores) == len(panelists) && len(scores) > 0 && result.AngoffReview.Ready
	if ready {
		stats := domain.ComputeTestStatistics(scores, projectRoundingPolicy(p), ordered)
		stats.ScoringMethod = domain.NormalizeScoringMethod(p.ScoringMethod)
		stats.RoundNumber = round.Number
		stats.Revision = domain.RoundRevision(round)
		result.Statistics = &stats
	}
	if round.Status == "finalized" && round.FinalSnapshot != nil {
		stats := round.FinalSnapshot.Statistics
		result.Statistics = &stats
	}
	if round.FinalSnapshot != nil && round.Status == "finalized" {
		result.AngoffReview, err = snapshotAngoffReview(*round.FinalSnapshot)
		if err != nil {
			return Monitoring{}, err
		}
	}
	if result.Statistics != nil {
		result.Statistics.ProjectionPercent = p.PassConversion.ProjectedPassPercentage(
			subject.Code, result.Statistics, result.AngoffReview.MeanRaw, float64(result.AngoffReview.MaxPoints),
		)
	}
	result.StandardSettingError = domain.RoundStandardSettingError(subject.SEM, round.FinalSnapshot, result.Statistics, result.AngoffReview, projectRoundingPolicy(p))
	result.ItemRounds, err = s.monitoringItemRounds(ctx, p, subject, history, ordered, users)
	if err != nil {
		return Monitoring{}, err
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
	history, err := s.store.ListRounds(ctx, projectID, subjectCode)
	if err != nil {
		return nil, err
	}
	for _, historical := range history {
		if historical.Number != round {
			continue
		}
		if snapshot := finalMonitoringSnapshot(historical); snapshot != nil {
			rows := []domain.Submission{}
			for _, row := range snapshot.Submissions {
				if row.PanelistID == panelistID {
					rows = append(rows, row)
				}
			}
			return rows, nil
		}
		break
	}
	return s.store.ListSubmissions(ctx, projectID, subjectCode, round, panelistID)
}

func responseValue(answer domain.Answer, maxScore int) float64 {
	if answer.Value != nil {
		if answer.QuestionType != domain.QuestionTypePG {
			if answer.MaxScore > 0 {
				maxScore = answer.MaxScore
			}
			if maxScore < 1 {
				return 0
			}
			return 100 * float64(*answer.Value) / float64(maxScore)
		}
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
	return s.monitoringAll(ctx, id, round, "")
}

var ErrMonitoringAssignment = errors.New("proyek ini tidak ditugaskan kepada akun Anda")

func (s *Service) MonitoringAllForViewer(ctx context.Context, id string, round int, viewerID string) (AllMonitoring, error) {
	if viewerID == "" {
		return AllMonitoring{}, ErrMonitoringAssignment
	}
	return s.monitoringAll(ctx, id, round, viewerID)
}

func (s *Service) monitoringAll(ctx context.Context, id string, round int, viewerID string) (AllMonitoring, error) {
	var result AllMonitoring
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		if viewerID != "" {
			viewer, err := s.store.FindByID(tx, viewerID)
			if errors.Is(err, mongo.ErrNoDocuments) {
				return ErrMonitoringAssignment
			}
			if err != nil {
				return err
			}
			if !viewer.Active || viewer.Removed || viewer.Role != domain.RoleUserMonitoring || viewer.ProjectID != id {
				return ErrMonitoringAssignment
			}
		}
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
				m = Monitoring{AngoffReview: domain.ComputeAngoffReview(nil, nil, nil), StandardSettingError: domain.RoundStandardSettingError(subject.SEM, nil, nil, domain.AngoffReview{}, projectRoundingPolicy(p)), Project: p, Subject: subject, Round: domain.Round{Number: round, Status: "unavailable"}, History: history, Panelists: []MonitoringPanelist{}, Items: []domain.Item{}, ItemGuidance: []MonitoringItemGuidance{}}
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
