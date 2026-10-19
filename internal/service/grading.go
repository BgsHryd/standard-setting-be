package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"standard-setting/backend/internal/domain"
)

func validateItemAnswer(item domain.Item, answer domain.Answer) error {
	if item.QuestionType != answer.QuestionType {
		return errors.New("jenis jawaban tidak sesuai butir")
	}
	if err := domain.ValidateAnswer(answer); err != nil {
		return err
	}
	if answer.QuestionType != domain.QuestionTypePG {
		if answer.Value != nil {
			return errors.New("jawaban distribusi tidak menerima nilai tunggal")
		}
		if item.MaxScore < 1 || len(answer.Distribution) != item.MaxScore+1 {
			return errors.New("jumlah kategori distribusi tidak sesuai skor maksimum butir")
		}
	}
	return nil
}

func (s *Service) openRound(ctx context.Context, projectID, subjectCode string, number int) (domain.Round, error) {
	rounds, err := s.store.ListRounds(ctx, projectID, subjectCode)
	if err != nil {
		return domain.Round{}, err
	}
	now := time.Now().UTC()
	for _, round := range rounds {
		if round.Number == number {
			if round.Status == "rolled_back" {
				round.Revision = domain.RoundRevision(round) + 1
				round.FinalSnapshot = nil
			}
			round.Status = "active"
			round.FinalizedAt = nil
			round.FinalizedBy = ""
			round.UpdatedAt = now
			return round, s.store.SaveRound(ctx, round)
		}
	}
	round := domain.Round{ID: newID("round"), ProjectID: projectID, SubjectCode: subjectCode, Number: number, Revision: 1, Status: "active", CreatedAt: now, UpdatedAt: now}
	return round, s.store.SaveRound(ctx, round)
}

func (s *Service) completeResponses(ctx context.Context, p domain.Project, subject domain.ProjectSubject, number int) ([]domain.Submission, []domain.Item, error) {
	users, err := s.store.ListUsers(ctx, p.ID)
	if err != nil {
		return nil, nil, err
	}
	rounds, err := s.store.ListRounds(ctx, p.ID, subject.Code)
	if err != nil {
		return nil, nil, err
	}
	var round domain.Round
	for _, candidate := range rounds {
		if candidate.Number == number {
			round = candidate
			break
		}
	}
	panelists, err := effectivePanelists(p, subject, users, round)
	if err != nil {
		return nil, nil, err
	}
	if len(panelists) == 0 {
		return nil, nil, errors.New("tidak ada panelis aktif untuk difinalisasi")
	}
	rows, err := s.store.ListSubmissions(ctx, p.ID, subject.Code, number, "")
	if err != nil {
		return nil, nil, err
	}
	byKey := map[string]domain.Submission{}
	for _, row := range rows {
		if row.Status == "saved" {
			byKey[row.PanelistID+"\x00"+row.ItemID] = row
		}
	}
	items := []domain.Item{}
	valid := []domain.Submission{}
	for _, itemID := range subject.ItemIDs {
		item, err := s.store.GetItem(ctx, itemID)
		if err != nil {
			return nil, nil, err
		}
		if item.ProjectID != p.ID || item.SubjectCode != subject.Code {
			return nil, nil, errors.New("alokasi butir tidak sesuai proyek")
		}
		items = append(items, item)
		for _, user := range panelists {
			row, exists := byKey[user.ID+"\x00"+itemID]
			if !exists || validateItemAnswer(item, row.Answer) != nil || !domain.SubmissionReviewed(row, round) || (p.FeatureVersion >= 1 && row.CandidateVersion != subject.CandidateVersion) {
				return nil, nil, fmt.Errorf("penilaian %s untuk %s belum lengkap/valid", user.Name, itemID)
			}
			valid = append(valid, row)
		}
	}
	if len(items) == 0 {
		return nil, nil, errors.New("tidak ada ID butir untuk difinalisasi")
	}
	return valid, items, nil
}

