package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"standard-setting/backend/internal/domain"
)

type PICSubjectConfigInput struct {
	CandidateDefinition string `json:"candidateDefinition"`
	ProjectVersion      int    `json:"projectVersion"`
}

type PICSubjectConfigView struct {
	ProjectName         string `json:"projectName"`
	SubjectCode         string `json:"subjectCode"`
	SubjectName         string `json:"subjectName"`
	CandidateDefinition string `json:"candidateDefinition"`
	CandidateVersion    int    `json:"candidateVersion"`
	ProjectVersion      int    `json:"projectVersion"`
	ReadOnly            bool   `json:"readOnly"`
}

func (s *Service) picConfigAssignment(ctx context.Context, actor string) (domain.Project, int, error) {
	u, err := s.store.FindByID(ctx, actor)
	if err != nil {
		return domain.Project{}, -1, err
	}
	if !u.Active || u.Removed || u.Role != domain.RolePICMapel {
		return domain.Project{}, -1, errors.New("hanya PIC mapel aktif dapat mengakses konfigurasi mapel")
	}
	p, err := s.store.GetProject(ctx, u.ProjectID)
	if err != nil {
		return domain.Project{}, -1, err
	}
	if p.ID == u.ProjectID {
		for i, sub := range p.Subjects {
			if sub.Code == u.SubjectCode && sub.PICUserID == u.ID {
				return p, i, nil
			}
		}
	}
	return domain.Project{}, -1, errors.New("PIC tidak ditugaskan pada mata pelajaran ini")
}

func picSubjectConfigView(p domain.Project, index int) PICSubjectConfigView {
	sub := p.Subjects[index]
	return PICSubjectConfigView{ProjectName: p.Name, SubjectCode: sub.Code, SubjectName: sub.Name, CandidateDefinition: sub.CandidateDefinition, CandidateVersion: sub.CandidateVersion, ProjectVersion: p.Version, ReadOnly: p.Status == domain.ProjectCompleted}
}

func (s *Service) PICSubjectConfig(ctx context.Context, actor string) (PICSubjectConfigView, error) {
	p, index, err := s.picConfigAssignment(ctx, actor)
	if err != nil {
		return PICSubjectConfigView{}, err
	}
	return picSubjectConfigView(p, index), nil
}

func (s *Service) SavePICSubjectConfig(ctx context.Context, actor string, input PICSubjectConfigInput) (PICSubjectConfigView, error) {
	var result PICSubjectConfigView
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		p, index, err := s.picConfigAssignment(tx, actor)
		if err != nil {
			return err
		}
		if p.Status == domain.ProjectCompleted {
			return errors.New("konfigurasi proyek selesai tidak dapat diubah")
		}
		if input.ProjectVersion != p.Version {
			return ErrRevisionConflict
		}
		definition := strings.TrimSpace(input.CandidateDefinition)
		if len([]rune(definition)) > 8000 {
			return errors.New("deskripsi kandidat maksimal 8.000 karakter")
		}
		definition = domain.NormalizeCandidateDefinition(definition)
		if definition == "" {
			return errors.New("deskripsi kandidat batas lulus wajib diisi")
		}
		if len([]rune(definition)) > 8000 {
			return errors.New("deskripsi kandidat maksimal 8.000 karakter")
		}
		sub := &p.Subjects[index]
		if definition == sub.CandidateDefinition {
			result = picSubjectConfigView(p, index)
			return nil
		}
		if err := s.store.TouchProject(tx, p.ID); err != nil {
			return err
		}
		now := time.Now().UTC()
		if sub.CandidateDefinition != "" && len(sub.CandidateHistory) == 0 {
			sub.CandidateHistory = append(sub.CandidateHistory, domain.CandidateDefinitionRevision{Version: sub.CandidateVersion, Definition: sub.CandidateDefinition, CreatedAt: p.UpdatedAt})
		}
		sub.CandidateDefinition = definition
		sub.CandidateVersion++
		sub.CandidateHistory = append(sub.CandidateHistory, domain.CandidateDefinitionRevision{Version: sub.CandidateVersion, Definition: definition, ActorID: actor, CreatedAt: now})
		p.Version++
		p.UpdatedAt = now
		if err := s.store.SaveProject(tx, p); err != nil {
			return err
		}
		if err := s.recordAudit(tx, actor, sub.Code, p.ID, "candidate_definition_update"); err != nil {
			return err
		}
		result = picSubjectConfigView(p, index)
		return nil
	})
	return result, err
}
