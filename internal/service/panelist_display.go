package service

import (
	"context"
	"errors"
	"time"

	"standard-setting/backend/internal/domain"
)

type PanelistDisplayInput struct {
	ShowKJ         bool `json:"showKJ"`
	ShowMeasure    bool `json:"showMeasure"`
	ProjectVersion int  `json:"projectVersion"`
}

type PanelistDisplayView struct {
	ShowKJ         bool `json:"showKJ"`
	ShowMeasure    bool `json:"showMeasure"`
	ProjectVersion int  `json:"projectVersion"`
}

// SetPanelistDisplay derives the project and subject from the current PIC assignment.
func (s *Service) SetPanelistDisplay(ctx context.Context, actor string, input PanelistDisplayInput) (PanelistDisplayView, error) {
	var result PanelistDisplayView
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, err := s.store.FindByID(tx, actor)
		if err != nil {
			return err
		}
		if !u.Active || u.Removed || u.Role != domain.RolePICMapel {
			return errors.New("hanya PIC mapel aktif dapat mengubah tampilan panelis")
		}
		p, err := s.store.GetProject(tx, u.ProjectID)
		if err != nil {
			return err
		}
		index := -1
		for i, sub := range p.Subjects {
			if sub.Code == u.SubjectCode && sub.PICUserID == u.ID {
				index = i
				break
			}
		}
		if index < 0 {
			return errors.New("PIC tidak ditugaskan pada mata pelajaran ini")
		}
		if p.Status == domain.ProjectCompleted {
			return errors.New("konfigurasi proyek selesai tidak dapat diubah")
		}
		if input.ProjectVersion != p.Version {
			return ErrRevisionConflict
		}
		if err := s.store.TouchProject(tx, p.ID); err != nil {
			return err
		}
		p.Subjects[index].ShowKJ = input.ShowKJ
		p.Subjects[index].ShowMeasure = input.ShowMeasure
		p.Version++
		p.UpdatedAt = time.Now().UTC()
		if err := s.store.SaveProject(tx, p); err != nil {
			return err
		}
		if err := s.recordAudit(tx, actor, p.ID, p.ID, "panelist_display_update"); err != nil {
			return err
		}
		result = PanelistDisplayView{ShowKJ: input.ShowKJ, ShowMeasure: input.ShowMeasure, ProjectVersion: p.Version}
		return nil
	})
	return result, err
}