func (s *Service) RoundAction(ctx context.Context, projectID, subjectCode, action, confirmation, userID string, expected ...int) (domain.Round, error) {
	var result domain.Round
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		p, err := s.store.GetProject(tx, projectID)
		if err != nil {
			return err
		}
		if p.Status != domain.ProjectActive {
			return errors.New("proyek sudah tidak aktif")
		}
		subject, err := subjectOf(p, subjectCode)
		if err != nil {
			return err
		}
		pic, picErr := s.store.FindByID(tx, userID)
		if picErr != nil || !pic.Active || pic.Removed || subject.PICUserID != userID {
			return errors.New("akun bukan PIC mapel ini")
		}
		current, err := s.store.CurrentRound(tx, projectID, subjectCode)
		if err != nil {
			return err
		}
		if len(expected) > 0 && expected[0] != current.Number {
			return ErrRevisionConflict
		}
		if (len(expected) > 1 && expected[1] != 0 && expected[1] != domain.RoundRevision(current)) || (domain.RoundRevision(current) > 1 && (len(expected) < 2 || expected[1] == 0)) {
			return ErrRevisionConflict
		}
		if err := s.store.TouchProject(tx, projectID); err != nil {
			return err
		}
		now := time.Now().UTC()
		switch action {
		case "finalize":
			if confirmation != "FINALISASI PUTARAN" {
				return errors.New("konfirmasi finalisasi tidak sesuai")
			}
			snapshot, err := s.freezeRound(tx, p, subject, current)
			if err != nil {
				return err
			}
			current.Revision = domain.RoundRevision(current)
			current.FinalSnapshot = &snapshot
			current.Snapshots = append(current.Snapshots, snapshot)
			current.Status = "finalized"
			current.FinalizedAt = &now
			current.FinalizedBy = userID
			current.UpdatedAt = now
			if err := s.store.SaveRound(tx, current); err != nil {
				return err
			}
			result, err = s.openRound(tx, projectID, subjectCode, current.Number+1)
			if err != nil {
				return err
			}
		case "rollback":
			if confirmation != "ROLLBACK PUTARAN" {
				return errors.New("konfirmasi rollback tidak sesuai")
			}
			if current.Number <= 1 {
				return errors.New("putaran pertama tidak dapat di-rollback")
			}
			current.Status = "rolled_back"
			current.UpdatedAt = now
			if err := s.store.SaveRound(tx, current); err != nil {
				return err
			}
			result, err = s.openRound(tx, projectID, subjectCode, current.Number-1)
			if err != nil {
				return err
			}
			result.Revision = domain.RoundRevision(result) + 1
			result.FinalSnapshot = nil
			result.UpdatedAt = now
			if err = s.store.SaveRound(tx, result); err != nil {
				return err
			}
		default:
			return errors.New("aksi putaran tidak didukung")
		}
		if err := s.recordAudit(tx, userID, current.ID, projectID, "round_"+action); err != nil {
			return err
		}
		// A shared project write serializes round changes against project completion.
		p.UpdatedAt = now
		return s.store.SaveProject(tx, p)
	})
	return result, err
}

// Public round metadata never includes frozen answers belonging to other panelists.
func publicRound(r domain.Round) domain.Round {
	r.FinalSnapshot = nil
	r.Snapshots = nil
	r.ExcludedPanelists = nil
	r.Revision = domain.RoundRevision(r)
	return r
}

type PanelistForm struct {
	Project             domain.Project        `json:"project"`
	Subject             domain.ProjectSubject `json:"subject"`
	Round               domain.Round          `json:"round"`
	Items               []domain.Item         `json:"items"`
	Submissions         []domain.Submission   `json:"submissions"`
	History             []domain.Round        `json:"history"`
	PreviousSubmissions []domain.Submission   `json:"previousSubmissions"`
	PreviousRound       *int                  `json:"previousRound"`
	ReadOnly            bool                  `json:"readOnly"`
	Feedback            []ItemFeedback        `json:"feedback"`
	PeriodMessage       string                `json:"periodMessage"`
}

