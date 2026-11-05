package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"standard-setting/backend/internal/domain"
)

var ErrExportConfirmationRequired = errors.New("proyek belum selesai; konfirmasi untuk mengunduh hasil sementara")

type workbookPanelist struct{ id, name string }
type workbookRound struct {
	round      domain.Round
	items      []domain.Item
	panelists  []workbookPanelist
	values     map[string]map[string]float64
	statistics *domain.ItemResult
	icc        *float64
	complete   bool
	source     string
}
type workbookSubject struct {
	subject domain.ProjectSubject
	rounds  []workbookRound
}

// ExportRoundWorkbooks reads one consistent project view. Workbook rendering is
// deliberately outside the transaction, so formatting does not hold DB locks.
func (s *Service) ExportRoundWorkbooks(ctx context.Context, projectID, subjectCode string, confirmed bool) ([]byte, string, error) {
	var project domain.Project
	subjects := []workbookSubject{}
	generated := time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60))
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		var err error
		project, err = s.store.GetProject(tx, projectID)
		if err != nil {
			return err
		}
		if project.Status != domain.ProjectCompleted && !confirmed {
			return ErrExportConfirmationRequired
		}
		if subjectCode != "" && subjectCode != "all" {
			if _, err := subjectOf(project, subjectCode); err != nil {
				return err
			}
		}
		users, err := s.store.ListUsers(tx, projectID)
		if err != nil {
			return err
		}
		subjects = nil // WithTransaction may retry its callback.
		for _, subject := range project.Subjects {
			if subjectCode != "" && subjectCode != "all" && subject.Code != subjectCode {
				continue
			}
			items, err := s.store.ListItems(tx, subject.Code, projectID, false)
			if err != nil {
				return err
			}
			rounds, err := s.store.ListRounds(tx, projectID, subject.Code)
			if err != nil {
				return err
			}
			rounds = append([]domain.Round(nil), rounds...)
			sort.Slice(rounds, func(i, j int) bool { return rounds[i].Number < rounds[j].Number })
			bundle := workbookSubject{subject: subject}
			for _, round := range rounds {
				if round.Number < 1 || (round.Status != "active" && round.Status != "finalized" && round.Status != "rolled_back") {
					continue // No practice or future rounds.
				}
				data, err := s.readWorkbookRound(tx, project, subject, round, items, users)
				if err != nil {
					return err
				}
				bundle.rounds = append(bundle.rounds, data)
			}
			subjects = append(subjects, bundle)
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	write := func(path string, data []byte, err error) error {
		if err != nil {
			return err
		}
		file, err := archive.Create(path)
		if err != nil {
			return err
		}
		_, err = file.Write(data)
		return err
	}
	for i, subject := range subjects {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		code := workbookFilePart(subject.subject.Code)
		folder := fmt.Sprintf("%02d-%s/", i+1, code)
		summary, err := renderRoundSummary(project, subject, generated)
		name := fmt.Sprintf("rekap_%s%s%s.xlsx", workbookFilePart(project.Code), code, generated.Format("20060102"))
		if err := write(folder+name, summary, err); err != nil {
			return nil, "", err
		}
		for _, round := range subject.rounds {
			body, err := renderRoundResponses(project, subject.subject, round, generated)
			if err := write(folder+fmt.Sprintf("putaran-%d.xlsx", round.round.Number), body, err); err != nil {
				return nil, "", err
			}
		}
	}
	if err := archive.Close(); err != nil {
		return nil, "", err
	}
	name := fmt.Sprintf("hasil-penilaian-%s-%s.zip", workbookFilePart(project.Code), generated.Format("20060102-150405"))
	return output.Bytes(), name, nil
}

func workbookFilePart(value string) string {
	var safe strings.Builder
	for _, r := range value {
		if safe.Len() >= 64 {
			break
		}
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			safe.WriteRune(r)
		} else if safe.Len() > 0 && !strings.HasSuffix(safe.String(), "-") {
			safe.WriteByte('-')
		}
	}
	result := strings.Trim(safe.String(), "-_")
	if result == "" {
		return "data"
	}
	return result
}

