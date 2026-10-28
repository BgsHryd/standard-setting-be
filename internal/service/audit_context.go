package service

import (
	"context"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
)

// AuditContext describes current display names without changing immutable audit identifiers.
type AuditContext struct {
	TargetLabel string `json:"targetLabel,omitempty"`
	ProjectName string `json:"projectName,omitempty"`
	SubjectName string `json:"subjectName,omitempty"`
	RoundNumber int    `json:"roundNumber,omitempty"`
}

func (s *Service) AuditContexts(ctx context.Context, events []domain.AuditEvent) (map[string]AuditContext, error) {
	result := map[string]AuditContext{}
	projects := map[string]domain.Project{}
	loaded := map[string]bool{}
	rounds := map[string]map[string]domain.Round{}
	for _, event := range events {
		context := AuditContext{}
		if event.ProjectID == "" {
			result[event.ID] = context
			continue
		}
		if !loaded[event.ProjectID] {
			p, err := s.store.GetProject(ctx, event.ProjectID)
			if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
				return nil, err
			}
			projects[event.ProjectID] = p
			loaded[event.ProjectID] = true
		}
		p := projects[event.ProjectID]
		isRound := event.Action == "panelist_round_start" || event.Action == "discussion_note_update" || event.Action == "round_finalize" || event.Action == "round_rollback" || event.Action == "panelist_round_submit" || event.Action == "panelist_round_reopen" || event.Action == "nbk_final_lock"
		if p.ID == "" {
			context.ProjectName = "Proyek yang sudah dihapus"
			if event.TargetID == event.ProjectID {
				context.TargetLabel = context.ProjectName
			} else if isRound {
				context.TargetLabel = "Putaran pada proyek yang sudah dihapus"
			}
			result[event.ID] = context
			continue
		}
		context.ProjectName = p.Name
		if event.TargetID == p.ID {
			context.TargetLabel = "Proyek " + p.Name
		}
		for _, subject := range p.Subjects {
			if subject.Code == event.TargetID && (event.Action == "candidate_definition_update" || event.Action == "practice_end") {
				context.SubjectName = subject.Name
				context.TargetLabel = subject.Name + " · Proyek " + p.Name
				break
			}
		}
		if isRound {
			if rounds[p.ID] == nil {
				rounds[p.ID] = map[string]domain.Round{}
				for _, subject := range p.Subjects {
					rows, err := s.store.ListRounds(ctx, p.ID, subject.Code)
					if err != nil {
						return nil, err
					}
					for _, r := range rows {
						rounds[p.ID][r.ID] = r
						if r.FinalSnapshot != nil {
							rounds[p.ID][r.FinalSnapshot.ID] = r
						}
						for _, snapshot := range r.Snapshots {
							rounds[p.ID][snapshot.ID] = r
						}
					}
				}
			}
			r, found := rounds[p.ID][event.TargetID]
			if found {
				context.RoundNumber = r.Number
				for _, subject := range p.Subjects {
					if subject.Code == r.SubjectCode {
						context.SubjectName = subject.Name
						break
					}
				}
				if context.SubjectName == "" {
					context.SubjectName = "Mata pelajaran tidak lagi tersedia"
				}
				context.TargetLabel = fmt.Sprintf("Putaran %d · %s · Proyek %s", r.Number, context.SubjectName, p.Name)
			} else {
				context.TargetLabel = "Putaran tidak lagi tersedia · Proyek " + p.Name
			}
		}
		result[event.ID] = context
	}
	return result, nil
}
