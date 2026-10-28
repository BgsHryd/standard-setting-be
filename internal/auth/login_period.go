package auth

import (
	"context"
	"errors"
	"time"

	"standard-setting/backend/internal/domain"
)

var ErrOutsideAssessmentPeriod = errors.New("Akun anda berada di luar jam penilaian")

// Only new PIC/Panelis logins depend on the assigned project's timestamps.
// The comparison uses absolute server time and does not add project-status rules.
func (s *Service) checkLoginPeriod(ctx context.Context, user domain.User, now time.Time) error {
	if user.Role != domain.RolePICMapel && user.Role != domain.RolePanelist {
		return nil
	}
	if user.ProjectID == "" {
		return ErrOutsideAssessmentPeriod
	}
	projects, ok := s.users.(interface {
		GetProject(context.Context, string) (domain.Project, error)
	})
	if !ok {
		return ErrSecurityUnavailable
	}
	project, err := projects.GetProject(ctx, user.ProjectID)
	if err != nil {
		return ErrSecurityUnavailable
	}
	if project.ID != user.ProjectID || project.StartDate.IsZero() || project.EndDate.IsZero() || !now.After(project.StartDate) || !now.Before(project.EndDate) {
		return ErrOutsideAssessmentPeriod
	}
	return nil
}
