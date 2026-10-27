package service

import (
	"context"
	"errors"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
	"strings"
	"time"
	"unicode/utf8"
)

type FinalNBKChoice struct {
	SnapshotID  string            `json:"snapshotId"`
	RoundNumber int               `json:"roundNumber"`
	Revision    int               `json:"revision"`
	Statistics  domain.ItemResult `json:"statistics"`
}

func receiptFor(round domain.Round, user string, candidate int) *domain.RoundReceipt {
	for i := range round.Receipts {
		r := &round.Receipts[i]
		if r.PanelistID == user && r.RoundRevision == domain.RoundRevision(round) && r.CandidateVersion == candidate && r.SubmittedAt != nil {
			return r
		}
	}
	return nil
}
func receiptMatches(r *domain.RoundReceipt, rows []domain.Submission, items []string) bool {
	if r == nil || len(r.Versions) != len(items) {
		return false
	}
	for _, id := range items {
		found := false
		for _, row := range rows {
			if row.ItemID == id && r.Versions[id] == row.Version {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func (s *Service) SubmitPanelistRound(ctx context.Context, user string, number, revision int, submit bool) (domain.RoundReceipt, error) {
	var result domain.RoundReceipt
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, p, err := s.ownPanelist(tx, user)
		if err != nil {
			return err
		}
		sub, err := subjectOf(p, u.SubjectCode)
		if err != nil {
			return err
		}
		if !domain.ProjectWritable(p, time.Now().UTC()) {
			return ErrPeriodClosed
		}
		if p.SimulationEnabled && sub.PracticeStatus == "open" || sub.FinalNBK != nil {
			return errors.New("penilaian belum dibuka atau sudah dikunci")
		}
		if err = s.contactPolicy(tx, user); err != nil {
			return err
		}
		r, err := s.store.CurrentRound(tx, p.ID, sub.Code)
		if err != nil || r.Number != number || domain.RoundRevision(r) != revision || roundExcluded(r, user) {
			return ErrRevisionConflict
		}
		rows, err := s.store.ListSubmissions(tx, p.ID, sub.Code, number, user)
		if err != nil {
			return err
		}
		result = domain.RoundReceipt{PanelistID: user, RoundRevision: revision, CandidateVersion: sub.CandidateVersion, Versions: map[string]int{}}
		if submit {
			if len(sub.ItemIDs) == 0 {
				return errors.New("belum ada butir penilaian")
			}
			for _, id := range sub.ItemIDs {
				found := false
				item, err := s.store.GetItem(tx, id)
				if err != nil {
					return err
				}
				for _, row := range rows {
					if row.ItemID == id && row.Status == "saved" && domain.SubmissionReviewed(row, r) && (p.FeatureVersion < 1 || row.CandidateVersion == sub.CandidateVersion) && validateItemAnswer(item, row.Answer) == nil {
						result.Versions[id] = row.Version
						found = true
						break
					}
				}
				if !found {
					return errors.New("lengkapi dan tinjau seluruh jawaban sebelum menyelesaikan putaran")
				}
			}
			if old := receiptFor(r, user, sub.CandidateVersion); receiptMatches(old, rows, sub.ItemIDs) {
				result = *old
				return nil
			}
			now := time.Now().UTC()
			result.SubmittedAt = &now
		}
		if err = s.store.TouchProject(tx, p.ID); err != nil {
			return err
		}
		receipts := []domain.RoundReceipt{}
		for _, old := range r.Receipts {
			if old.PanelistID != user {
				receipts = append(receipts, old)
			}
		}
		if submit {
			receipts = append(receipts, result)
		}
		r.Receipts = receipts
		r.UpdatedAt = time.Now().UTC()
		if err = s.store.SaveRound(tx, r); err != nil {
			return err
		}
		action := "panelist_round_reopen"
		if submit {
			action = "panelist_round_submit"
		}
		return s.recordAudit(tx, user, r.ID, p.ID, action)
	})
	return result, err
}

func (s *Service) LockFinalNBK(ctx context.Context, actor, project, code string, number, revision, version int, reason string) (domain.NBKDecision, error) {
	var result domain.NBKDecision
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 1000 {
		return result, errors.New("alasan penetapan wajib diisi, maksimal 1.000 karakter")
	}
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, err := s.store.FindByID(tx, actor)
		if err != nil {
			return err
		}
		p, err := s.store.GetProject(tx, project)
		if err != nil {
			return err
		}
		sub, err := subjectOf(p, code)
		if err != nil {
			return err
		}
		if !u.Active || u.Removed || u.Role != domain.RolePICMapel || sub.PICUserID != u.ID {
			return errors.New("hanya PIC mapel yang dapat menetapkan NBK final")
		}
		if p.Version != version {
			return ErrRevisionConflict
		}
		if p.Status != domain.ProjectActive || sub.FinalNBK != nil {
			return errors.New("NBK final sudah dikunci atau proyek tidak aktif")
		}
		if p.SimulationEnabled && sub.PracticeStatus == "open" {
			return errors.New("latihan belum ditutup")
		}
		rounds, err := s.store.ListRounds(tx, p.ID, code)
		if err != nil {
			return err
		}
		var chosen *domain.RoundSnapshot
		completed := map[int]bool{}
		for _, r := range rounds {
			if r.Status == "active" {
				return errors.New("selesaikan seluruh putaran sebelum menetapkan NBK")
			}
			if r.Status == "finalized" && r.FinalSnapshot != nil {
				completed[r.Number] = true
				if r.Number == number && domain.RoundRevision(r) == revision {
					copy := *r.FinalSnapshot
					chosen = &copy
				}
			}
		}
		for n := 1; n <= p.PlannedRounds; n++ {
			if !completed[n] {
				return errors.New("seluruh putaran terencana harus difinalisasi")
			}
		}
		if chosen == nil || chosen.Statistics.PanelistCount == 0 {
			return errors.New("pilih snapshot putaran final yang valid")
		}
		if err = s.store.TouchProject(tx, p.ID); err != nil {
			return err
		}
		result = domain.NBKDecision{SnapshotID: chosen.ID, RoundNumber: number, Revision: revision, Statistics: chosen.Statistics, ActorID: actor, Reason: reason, LockedAt: time.Now().UTC()}
		for i := range p.Subjects {
			if p.Subjects[i].Code == code {
				p.Subjects[i].FinalNBK = &result
			}
		}
		p.Version++
		p.UpdatedAt = result.LockedAt
		if err = s.store.SaveProject(tx, p); err != nil {
			return err
		}
		return s.recordAudit(tx, actor, chosen.ID, p.ID, "nbk_final_lock")
	})
	return result, err
}