func (s *Service) readWorkbookRound(ctx context.Context, p domain.Project, sub domain.ProjectSubject, round domain.Round, items []domain.Item, users []domain.User) (workbookRound, error) {
	data := workbookRound{round: round, values: map[string]map[string]float64{}, source: "Respons tersimpan saat unduhan dibuat; hasil sementara."}
	byItem := map[string]domain.Item{}
	for _, item := range items {
		byItem[item.ID] = item
	}
	ids := append([]string(nil), sub.ItemIDs...)
	rows := []domain.Submission{}
	candidate := sub.CandidateVersion
	snapshot := finalMonitoringSnapshot(round)
	if snapshot != nil {
		data.source = fmt.Sprintf("Snapshot putaran terkunci, revisi %d.", snapshot.Revision)
		stats := domain.SampleStatistics(snapshot.Statistics)
		if stats.PanelistCount > 0 {
			data.statistics = &stats
		}
		rows, candidate = snapshot.Submissions, snapshot.CandidateVersion
		names := map[string]string{}
		for _, person := range snapshot.Roster {
			names[person.PanelistID] = person.PanelistName
		}
		for _, row := range rows {
			if names[row.PanelistID] == "" {
				names[row.PanelistID] = row.PanelistName
			}
		}
		for _, id := range snapshot.PanelistIDs {
			data.panelists = append(data.panelists, workbookPanelist{id, names[id]})
		}
		// Freeze maximum/type from the answer shape, never from edited master data.
		frozenIDs := map[string]bool{}
		for id := range snapshot.ItemStatistics {
			frozenIDs[id] = true
		}
		for _, row := range rows {
			if !contains(snapshot.PanelistIDs, row.PanelistID) || !domain.SubmissionReviewed(row, round) || p.FeatureVersion >= 1 && row.CandidateVersion != candidate {
				continue
			}
			if len(snapshot.ItemStatistics) > 0 && !frozenIDs[row.ItemID] {
				continue
			}
			frozenIDs[row.ItemID] = true
			if row.Status != "saved" || domain.ValidateAnswer(row.Answer) != nil {
				continue
			}
			maximum := len(row.Answer.Distribution) - 1
			if row.Answer.QuestionType == domain.QuestionTypePG {
				maximum = 1
			}
			byItem[row.ItemID] = domain.Item{ID: row.ItemID, QuestionType: row.Answer.QuestionType, MaxScore: maximum}
		}
		ids = nil
		for _, id := range sub.ItemIDs {
			if frozenIDs[id] {
				ids = append(ids, id)
				delete(frozenIDs, id)
			}
		}
		extra := []string{}
		for id := range frozenIDs {
			extra = append(extra, id)
		}
		sort.Strings(extra)
		ids = append(ids, extra...)
	} else {
		panelists, err := effectivePanelists(p, sub, users, round)
		if err != nil {
			return data, err
		}
		for _, user := range panelists {
			data.panelists = append(data.panelists, workbookPanelist{user.ID, user.Name})
		}
		rows, err = s.store.ListSubmissions(ctx, p.ID, sub.Code, round.Number, "")
		if err != nil {
			return data, err
		}
		if round.Status == "finalized" {
			data.source = "Hasil historis tanpa snapshot lengkap; statistik tidak direkonstruksi dari master terbaru."
		} else if round.Status == "rolled_back" {
			data.source = "Putaran dibatalkan; respons valid yang masih tersimpan, bukan hasil final."
		}
	}
	for _, id := range ids {
		item, ok := byItem[id]
		if !ok {
			item = domain.Item{ID: id}
		}
		data.items = append(data.items, item)
		data.values[id] = map[string]float64{}
	}
	included := map[string]bool{}
	for _, panelist := range data.panelists {
		included[panelist.id] = true
	}
	for _, row := range rows {
		item, ok := byItem[row.ItemID]
		if !ok || data.values[row.ItemID] == nil || !included[row.PanelistID] || row.Status != "saved" || validateItemAnswer(item, row.Answer) != nil || !domain.SubmissionReviewed(row, round) || p.FeatureVersion >= 1 && row.CandidateVersion != candidate {
			continue
		}
		data.values[row.ItemID][row.PanelistID] = responseValue(row.Answer, item.MaxScore)
	}
	data.complete = len(data.items) > 0 && len(data.panelists) > 0
	matrix := make([][]float64, len(data.items))
	for i, item := range data.items {
		for _, panelist := range data.panelists {
			value, ok := data.values[item.ID][panelist.id]
			data.complete = data.complete && ok
			matrix[i] = append(matrix[i], value)
		}
	}
	if data.complete && (snapshot != nil || round.Status == "active") {
		data.icc = domain.ICC2K(matrix)
		if snapshot == nil {
			scores := []float64{}
			for _, panelist := range data.panelists {
				weighted, maximum := 0.0, 0.0
				for _, item := range data.items {
					weighted += data.values[item.ID][panelist.id] * float64(item.MaxScore)
					maximum += float64(item.MaxScore)
				}
				if maximum <= 0 {
					return data, errors.New("skor maksimum butir tidak valid")
				}
				scores = append(scores, weighted/maximum)
			}
			stats := domain.ComputeStatistics(scores, projectRoundingPolicy(p))
			data.statistics = &stats
		}
	}
	return data, nil
}