func (s *Service) PanelistForm(ctx context.Context, userID, projectID, subjectCode string) (PanelistForm, error) {
	p, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return PanelistForm{}, err
	}
	if p.Status == domain.ProjectDraft {
		return PanelistForm{}, errors.New("proyek masih Draft; penilaian belum dibuka")
	}
	subject, err := subjectOf(p, subjectCode)
	if err != nil {
		return PanelistForm{}, err
	}
	u, err := s.store.FindByID(ctx, userID)
	if err != nil || !u.Active || u.Removed || u.Role != domain.RolePanelist || u.ProjectID != projectID || u.SubjectCode != subjectCode || !contains(subject.PanelistUserIDs, userID) {
		return PanelistForm{}, errors.New("panelis tidak ditugaskan pada proyek/mapel ini")
	}
	if err := s.contactPolicy(ctx, userID); err != nil {
		return PanelistForm{}, err
	}
	history, err := s.store.ListRounds(ctx, projectID, subjectCode)
	if err != nil {
		return PanelistForm{}, err
	}
	round, err := s.displayRound(ctx, p, subjectCode, history)
	if err != nil {
		return PanelistForm{}, err
	}
	items, err := s.store.ListItems(ctx, subjectCode, projectID, false)
	if err != nil {
		return PanelistForm{}, err
	}
	ordered := []domain.Item{}
	for _, id := range subject.ItemIDs {
		for _, item := range items {
			if item.ID == id {
				ordered = append(ordered, item)
			}
		}
	}
	rows, err := s.store.ListSubmissions(ctx, projectID, subjectCode, round.Number, userID)
	if err != nil {
		return PanelistForm{}, err
	}
	previous := []domain.Submission{}
	var previousRound *int
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Status == "finalized" && history[i].Number < round.Number {
			n := history[i].Number
			previousRound = &n
			previous, err = s.store.ListSubmissions(ctx, projectID, subjectCode, n, userID)
			break
		}
	}
	if err != nil {
		return PanelistForm{}, err
	}
	feedback := s.feedback(ctx, userID, p, subject, round, history)
	readOnly := !domain.ProjectWritable(p, time.Now().UTC()) || roundExcluded(round, userID)
	message := ""
	if !domain.ProjectWritable(p, time.Now().UTC()) {
		message = ErrPeriodClosed.Error()
	}
	if roundExcluded(round, userID) {
		message = "Anda dikeluarkan dari putaran ini oleh PIC; jawaban tetap tersimpan."
	}
	for i := range history {
		history[i] = publicRound(history[i])
	}
	return PanelistForm{Project: p, Subject: subject, Round: publicRound(round), Items: ordered, Submissions: rows, History: history, PreviousSubmissions: previous, PreviousRound: previousRound, ReadOnly: readOnly, Feedback: feedback, PeriodMessage: message}, nil
}

func (s *Service) SaveSubmission(ctx context.Context, userID, name, projectID, subjectCode string, number int, itemID string, answer domain.Answer) (domain.Submission, error) {
	return s.SaveSubmissionWithOptions(ctx, userID, name, projectID, subjectCode, number, itemID, answer, SubmissionOptions{legacy: true})
}

