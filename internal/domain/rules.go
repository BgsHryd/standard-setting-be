package domain

import (
	"fmt"
	"time"
)

func ValidateAnswer(answer Answer) error {
	switch answer.QuestionType {
	case QuestionTypePG:
		if answer.Value == nil || *answer.Value < 0 || *answer.Value > 100 {
			return fmt.Errorf("nilai Pilihan Ganda harus berada pada 0 sampai 100")
		}
		if len(answer.Distribution) > 0 {
			return fmt.Errorf("Pilihan Ganda tidak menerima distribusi skor")
		}
	case QuestionTypeComplex, QuestionTypeComplexMCMA, QuestionTypeComplexBS, QuestionTypeEssay:
		if len(answer.Distribution) == 0 {
			return fmt.Errorf("distribusi skor wajib diisi")
		}
		total := 0
		for _, value := range answer.Distribution {
			if value < 0 || value > 100 {
				return fmt.Errorf("setiap nilai distribusi harus berada pada 0 sampai 100")
			}
			total += value
		}
		if total != 100 {
			return fmt.Errorf("jumlah distribusi kandidat harus tepat 100")
		}
	default:
		return fmt.Errorf("jenis soal tidak didukung")
	}

	return nil
}

func ValidateActivation(project Project) []string {
	errors := make([]string, 0)
	if project.Name == "" {
		errors = append(errors, "nama proyek wajib diisi")
	}
	if project.StartDate.IsZero() || project.EndDate.IsZero() || !project.EndDate.After(project.StartDate) {
		errors = append(errors, "periode proyek belum valid")
	}
	if len(project.Subjects) == 0 {
		errors = append(errors, "minimal satu mata pelajaran wajib dipilih")
	}
	for _, subject := range project.Subjects {
		if project.FeatureVersion >= 1 && NormalizeCandidateDefinition(subject.CandidateDefinition) == "" {
			errors = append(errors, fmt.Sprintf("deskripsi kandidat batas lulus %s wajib diisi", subject.Name))
		}
		if len(subject.ItemIDs) == 0 {
			errors = append(errors, fmt.Sprintf("ID butir %s belum dipilih", subject.Name))
		}
		if subject.PICUserID == "" {
			errors = append(errors, fmt.Sprintf("PIC %s belum ditentukan", subject.Name))
		}
		if subject.PanelistCount < 1 || len(subject.PanelistUserIDs) < subject.PanelistCount {
			errors = append(errors, fmt.Sprintf("panelis %s belum lengkap", subject.Name))
		}
	}
	if !project.AccountsGenerated {
		errors = append(errors, "akun proyek belum dibuat")
	}
	return errors
}

func AllocateItems(projectID string, items []Item) ([]Item, error) {
	updated := append([]Item(nil), items...)
	for index := range updated {
		item := &updated[index]
		if item.UsageStatus == ItemTested {
			return nil, fmt.Errorf("ID butir %s sudah diuji", item.ID)
		}
		if item.ProjectID != "" && item.ProjectID != projectID {
			return nil, fmt.Errorf("ID butir %s sudah dialokasikan ke proyek lain", item.ID)
		}
		item.ProjectID = projectID
		item.UsageStatus = ItemAllocated
		item.UpdatedAt = time.Now()
	}
	return updated, nil
}

func CompleteProject(project Project, items []Item) (Project, []Item, error) {
	if project.Status != ProjectActive {
		return project, nil, fmt.Errorf("hanya proyek aktif yang dapat diselesaikan")
	}

	now := time.Now()
	project.Status = ProjectCompleted
	project.CompletedAt = &now
	project.UpdatedAt = now
	updated := append([]Item(nil), items...)
	for index := range updated {
		if updated[index].ProjectID == project.ID {
			updated[index].UsageStatus = ItemTested
			updated[index].ReadOnly = true
			updated[index].UpdatedAt = now
		}
	}
	return project, updated, nil
}
