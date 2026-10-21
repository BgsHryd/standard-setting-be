package service

import (
	"context"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/mongo"
	"reflect"
	"standard-setting/backend/internal/domain"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrRevisionConflict = domain.ErrRevisionConflict
	ErrContactRequired  = errors.New("lengkapi minimal satu kontak WhatsApp atau email sebelum mengisi borang")
	ErrPeriodClosed     = errors.New("pengisian borang berada di luar periode proyek")
)

type contactStore interface {
	GetContact(context.Context, string) (domain.PanelistContact, error)
	SaveContact(context.Context, domain.PanelistContact, int) error
	RequiresContact(context.Context, string) (bool, error)
}
type ContactInput struct {
	WhatsApp string `json:"whatsapp"`
	Email    string `json:"email"`
	Version  int    `json:"version"`
}
type ContactView struct {
	domain.PanelistContact
	Required bool `json:"required"`
	Valid    bool `json:"valid"`
}

func (s *Service) contactPolicy(ctx context.Context, id string) error {
	if st, ok := s.store.(contactStore); ok {
		required, err := st.RequiresContact(ctx, id)
		if err != nil {
			return err
		}
		if required {
			return ErrContactRequired
		}
	}
	return nil
}
func (s *Service) ContactRequired(ctx context.Context, id string) (bool, error) {
	st, ok := s.store.(contactStore)
	if !ok {
		return false, nil
	}
	return st.RequiresContact(ctx, id)
}
func (s *Service) ownPanelist(ctx context.Context, id string) (domain.User, domain.Project, error) {
	u, err := s.store.FindByID(ctx, id)
	if err != nil || !u.Active || u.Removed || u.Role != domain.RolePanelist {
		return u, domain.Project{}, errors.New("akun bukan panelis aktif")
	}
	p, err := s.store.GetProject(ctx, u.ProjectID)
	if err != nil {
		return u, p, err
	}
	sub, err := subjectOf(p, u.SubjectCode)
	if err != nil || !contains(sub.PanelistUserIDs, id) {
		return u, p, errors.New("panelis tidak ditugaskan")
	}
	return u, p, nil
}
func (s *Service) Contact(ctx context.Context, id string) (ContactView, error) {
	u, p, err := s.ownPanelist(ctx, id)
	if err != nil {
		return ContactView{}, err
	}
	st, ok := s.store.(contactStore)
	if !ok {
		return ContactView{}, errors.New("penyimpanan kontak belum tersedia")
	}
	c, err := st.GetContact(ctx, id)
	if errors.Is(err, mongo.ErrNoDocuments) {
		c = domain.PanelistContact{UserID: id, ProjectID: p.ID, SubjectCode: u.SubjectCode}
		err = nil
	}
	if err != nil {
		return ContactView{}, err
	}
	_, _, invalid := domain.NormalizeContact(c.WhatsApp, c.Email)
	return ContactView{c, p.CollectContactPanelis, invalid == nil}, nil
}
func (s *Service) UpdateContact(ctx context.Context, id string, input ContactInput) (ContactView, error) {
	var result ContactView
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		u, p, err := s.ownPanelist(tx, id)
		if err != nil {
			return err
		}
		st, ok := s.store.(contactStore)
		if !ok {
			return errors.New("penyimpanan kontak belum tersedia")
		}
		wa, email, err := domain.NormalizeContact(input.WhatsApp, input.Email)
		if !p.CollectContactPanelis && strings.TrimSpace(input.WhatsApp) == "" && strings.TrimSpace(input.Email) == "" {
			wa, email, err = "", "", nil
		}
		if err != nil {
			return err
		}
		old, err := st.GetContact(tx, id)
		if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
			return err
		}
		if old.Version != input.Version {
			return ErrRevisionConflict
		}
		if err = s.store.TouchProject(tx, p.ID); err != nil {
			return err
		}
		c := domain.PanelistContact{UserID: id, ProjectID: p.ID, SubjectCode: u.SubjectCode, WhatsApp: wa, Email: email, Version: old.Version + 1, UpdatedAt: time.Now().UTC()}
		if err = st.SaveContact(tx, c, input.Version); err != nil {
			return err
		}
		if err = s.recordAudit(tx, id, id, p.ID, "contact_update"); err != nil {
			return err
		}
		result = ContactView{c, p.CollectContactPanelis, wa != "" || email != ""}
		return nil
	})
	return result, err
}

