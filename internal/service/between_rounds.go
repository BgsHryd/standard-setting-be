package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"standard-setting/backend/internal/domain"
)

// RoundOutcome contains only frozen aggregates, never roster or other users' answers.
type RoundOutcome struct {
	RoundNumber       int       `json:"roundNumber"`
	Revision          int       `json:"revision"`
	FinalizedAt       time.Time `json:"finalizedAt"`
	PanelistCount     int       `json:"panelistCount"`
	NBKPercent        float64   `json:"nbkPercent"`
	RoundingPolicy    string    `json:"roundingPolicy"`
	MaxScore          *float64  `json:"maxScore"`
	NBKScore          *float64  `json:"nbkScore"`
	ProjectionPercent *float64  `json:"projectionPercent"`
	ProjectionCount   int       `json:"projectionCount"`
}

type RoundReview struct {
	RoundOutcome
	Items []ItemFeedback `json:"items"`
}

type impactSummaryStore interface {
	ListImpactSummaries(context.Context, string) ([]domain.ImpactBatch, error)
}

type discussionNoteStore interface {
	ListDiscussionNotes(context.Context, string, string, string, int, int) ([]domain.DiscussionNote, error)
	SaveDiscussionNote(context.Context, domain.DiscussionNote, int) error
}

type DiscussionNoteInput struct {
	RoundNumber   int    `json:"roundNumber"`
	RoundRevision int    `json:"roundRevision"`
	Version       int    `json:"version"`
	Text          string `json:"text"`
}

func snapshotMaxScore(snap domain.RoundSnapshot) *float64 {
	maximums := map[string]int{}
	for _, row := range snap.Submissions {
		if _, ok := snap.ItemStatistics[row.ItemID]; !ok {
			continue
		}
		maximum := 1
		if row.Answer.QuestionType != domain.QuestionTypePG {
			maximum = len(row.Answer.Distribution) - 1
		}
		if maximum < 1 {
			return nil
		}
		maximums[row.ItemID] = maximum
	}
	if len(maximums) == 0 || len(maximums) != len(snap.ItemStatistics) {
		return nil
	}
	total := 0.0
	for _, maximum := range maximums {
		total += float64(maximum)
	}
	return &total
}

func (s *Service) roundResults(ctx context.Context, user string, p domain.Project, subject domain.ProjectSubject, current domain.Round, history []domain.Round) (*RoundReview, []RoundOutcome, error) {
	outcomes := []RoundOutcome{}
	var review *RoundReview
	var batches []domain.ImpactBatch
	if st, ok := s.store.(impactSummaryStore); ok {
		var err error
		batches, err = st.ListImpactSummaries(ctx, p.ID)
		if err != nil {
			return nil, nil, err
		}
	}
	ordered := append([]domain.Round(nil), history...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Number < ordered[j].Number })
	for _, round := range ordered {
		if round.Number > current.Number || round.Status != "finalized" || round.FinalSnapshot == nil {
			continue
		}
		snap := *round.FinalSnapshot
		// Historical archived revisions invalidated by rollback never enter this DTO.
		if snap.Revision != domain.RoundRevision(round) {
			continue
		}
		out := RoundOutcome{RoundNumber: snap.RoundNumber, Revision: snap.Revision, FinalizedAt: snap.CreatedAt, PanelistCount: snap.Statistics.PanelistCount, NBKPercent: snap.Statistics.OperationalCutoff, RoundingPolicy: snap.RoundingPolicy, MaxScore: snapshotMaxScore(snap)}
		if out.MaxScore != nil {
			score := out.NBKPercent * *out.MaxScore / 100
			out.NBKScore = &score
		}
		var newest time.Time
		for _, batch := range batches {
			for _, aggregate := range batch.Subjects {
				if aggregate.SubjectCode == subject.Code && aggregate.CutoffSnapshotID == snap.ID && aggregate.Count > 0 && (out.ProjectionPercent == nil || batch.CreatedAt.After(newest)) {
					percent := aggregate.Percentage
					out.ProjectionPercent, out.ProjectionCount, newest = &percent, aggregate.Count, batch.CreatedAt
				}
			}
		}
		outcomes = append(outcomes, out)
		items := []ItemFeedback{}
		for _, id := range subject.ItemIDs {
			if _, ok := snap.ItemStatistics[id]; !ok {
				continue
			}
			f := snapshotFeedback(snap, id, user)
			// Ordering anonymous points by value avoids linking the same panelist across items.
			sort.Float64s(f.Values)
			items = append(items, f)
		}
		review = &RoundReview{RoundOutcome: out, Items: items}
	}
	return review, outcomes, nil
}

