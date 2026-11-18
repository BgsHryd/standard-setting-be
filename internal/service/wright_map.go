package service

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"standard-setting/backend/internal/domain"
)

// GridFS uploads occur outside the transaction; the store commits the current-file
// pointer only after authorize runs inside the shared project transaction.
type wrightMapStore interface {
	GetWrightMap(context.Context, string, string) (*domain.WrightMap, error)
	ReplaceWrightMap(context.Context, string, string, int, domain.WrightMap, func(context.Context) error) (*domain.WrightMap, error)
}

type WrightMapInput struct {
	FileName string `json:"fileName"`
	Text     string `json:"text"`
	Version  int    `json:"version"`
}

func (s *Service) wrightMapAccess(ctx context.Context, actor, projectID, subject string, write bool) error {
	u, err := s.store.FindByID(ctx, actor)
	if err != nil || !u.Active || u.Removed {
		return domain.ErrWrightMapForbidden
	}
	if u.Role != domain.RoleAdminSuper {
		if write || (u.Role != domain.RolePICMapel && u.Role != domain.RolePanelist) || u.ProjectID != projectID || u.SubjectCode != subject {
			return domain.ErrWrightMapForbidden
		}
	}
	p, err := s.store.GetProject(ctx, projectID)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return domain.ErrWrightMapNotFound
	}
	if err != nil {
		return err
	}
	assigned := false
	for _, sub := range p.Subjects {
		if sub.Code != subject {
			continue
		}
		if u.Role == domain.RoleAdminSuper || (u.Role == domain.RolePICMapel && sub.PICUserID == u.ID) {
			assigned = true
		}
		if u.Role == domain.RolePanelist {
			for _, id := range sub.PanelistUserIDs {
				if id == u.ID {
					assigned = true
				}
			}
		}
	}
	if !assigned {
		if u.Role == domain.RoleAdminSuper {
			return domain.ErrWrightMapNotFound
		}
		return domain.ErrWrightMapForbidden
	}
	if write && p.Status == domain.ProjectCompleted {
		return errors.New("Wright map proyek selesai tidak dapat diubah")
	}
	return nil
}

func (s *Service) WrightMap(ctx context.Context, actor, projectID, subject string) (*domain.WrightMap, error) {
	if err := s.wrightMapAccess(ctx, actor, projectID, subject, false); err != nil {
		return nil, err
	}
	st, ok := s.store.(wrightMapStore)
	if !ok {
		return nil, errors.New("penyimpanan Wright map tidak tersedia")
	}
	return st.GetWrightMap(ctx, projectID, subject)
}

func (s *Service) SaveWrightMap(ctx context.Context, actor, projectID, subject string, input WrightMapInput) (*domain.WrightMap, error) {
	if err := s.wrightMapAccess(ctx, actor, projectID, subject, true); err != nil {
		return nil, err
	}
	if input.Version < 0 {
		return nil, errors.New("versi Wright map tidak valid")
	}
	if err := domain.ValidateWrightMap(input.FileName, input.Text); err != nil {
		return nil, err
	}
	st, ok := s.store.(wrightMapStore)
	if !ok {
		return nil, errors.New("penyimpanan Wright map tidak tersedia")
	}
	return st.ReplaceWrightMap(ctx, projectID, subject, input.Version, domain.WrightMap{FileName: input.FileName, Text: input.Text, UpdatedAt: time.Now().UTC(), Version: input.Version + 1}, func(tx context.Context) error {
		if err := s.wrightMapAccess(tx, actor, projectID, subject, true); err != nil {
			return err
		}
		return s.store.TouchProject(tx, projectID)
	})
}