func (s *Service) recordAudit(ctx context.Context, actor, target, project, action string) error {
	if st, ok := s.store.(interface {
		RecordAudit(context.Context, domain.AuditEvent) error
	}); ok {
		return st.RecordAudit(ctx, domain.AuditEvent{ID: newID("audit"), ActorID: actor, TargetID: target, ProjectID: project, Action: action, Outcome: "success", RequestID: domain.AuditRequestID(ctx), CreatedAt: time.Now().UTC()})
	}
	return nil
}

func effectivePanelists(p domain.Project, sub domain.ProjectSubject, users []domain.User, round domain.Round) ([]domain.User, error) {
	assigned, err := assignedPanelists(p, sub, users)
	if err != nil {
		return nil, err
	}
	filtered := []domain.User{}
	for _, u := range assigned {
		if !roundExcluded(round, u.ID) {
			filtered = append(filtered, u)
		}
	}
	return filtered, nil
}
func roundExcluded(round domain.Round, id string) bool {
	for _, e := range round.ExcludedPanelists {
		if e.PanelistID == id {
			return true
		}
	}
	return false
}
func (s *Service) SetRoundExclusion(ctx context.Context, actor, roundID, panelistID, reason string, revision int, excluded bool) error {
	return s.store.WithTransaction(ctx, func(tx context.Context) error {
		pic, err := s.store.FindByID(tx, actor)
		if err != nil || pic.Role != domain.RolePICMapel || !pic.Active || pic.Removed {
			return errors.New("hanya PIC mapel yang dapat mengubah peserta putaran")
		}
		p, err := s.store.GetProject(tx, pic.ProjectID)
		if err != nil {
			return err
		}
		if p.Status != domain.ProjectActive {
			return errors.New("proyek tidak aktif")
		}
		sub, err := subjectOf(p, pic.SubjectCode)
		if err != nil || sub.PICUserID != pic.ID {
			return errors.New("PIC tidak ditugaskan")
		}
		r, err := s.store.CurrentRound(tx, p.ID, pic.SubjectCode)
		if err != nil || r.ID != roundID || domain.RoundRevision(r) != revision {
			return ErrRevisionConflict
		}
		user, err := s.store.FindByID(tx, panelistID)
		if err != nil || user.Role != domain.RolePanelist || user.ProjectID != p.ID || user.SubjectCode != pic.SubjectCode || !contains(sub.PanelistUserIDs, user.ID) || !user.Active || user.Removed {
			return errors.New("panelis tidak ditugaskan atau tidak aktif")
		}
		reason = strings.TrimSpace(reason)
		if excluded && (reason == "" || utf8.RuneCountInString(reason) > 1000) {
			return errors.New("alasan wajib diisi, maksimal 1.000 karakter")
		}
		if err = s.store.TouchProject(tx, p.ID); err != nil {
			return err
		}
		entries := []domain.RoundExclusion{}
		for _, e := range r.ExcludedPanelists {
			if e.PanelistID != panelistID {
				entries = append(entries, e)
			}
		}
		action := "round_include"
		if excluded {
			action = "round_exclude"
			entries = append(entries, domain.RoundExclusion{PanelistID: panelistID, Reason: reason, ActorID: actor, CreatedAt: time.Now().UTC()})
		}
		r.ExcludedPanelists = entries
		r.UpdatedAt = time.Now().UTC()
		if err = s.store.SaveRound(tx, r); err != nil {
			return err
		}
		return s.recordAudit(tx, actor, panelistID, p.ID, action)
	})
}

type SubmissionOptions struct {
	RoundRevision    int    `json:"roundRevision"`
	CandidateVersion int    `json:"candidateVersion"`
	Version          int    `json:"version"`
	Note             string `json:"note"`
	Reviewed         bool   `json:"reviewed"`
	IdempotencyKey   string `json:"idempotencyKey"`
	legacy           bool
}