func (s *Service) writablePanelistRound(ctx context.Context, id string, number, revision int) (domain.Project, domain.ProjectSubject, domain.Round, error) {
	u, p, err := s.ownPanelist(ctx, id)
	if err != nil {
		return p, domain.ProjectSubject{}, domain.Round{}, err
	}
	sub, err := subjectOf(p, u.SubjectCode)
	if err != nil {
		return p, sub, domain.Round{}, err
	}
	if err := s.contactPolicy(ctx, id); err != nil {
		return p, sub, domain.Round{}, err
	}
	if !domain.ProjectWritable(p, time.Now().UTC()) {
		return p, sub, domain.Round{}, ErrPeriodClosed
	}
	round, err := s.store.CurrentRound(ctx, p.ID, sub.Code)
	if err != nil {
		return p, sub, round, err
	}
	if number < 1 || revision < 1 || round.Number != number || domain.RoundRevision(round) != revision {
		return p, sub, round, ErrRevisionConflict
	}
	if roundExcluded(round, id) || (p.SimulationEnabled && sub.PracticeStatus == "open") || sub.FinalNBK != nil || receiptFor(round, id, sub.CandidateVersion) != nil {
		return p, sub, round, errors.New("putaran tidak dapat diubah")
	}
	return p, sub, round, nil
}

func (s *Service) StartPanelistRound(ctx context.Context, id string, number, revision int) (domain.RoundStart, error) {
	var result domain.RoundStart
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		p, _, round, err := s.writablePanelistRound(tx, id, number, revision)
		if err != nil {
			return err
		}
		for _, start := range round.Starts {
			if start.PanelistID == id && start.RoundRevision == revision {
				result = start
				return nil
			}
		}
		if err := s.store.TouchProject(tx, p.ID); err != nil {
			return err
		}
		result = domain.RoundStart{PanelistID: id, RoundNumber: number, RoundRevision: revision, StartedAt: time.Now().UTC().Truncate(time.Millisecond)}
		round.Starts = append(round.Starts, result)
		if err := s.store.SaveRound(tx, round); err != nil {
			return err
		}
		return s.recordAudit(tx, id, round.ID, p.ID, "panelist_round_start")
	})
	return result, err
}

func (s *Service) SaveDiscussionNote(ctx context.Context, id, itemID string, input DiscussionNoteInput) (domain.DiscussionNote, error) {
	var result domain.DiscussionNote
	input.Text = strings.TrimSpace(input.Text)
	if input.Version < 0 || utf8.RuneCountInString(input.Text) > 1000 {
		return result, errors.New("catatan maksimal 1.000 karakter dan versi harus valid")
	}
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		p, sub, round, err := s.writablePanelistRound(tx, id, input.RoundNumber, input.RoundRevision)
		if err != nil {
			return err
		}
		if !contains(sub.ItemIDs, itemID) {
			return errors.New("butir di luar penugasan panelis")
		}
		history, err := s.store.ListRounds(tx, p.ID, sub.Code)
		if err != nil {
			return err
		}
		prior := false
		for _, r := range history {
			if r.Number < round.Number && r.Status == "finalized" && r.FinalSnapshot != nil {
				prior = true
			}
		}
		if !prior {
			return errors.New("hasil putaran sebelumnya belum tersedia")
		}
		st, ok := s.store.(discussionNoteStore)
		if !ok {
			return errors.New("penyimpanan catatan diskusi belum tersedia")
		}
		key, _ := json.Marshal([]any{id, p.ID, sub.Code, round.Number, domain.RoundRevision(round), itemID})
		result = domain.DiscussionNote{ID: fmt.Sprintf("discussion_%x", sha256.Sum256(key)), UserID: id, ProjectID: p.ID, SubjectCode: sub.Code, ItemID: itemID, RoundNumber: round.Number, RoundRevision: domain.RoundRevision(round), Version: input.Version + 1, Text: input.Text, UpdatedAt: time.Now().UTC()}
		if err := s.store.TouchProject(tx, p.ID); err != nil {
			return err
		}
		if err := st.SaveDiscussionNote(tx, result, input.Version); err != nil {
			return err
		}
		return s.recordAudit(tx, id, round.ID, p.ID, "discussion_note_update")
	})
	return result, err
}