func (s *Service) CompleteProject(ctx context.Context, id, confirmation string) (domain.Project, error) {
	if confirmation != "SELESAIKAN PROYEK" {
		return domain.Project{}, errors.New("konfirmasi penyelesaian proyek tidak sesuai")
	}
	var result domain.Project
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		p, err := s.store.GetProject(tx, id)
		if err != nil {
			return err
		}
		if p.Status != domain.ProjectActive {
			return errors.New("hanya proyek aktif yang dapat diselesaikan")
		}
		if len(p.Subjects) == 0 {
			return errors.New("proyek belum memiliki mata pelajaran")
		}
		now := time.Now().UTC()
		for _, subject := range p.Subjects {
			history, err := s.store.ListRounds(tx, id, subject.Code)
			if err != nil {
				return err
			}
			var final domain.Round
			for _, round := range history {
				if round.Status == "finalized" && round.Number > final.Number {
					final = round
				}
			}
			if final.Number == 0 {
				return fmt.Errorf("mapel %s belum memiliki putaran yang difinalisasi", subject.Name)
			}
			current, err := s.store.CurrentRound(tx, id, subject.Code)
			if err != nil {
				return err
			}
			pending, err := s.store.ListSubmissions(tx, id, subject.Code, current.Number, "")
			if err != nil {
				return err
			}
			users, err := s.store.ListUsers(tx, p.ID)
			if err != nil {
				return err
			}
			active, err := effectivePanelists(p, subject, users, current)
			if err != nil {
				return err
			}
			activeIDs := map[string]bool{}
			for _, u := range active {
				activeIDs[u.ID] = true
			}
			for _, row := range pending {
				if activeIDs[row.PanelistID] {
					return errors.New("putaran aktif masih memiliki respons yang belum difinalisasi")
				}
			}
			var rows []domain.Submission
			var items []domain.Item
			if final.FinalSnapshot != nil {
				rows = final.FinalSnapshot.Submissions
				for _, itemID := range subject.ItemIDs {
					item, e := s.store.GetItem(tx, itemID)
					if e != nil {
						return e
					}
					items = append(items, item)
				}
			} else {
				rows, items, err = s.completeResponses(tx, p, subject, final.Number)
				if err != nil {
					return err
				}
			}
			for i := range items {
				values := []float64{}
				for _, row := range rows {
					if row.ItemID == items[i].ID {
						values = append(values, responseValue(row.Answer, items[i].MaxScore))
					}
				}
				stats := calculateStatistics(values)
				for _, row := range rows {
					if row.ItemID == items[i].ID && !contains(stats.PanelistIDs, row.PanelistID) {
						stats.PanelistIDs = append(stats.PanelistIDs, row.PanelistID)
					}
				}
				stats.RoundNumber = final.Number
				stats.CompletedAt = now
				if final.FinalSnapshot != nil {
					stats = final.FinalSnapshot.ItemStatistics[items[i].ID]
				}
				items[i].FinalResult = &stats
				items[i].ReadOnly = true
				items[i].UsageStatus = domain.ItemTested
				items[i].UpdatedAt = now
			}
			if err := s.store.UpsertItems(tx, items); err != nil {
				return err
			}
			current.Status = "closed"
			current.UpdatedAt = now
			if err := s.store.SaveRound(tx, current); err != nil {
				return err
			}
		}
		p.Status = domain.ProjectCompleted
		p.CompletedAt = &now
		p.UpdatedAt = now
		if err := s.store.SaveProject(tx, p); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}

type ItemResultDetail struct {
	Item       domain.Item         `json:"item"`
	Statistics *domain.ItemResult  `json:"statistics"`
	Responses  []domain.Submission `json:"responses"`
	Warning    string              `json:"warning,omitempty"`
}

func (s *Service) ItemResult(ctx context.Context, id string) (ItemResultDetail, error) {
	item, err := s.store.GetItem(ctx, id)
	if err != nil {
		return ItemResultDetail{}, err
	}
	if item.UsageStatus != domain.ItemTested {
		return ItemResultDetail{}, errors.New("hasil hanya tersedia untuk ID butir yang sudah diuji")
	}
	result := ItemResultDetail{Item: item, Statistics: item.FinalResult, Responses: []domain.Submission{}}
	if item.FinalResult == nil || item.FinalResult.RoundNumber < 1 {
		result.Statistics = nil
		result.Warning = "Snapshot putaran final belum tersedia untuk data historis ini."
		return result, nil
	}
	rows, err := s.store.ListSubmissions(ctx, item.ProjectID, item.SubjectCode, item.FinalResult.RoundNumber, "")
	if err != nil {
		return ItemResultDetail{}, err
	}
	users, err := s.store.ListUsers(ctx, item.ProjectID)
	if err != nil {
		return ItemResultDetail{}, err
	}
	active := map[string]bool{}
	for _, u := range users {
		if u.Active && !u.Removed && u.Role == domain.RolePanelist && u.SubjectCode == item.SubjectCode {
			active[u.ID] = true
		}
	}
	for _, row := range rows {
		if row.ItemID == id && active[row.PanelistID] && (len(item.FinalResult.PanelistIDs) == 0 || contains(item.FinalResult.PanelistIDs, row.PanelistID)) {
			result.Responses = append(result.Responses, row)
		}
	}
	if len(item.FinalResult.PanelistIDs) == 0 {
		result.Warning = "Snapshot historis belum menyimpan identitas panelis pembentuk statistik; baris berikut adalah respons putaran final dari peserta yang saat ini aktif."
	} else if len(result.Responses) != item.FinalResult.PanelistCount {
		result.Warning = "Statistik final adalah snapshot historis; baris peserta nonaktif atau dihapus tidak ditampilkan."
	}
	return result, nil
}