func (s *Service) ExtendRounds(ctx context.Context, actor string, version int) (domain.Project, error) {
	var result domain.Project
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, err := s.store.FindByID(tx, actor)
		if err != nil {
			return err
		}
		p, err := s.store.GetProject(tx, u.ProjectID)
		if err != nil {
			return err
		}
		sub, err := subjectOf(p, u.SubjectCode)
		if err != nil {
			return err
		}
		if !u.Active || u.Removed || u.Role != domain.RolePICMapel || sub.PICUserID != u.ID {
			return errors.New("hanya PIC mapel yang ditugaskan")
		}
		if p.Version != version {
			return ErrRevisionConflict
		}
		if p.Status != domain.ProjectActive || p.PlannedRounds < 1 || p.PlannedRounds >= 20 {
			return errors.New("jumlah putaran tidak dapat ditambah")
		}
		// Planned rounds are project-wide: never reopen a subject with a locked decision.
		for _, v := range p.Subjects {
			if v.FinalNBK != nil {
				return errors.New("NBK salah satu mapel sudah dikunci; jumlah putaran proyek tidak dapat diubah")
			}
		}
		if err = s.store.TouchProject(tx, p.ID); err != nil {
			return err
		}
		p.PlannedRounds++
		for _, v := range p.Subjects {
			_, err := s.store.CurrentRound(tx, p.ID, v.Code)
			if err == nil {
				continue
			}
			if !errors.Is(err, mongo.ErrNoDocuments) {
				return err
			}
			rounds, err := s.store.ListRounds(tx, p.ID, v.Code)
			if err != nil {
				return err
			}
			last := 0
			for _, r := range rounds {
				if r.Status == "finalized" && r.Number > last {
					last = r.Number
				}
			}
			if last > 0 && last < p.PlannedRounds {
				if _, err = s.openRound(tx, p.ID, v.Code, last+1); err != nil {
					return err
				}
			}
		}
		p.Version++
		p.UpdatedAt = time.Now().UTC()
		if err = s.store.SaveProject(tx, p); err != nil {
			return err
		}
		result = p
		return s.recordAudit(tx, actor, p.ID, p.ID, "round_extend")
	})
	return result, err
}
