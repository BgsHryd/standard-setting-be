package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"standard-setting/backend/internal/domain"
	"strings"
	"time"
)

// Legacy identities are deterministic until their first persisted configuration edit.
func ensureSlots(p *domain.Project) {
	for i := range p.Subjects {
		sub := &p.Subjects[i]
		if len(sub.Slots) > 0 {
			continue
		}
		sub.Slots = []domain.PersonnelSlot{{ID: sub.Code + "-pic", Role: domain.RolePICMapel, Name: sub.PICName, UserID: sub.PICUserID}}
		for n := 0; n < sub.PanelistCount; n++ {
			id, name := "", ""
			if n < len(sub.PanelistUserIDs) {
				id = sub.PanelistUserIDs[n]
			}
			if n < len(sub.PanelistNames) {
				name = sub.PanelistNames[n]
			}
			sub.Slots = append(sub.Slots, domain.PersonnelSlot{ID: fmt.Sprintf("%s-panelis-%d", sub.Code, n+1), Role: domain.RolePanelist, Name: name, UserID: id})
		}
	}
}
func itemStructure(p domain.Project) map[string][]string {
	r := map[string][]string{}
	for _, s := range p.Subjects {
		r[s.Code] = s.ItemIDs
	}
	return r
}
func (s *Service) SaveProject(ctx context.Context, input domain.Project, userID string) (domain.Project, error) {
	var result domain.Project
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		now := time.Now().UTC()
		p := input
		p.Name = strings.TrimSpace(input.Name)
		p.UpdatedAt = now
		p.WizardStep = max(1, min(5, p.WizardStep))
		p.ItemsEditable = false
		var old domain.Project
		users := []domain.User{}
		if p.ID == "" {
			p.ID = newID("project")
			p.Code = "SS-" + strings.ToUpper(p.ID[len(p.ID)-6:])
			p.CreatedBy = userID
			p.CreatedAt = now
			p.Status = domain.ProjectDraft
			p.Version = 1
			p.AccountsGenerated = false
			p.FeatureVersion = 1
			p.WorkflowVersion = 1
			if p.PlannedRounds == 0 {
				p.PlannedRounds = 3
			}
			p.SourceProjectID = ""
			if p.RoundingPolicy == "" {
				p.RoundingPolicy = "ceil_1pp"
			}
		} else {
			var err error
			old, err = s.store.GetProject(tx, p.ID)
			if err != nil {
				return err
			}
			if input.Version != old.Version {
				return errors.New("konfigurasi sudah berubah; muat ulang sebelum menyimpan")
			}
			if old.Status == domain.ProjectCompleted {
				old.EvaluationClosed = input.EvaluationClosed
				old.Version++
				old.UpdatedAt = now
				if err = s.store.TouchProject(tx, old.ID); err != nil {
					return err
				}
				if err = s.store.SaveProject(tx, old); err != nil {
					return err
				}
				result = old
				return s.recordAudit(tx, userID, old.ID, old.ID, "evaluation_policy_update")
			}
			if err := s.store.TouchProject(tx, p.ID); err != nil {
				return err
			}
			ensureSlots(&old)
			p.Code, p.CreatedBy, p.CreatedAt, p.Status, p.CompletedAt = old.Code, old.CreatedBy, old.CreatedAt, old.Status, old.CompletedAt
			p.Version = old.Version + 1
			p.FeatureVersion, p.SourceProjectID = old.FeatureVersion, old.SourceProjectID
			p.WorkflowVersion = old.WorkflowVersion
			if p.PlannedRounds != old.PlannedRounds || p.SimulationEnabled != old.SimulationEnabled {
				if old.Status != domain.ProjectDraft {
					return errors.New("pengaturan putaran dan latihan hanya dapat diubah saat Draft; PIC dapat menambah putaran")
				}
			}
			if p.RoundingPolicy == "" {
				p.RoundingPolicy = old.RoundingPolicy
			}
			p.WizardStep = max(p.WizardStep, old.WizardStep)
			if !reflect.DeepEqual(itemStructure(p), itemStructure(old)) {
				rows, err := s.store.ListSubmissions(tx, p.ID, "", 0, "")
				if err != nil {
					return err
				}
				if len(rows) > 0 {
					return errors.New("daftar butir terkunci: sudah ada peserta melakukan penilaian")
				}
			}
			users, err = s.store.ListUsers(tx, p.ID)
			if err != nil {
				return err
			}
		}
		if p.PlannedRounds < 0 || p.PlannedRounds > 20 || (p.WorkflowVersion >= 1 && p.PlannedRounds < 1) {
			return errors.New("jumlah putaran harus 1–20")
		}
		if p.RoundingPolicy != "" && p.RoundingPolicy != "none" && p.RoundingPolicy != "nearest_1pp" && p.RoundingPolicy != "ceil_1pp" {
			return errors.New("kebijakan pembulatan tidak valid")
		}
		if p.Subjects == nil {
			p.Subjects = []domain.ProjectSubject{}
		}
		if !p.StartDate.IsZero() && !p.EndDate.IsZero() && !p.EndDate.After(p.StartDate) {
			return errors.New("tanggal selesai tidak boleh sebelum tanggal mulai")
		}
		catalog, err := s.store.ListSubjects(tx)
		if err != nil {
			return err
		}
		byCode := map[string]domain.Subject{}
		for _, v := range catalog {
			byCode[v.Code] = v
		}
		selected := []string{}
		seenSubjects, seenItems, seenSlots := map[string]bool{}, map[string]bool{}, map[string]bool{}
		retained := map[string]bool{}
		allReady := len(p.Subjects) > 0
		for i := range p.Subjects {
			sub := &p.Subjects[i]
			cat, exists := byCode[sub.Code]
			if !exists || seenSubjects[sub.Code] {
				return errors.New("mata pelajaran tidak ditemukan atau ganda")
			}
			seenSubjects[sub.Code] = true
			sub.Name = cat.Name
			if sub.PanelistCount < 0 || sub.PanelistCount > 1000 {
				return errors.New("jumlah panelis harus 0–1000")
			}
			var previous domain.ProjectSubject
			for _, v := range old.Subjects {
				if v.Code == sub.Code {
					previous = v
				}
			}
			sub.PracticeStatus, sub.FinalNBK = previous.PracticeStatus, previous.FinalNBK
			if old.Status == domain.ProjectActive && previous.Code == "" && p.SimulationEnabled {
				sub.PracticeStatus = "open"
			}
			sub.CandidateDefinition = strings.TrimSpace(sub.CandidateDefinition)
			if len([]rune(sub.CandidateDefinition)) > 8000 {
				return errors.New("deskripsi kandidat maksimal 8.000 karakter")
			}
			sub.CandidateDefinition = domain.NormalizeCandidateDefinition(sub.CandidateDefinition)
			if len([]rune(sub.CandidateDefinition)) > 8000 {
				return errors.New("deskripsi kandidat maksimal 8.000 karakter")
			}
			sub.CandidateVersion = previous.CandidateVersion
			sub.CandidateHistory = append([]domain.CandidateDefinitionRevision(nil), previous.CandidateHistory...)
			if sub.CandidateDefinition != previous.CandidateDefinition {
				if previous.CandidateDefinition != "" && len(sub.CandidateHistory) == 0 {
					sub.CandidateHistory = append(sub.CandidateHistory, domain.CandidateDefinitionRevision{Version: previous.CandidateVersion, Definition: previous.CandidateDefinition, CreatedAt: old.UpdatedAt})
				}
				sub.CandidateVersion++
				sub.CandidateHistory = append(sub.CandidateHistory, domain.CandidateDefinitionRevision{Version: sub.CandidateVersion, Definition: sub.CandidateDefinition, ActorID: userID, CreatedAt: now})
				if err = s.recordAudit(tx, userID, sub.Code, p.ID, "candidate_definition_update"); err != nil {
					return err
				}
			}
			previousByID := map[string]domain.PersonnelSlot{}
			for _, slot := range previous.Slots {
				previousByID[slot.ID] = slot
			}
			inputSlots := sub.Slots
			sub.Slots = nil
			sub.PICUserID = ""
			sub.PanelistUserIDs = []string{}
			for n := 0; n <= sub.PanelistCount; n++ {
				role := domain.RolePanelist
				name := ""
				if n == 0 {
					role = domain.RolePICMapel
					name = sub.PICName
				} else if n-1 < len(sub.PanelistNames) {
					name = sub.PanelistNames[n-1]
				}
				var slot domain.PersonnelSlot
				if n < len(inputSlots) && inputSlots[n].ID != "" {
					slot = previousByID[inputSlots[n].ID]
					if slot.ID == "" && len(previous.Slots) > 0 && n < len(previous.Slots) {
						return errors.New("identitas personel berubah; muat ulang konfigurasi")
					}
				}
				if slot.ID == "" && n < len(previous.Slots) {
					slot = previous.Slots[n]
				}
				if slot.ID == "" {
					slot.ID = newID("slot")
				}
				if seenSlots[slot.ID] || slot.Role != "" && slot.Role != role {
					return errors.New("slot personel ganda atau role tidak cocok")
				}
				seenSlots[slot.ID] = true
				slot.Role = role
				slot.Name = strings.TrimSpace(name)
				if slot.UserID != "" {
					u, err := s.store.FindByID(tx, slot.UserID)
					if err != nil {
						return err
					}
					if u.Removed || u.ProjectID != p.ID || u.SubjectCode != sub.Code || u.Role != role {
						return errors.New("akun tidak sesuai penugasan")
					}
					retained[u.ID] = true
					u.Name = slot.Name
					u.ProjectName = p.Name
					u.SlotID = slot.ID
					u.UpdatedAt = now
					if err := s.store.SaveUser(tx, u); err != nil {
						return err
					}
					if n == 0 {
						sub.PICUserID = u.ID
					} else {
						sub.PanelistUserIDs = append(sub.PanelistUserIDs, u.ID)
					}
				} else {
					allReady = false
				}
				sub.Slots = append(sub.Slots, slot)
			}
			sub.PICName = sub.Slots[0].Name
			sub.PanelistNames = []string{}
			for _, slot := range sub.Slots[1:] {
				sub.PanelistNames = append(sub.PanelistNames, slot.Name)
			}
			if sub.ItemIDs == nil {
				sub.ItemIDs = []string{}
			}
			for _, id := range sub.ItemIDs {
				if seenItems[id] {
					return errors.New("ID butir tidak boleh dipilih dua kali")
				}
				seenItems[id] = true
				item, err := s.store.GetItem(tx, id)
				if err != nil {
					return fmt.Errorf("ID butir %s tidak ditemukan", id)
				}
				if item.SubjectCode != sub.Code {
					return fmt.Errorf("ID butir %s tidak sesuai mata pelajaran", id)
				}
				selected = append(selected, id)
			}
		}
		for _, u := range users {
			if !retained[u.ID] && !u.Removed {
				u.Active = false
				u.Removed = true
				u.AuthVersion++
				u.UpdatedAt = now
				if err := s.store.SaveUser(tx, u); err != nil {
					return err
				}
			}
		}
		p.AccountsGenerated = allReady
		if err := s.store.SyncProjectItems(tx, p.ID, p.Name, selected); err != nil {
			return err
		}
		if old.Status == domain.ProjectActive {
			for _, sub := range p.Subjects {
				rounds, err := s.store.ListRounds(tx, p.ID, sub.Code)
				if err != nil {
					return err
				}
				if len(rounds) == 0 {
					if _, err = s.openRound(tx, p.ID, sub.Code, 1); err != nil {
						return err
					}
				}
			}
		}
		if err := s.store.SaveProject(tx, p); err != nil {
			return err
		}
		result = p
		return s.recordAudit(tx, userID, p.ID, p.ID, "project_configuration_update")
	})
	return result, err
}