func (s *Service) SaveSubmissionWithOptions(ctx context.Context, userID, name, projectID, subjectCode string, number int, itemID string, answer domain.Answer, input SubmissionOptions) (domain.Submission, error) {
	var result domain.Submission
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		p, err := s.store.GetProject(tx, projectID)
		if err != nil {
			return err
		}
		if !domain.ProjectWritable(p, time.Now().UTC()) {
			return ErrPeriodClosed
		}
		sub, err := subjectOf(p, subjectCode)
		if err != nil {
			return err
		}
		u, err := s.store.FindByID(tx, userID)
		if err != nil || !u.Active || u.Removed || u.Role != domain.RolePanelist || u.ProjectID != p.ID || u.SubjectCode != subjectCode || !contains(sub.PanelistUserIDs, userID) {
			return errors.New("panelis tidak ditugaskan pada proyek/mapel ini")
		}
		if err = s.contactPolicy(tx, userID); err != nil {
			return err
		}
		if !contains(sub.ItemIDs, itemID) {
			return errors.New("ID butir bukan bagian alokasi mapel")
		}
		item, err := s.store.GetItem(tx, itemID)
		if err != nil {
			return err
		}
		if item.ProjectID != p.ID || item.SubjectCode != subjectCode || item.ReadOnly || item.UsageStatus != domain.ItemAllocated {
			return errors.New("ID butir tidak tersedia")
		}
		if err = validateItemAnswer(item, answer); err != nil {
			return err
		}
		if err = domain.ValidateNote(input.Note); err != nil {
			return err
		}
		r, err := s.store.CurrentRound(tx, p.ID, subjectCode)
		if err != nil || r.Number != number || r.Status != "active" {
			return ErrRevisionConflict
		}
		revision := domain.RoundRevision(r)
		if (input.RoundRevision != revision && !(input.RoundRevision == 0 && revision == 1)) || roundExcluded(r, userID) {
			return ErrRevisionConflict
		}
		if p.FeatureVersion >= 1 && input.CandidateVersion != sub.CandidateVersion {
			return ErrRevisionConflict
		}
		rows, err := s.store.ListSubmissions(tx, p.ID, subjectCode, number, userID)
		if err != nil {
			return err
		}
		old := domain.Submission{}
		for _, row := range rows {
			if row.ItemID == itemID {
				old = row
				break
			}
		}
		if input.IdempotencyKey != "" && input.IdempotencyKey == old.IdempotencyKey {
			if !reflect.DeepEqual(answer, old.Answer) || strings.TrimSpace(input.Note) != old.Note {
				return ErrRevisionConflict
			}
			result = old
			return nil
		}
		if !input.legacy && input.Version != old.Version {
			return ErrRevisionConflict
		}
		if len(input.IdempotencyKey) > 128 {
			return errors.New("idempotency key terlalu panjang")
		}
		if err = s.store.TouchProject(tx, p.ID); err != nil {
			return err
		}
		if err = s.store.TouchRound(tx, r); err != nil {
			return err
		}
		review := old.ReviewRevision
		if revision == 1 || input.Reviewed {
			review = revision
		}
		id := old.ID
		if id == "" {
			id = newID("submission")
		}
		row := domain.Submission{ID: id, ProjectID: p.ID, SubjectCode: subjectCode, RoundNumber: number, PanelistID: userID, PanelistName: u.Name, ItemID: itemID, Answer: answer, Status: "saved", Version: old.Version + 1, RoundRevision: revision, ReviewRevision: review, CandidateVersion: sub.CandidateVersion, Note: strings.TrimSpace(input.Note), IdempotencyKey: input.IdempotencyKey, UpdatedAt: time.Now().UTC()}
		result, err = s.store.UpsertSubmission(tx, row)
		return err
	})
	return result, err
}
func (s *Service) ReviewSubmission(ctx context.Context, user, project, subject string, number int, item string, revision, version int, candidateVersion ...int) (domain.Submission, error) {
	rows, err := s.store.ListSubmissions(ctx, project, subject, number, user)
	if err != nil {
		return domain.Submission{}, err
	}
	p, err := s.store.GetProject(ctx, project)
	if err != nil {
		return domain.Submission{}, err
	}
	sub, err := subjectOf(p, subject)
	if err != nil {
		return domain.Submission{}, err
	}
	if p.FeatureVersion >= 1 && (len(candidateVersion) == 0 || candidateVersion[0] != sub.CandidateVersion) {
		return domain.Submission{}, ErrRevisionConflict
	}
	for _, row := range rows {
		if row.ItemID == item {
			return s.SaveSubmissionWithOptions(ctx, user, "", project, subject, number, item, row.Answer, SubmissionOptions{RoundRevision: revision, Version: version, Note: row.Note, Reviewed: true, CandidateVersion: sub.CandidateVersion})
		}
	}
	return domain.Submission{}, errors.New("jawaban belum tersimpan untuk ditinjau")
}