func (s *Service) ListSubjects(ctx context.Context) ([]domain.Subject, error) {
	subjects, err := s.store.ListSubjects(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.store.ListItems(ctx, "", "", false)
	if err != nil {
		return nil, err
	}
	for i := range subjects {
		for _, item := range items {
			if item.SubjectCode != subjects[i].Code {
				continue
			}
			subjects[i].Total++
			switch item.UsageStatus {
			case domain.ItemAvailable:
				subjects[i].Available++
			case domain.ItemAllocated:
				subjects[i].Allocated++
			case domain.ItemTested:
				subjects[i].Tested++
			}
		}
	}
	return subjects, nil
}

var ErrInvalidSubject = errors.New("mata pelajaran tidak valid")
var subjectCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_-]{1,19}$`)

func (s *Service) CreateSubject(ctx context.Context, name, code string) (domain.Subject, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if !subjectCodePattern.MatchString(code) {
		return domain.Subject{}, fmt.Errorf("%w: kode mapel wajib 2–20 huruf/angka, boleh - atau _, dan diawali huruf", ErrInvalidSubject)
	}
	name = strings.Join(strings.Fields(name), " ")
	if len([]rune(name)) < 2 || len([]rune(name)) > 100 {
		return domain.Subject{}, fmt.Errorf("%w: nama mata pelajaran harus 2–100 karakter", ErrInvalidSubject)
	}
	sub := domain.Subject{Code: code, Name: name, NormalizedName: domain.NormalizeSubjectName(name)}
	return sub, s.store.CreateSubject(ctx, sub)
}