type ItemFeedback struct {
	ItemID      string    `json:"itemId"`
	RoundNumber int       `json:"roundNumber"`
	Revision    int       `json:"revision"`
	N           int       `json:"n"`
	Median      float64   `json:"median"`
	Minimum     float64   `json:"minimum"`
	Maximum     float64   `json:"maximum"`
	Values      []float64 `json:"values"`
	OwnValue    *float64  `json:"ownValue,omitempty"`
	SmallPanel  bool      `json:"smallPanel"`
}

func snapshotFeedback(snap domain.RoundSnapshot, itemID, userID string) ItemFeedback {
	stats := snap.ItemStatistics[itemID]
	f := ItemFeedback{ItemID: itemID, RoundNumber: snap.RoundNumber, Revision: snap.Revision, N: stats.PanelistCount, Median: stats.Median, Minimum: stats.Minimum, Maximum: stats.Maximum, Values: []float64{}, SmallPanel: stats.PanelistCount < 3}
	for _, row := range snap.Submissions {
		if row.ItemID != itemID || !contains(snap.PanelistIDs, row.PanelistID) {
			continue
		}
		v := 0.0
		// Item values can be reconstructed from the stored answer shape; distributions sum to 100.
		maxScore := len(row.Answer.Distribution) - 1
		if maxScore < 1 {
			maxScore = 1
		}
		v = responseValue(row.Answer, maxScore)
		if row.PanelistID == userID {
			f.OwnValue = &v
		}
		if !f.SmallPanel {
			f.Values = append(f.Values, v)
		}
	}
	return f
}
func (s *Service) feedback(ctx context.Context, user string, p domain.Project, sub domain.ProjectSubject, current domain.Round, history []domain.Round) []ItemFeedback {
	result := []ItemFeedback{}
	for i := len(history) - 1; i >= 0; i-- {
		r := history[i]
		if r.Number >= current.Number || r.Status != "finalized" || r.FinalSnapshot == nil {
			continue
		}
		for _, id := range sub.ItemIDs {
			if _, ok := r.FinalSnapshot.ItemStatistics[id]; ok {
				result = append(result, snapshotFeedback(*r.FinalSnapshot, id, user))
			}
		}
		break
	}
	return result
}
func (s *Service) Discussion(ctx context.Context, user, itemID string, number int) (ItemFeedback, error) {
	pic, err := s.store.FindByID(ctx, user)
	if err != nil || !pic.Active || pic.Removed || pic.Role != domain.RolePICMapel {
		return ItemFeedback{}, errors.New("hanya PIC mapel")
	}
	p, err := s.store.GetProject(ctx, pic.ProjectID)
	if err != nil {
		return ItemFeedback{}, err
	}
	sub, err := subjectOf(p, pic.SubjectCode)
	if err != nil || sub.PICUserID != user || !contains(sub.ItemIDs, itemID) {
		return ItemFeedback{}, errors.New("butir di luar penugasan PIC")
	}
	history, err := s.store.ListRounds(ctx, p.ID, sub.Code)
	if err != nil {
		return ItemFeedback{}, err
	}
	for i := len(history) - 1; i >= 0; i-- {
		r := history[i]
		if (number == 0 || r.Number == number) && r.Status == "finalized" && r.FinalSnapshot != nil {
			f := snapshotFeedback(*r.FinalSnapshot, itemID, "")
			f.OwnValue = nil
			return f, nil
		}
	}
	return ItemFeedback{}, errors.New("snapshot putaran final belum tersedia")
}
func (s *Service) freezeRound(ctx context.Context, p domain.Project, sub domain.ProjectSubject, r domain.Round) (domain.RoundSnapshot, error) {
	rows, items, err := s.completeResponses(ctx, p, sub, r.Number)
	if err != nil {
		return domain.RoundSnapshot{}, err
	}
	all, err := s.store.ListSubmissions(ctx, p.ID, sub.Code, r.Number, "")
	if err != nil {
		return domain.RoundSnapshot{}, err
	}
	snap := domain.RoundSnapshot{ID: fmt.Sprintf("%s-r%d", r.ID, domain.RoundRevision(r)), Revision: domain.RoundRevision(r), RoundNumber: r.Number, SubjectCode: sub.Code, PanelistIDs: []string{}, ItemStatistics: map[string]domain.ItemResult{}, Submissions: all, CandidateDefinition: sub.CandidateDefinition, CandidateVersion: sub.CandidateVersion, RoundingPolicy: projectRoundingPolicy(p), CreatedAt: time.Now().UTC()}
	for _, row := range rows {
		if !contains(snap.PanelistIDs, row.PanelistID) {
			snap.PanelistIDs = append(snap.PanelistIDs, row.PanelistID)
		}
	}
	users, err := s.store.ListUsers(ctx, p.ID)
	if err != nil {
		return domain.RoundSnapshot{}, err
	}
	snap.Roster = []domain.SnapshotPanelist{}
	for _, u := range users {
		if u.Role != domain.RolePanelist || u.SubjectCode != sub.Code {
			continue
		}
		reason := ""
		for _, e := range r.ExcludedPanelists {
			if e.PanelistID == u.ID {
				reason = e.Reason
				break
			}
		}
		snap.Roster = append(snap.Roster, domain.SnapshotPanelist{PanelistID: u.ID, PanelistName: u.Name, Included: contains(snap.PanelistIDs, u.ID), Active: u.Active, Removed: u.Removed, ExclusionReason: reason})
	}
	scores := []float64{}
	for _, id := range snap.PanelistIDs {
		weighted, total := 0.0, 0.0
		for _, item := range items {
			for _, row := range rows {
				if row.ItemID == item.ID && row.PanelistID == id {
					weighted += responseValue(row.Answer, item.MaxScore) * float64(item.MaxScore)
					total += float64(item.MaxScore)
				}
			}
		}
		scores = append(scores, weighted/total)
	}
	snap.Statistics = domain.ComputeStatistics(scores, projectRoundingPolicy(p))
	snap.Statistics.RoundNumber = r.Number
	snap.Statistics.Revision = snap.Revision
	snap.Statistics.PanelistIDs = snap.PanelistIDs
	snap.Statistics.CompletedAt = snap.CreatedAt
	for _, item := range items {
		values := []float64{}
		for _, row := range rows {
			if row.ItemID == item.ID {
				values = append(values, responseValue(row.Answer, item.MaxScore))
			}
		}
		stats := domain.ComputeStatistics(values, projectRoundingPolicy(p))
		stats.PanelistIDs = snap.PanelistIDs
		stats.RoundNumber = r.Number
		stats.Revision = snap.Revision
		stats.CompletedAt = snap.CreatedAt
		snap.ItemStatistics[item.ID] = stats
	}
	return snap, nil
}
func (s *Service) CopyProject(ctx context.Context, id, name, actor string) (domain.Project, error) {
	var result domain.Project
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		source, err := s.store.GetProject(tx, id)
		if err != nil {
			return err
		}
		name = strings.TrimSpace(name)
		if name == "" || utf8.RuneCountInString(name) > 200 {
			return errors.New("nama proyek baru wajib diisi, maksimal 200 karakter")
		}
		subjs := []domain.ProjectSubject{}
		for _, sub := range source.Subjects {
			subjs = append(subjs, domain.ProjectSubject{Code: sub.Code, Name: sub.Name, CandidateDefinition: sub.CandidateDefinition, CandidateVersion: 1, PanelistCount: sub.PanelistCount, PanelistNames: append([]string(nil), sub.PanelistNames...), PICName: sub.PICName, ItemIDs: []string{}, PanelistUserIDs: []string{}})
		}
		result, err = s.SaveProject(tx, domain.Project{Name: name, Description: source.Description, Subjects: subjs, CollectContactPanelis: source.CollectContactPanelis, RoundingPolicy: source.RoundingPolicy}, actor)
		if err != nil {
			return err
		}
		result.SourceProjectID = id
		if err = s.store.SaveProject(tx, result); err != nil {
			return err
		}
		return s.recordAudit(tx, actor, result.ID, result.ID, "project_copy")
	})
	return result, err
}

func projectRoundingPolicy(p domain.Project) string {
	if p.RoundingPolicy != "" {
		return p.RoundingPolicy
	}
	if p.FeatureVersion < 1 {
		return "none"
	}
	return "ceil_1pp"
}
