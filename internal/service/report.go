package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"standard-setting/backend/internal/domain"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type ReportRound struct {
	Number   int    `json:"number"`
	Revision int    `json:"revision"`
	Status   string `json:"status"`
}
type ReportResponse struct {
	Roster     *domain.SnapshotPanelist `json:"roster,omitempty"`
	Submission domain.Submission        `json:"submission"`
	Included   bool                     `json:"included"`
	Reviewed   bool                     `json:"reviewed"`
	Missing    bool                     `json:"missing"`
}
type ReportSnapshotReference struct {
	ID               string            `json:"id"`
	RoundNumber      int               `json:"roundNumber"`
	Revision         int               `json:"revision"`
	Statistics       domain.ItemResult `json:"statistics"`
	RoundingPolicy   string            `json:"roundingPolicy"`
	CandidateVersion int               `json:"candidateVersion"`
	CreatedAt        time.Time         `json:"createdAt"`
}
type ReportSubject struct {
	RosterComplete      bool                         `json:"rosterComplete"`
	ArchiveSnapshots    []ReportSnapshotReference    `json:"archiveSnapshots"`
	Historical          bool                         `json:"historical"`
	Subject             domain.ProjectSubject        `json:"subject"`
	Round               ReportRound                  `json:"round"`
	SnapshotID          string                       `json:"snapshotId"`
	Statistics          domain.ItemResult            `json:"statistics"`
	ItemStatistics      map[string]domain.ItemResult `json:"itemStatistics"`
	RoundingPolicy      string                       `json:"roundingPolicy"`
	CandidateDefinition string                       `json:"candidateDefinition"`
	CandidateVersion    int                          `json:"candidateVersion"`
	Legacy              bool                         `json:"legacy"`
	Available           bool                         `json:"available"`
	Warning             string                       `json:"warning,omitempty"`
	Responses           []ReportResponse             `json:"responses"`
}
type ReportManifest struct {
	SchemaVersion     int       `json:"schemaVersion"`
	GeneratedAt       time.Time `json:"generatedAt"`
	RequestedRound    int       `json:"requestedRound"`
	RequestedRevision int       `json:"requestedRevision"`
	Source            string    `json:"source"`
}
type ProjectReport struct {
	Project     domain.Project  `json:"project"`
	Subjects    []ReportSubject `json:"subjects"`
	GeneratedAt time.Time       `json:"generatedAt"`
	Manifest    ReportManifest  `json:"manifest"`
}

func (s *Service) ProjectReport(ctx context.Context, id string, number int) (ProjectReport, error) {
	return s.ProjectReportRevision(ctx, id, number, 0)
}
func (s *Service) ProjectReportRevision(ctx context.Context, id string, number, revision int) (ProjectReport, error) {
	result := ProjectReport{Subjects: []ReportSubject{}, GeneratedAt: time.Now().UTC()}
	if number < 0 || revision < 0 || (revision > 0 && number == 0) {
		return result, errors.New("pilih nomor putaran untuk revisi snapshot")
	}
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		p, err := s.store.GetProject(tx, id)
		if err != nil {
			return err
		}
		result.Project = p
		result.Subjects = []ReportSubject{}
		for _, sub := range p.Subjects {
			r := ReportSubject{Subject: sub, ItemStatistics: map[string]domain.ItemResult{}, Responses: []ReportResponse{}, ArchiveSnapshots: []ReportSnapshotReference{}}
			rounds, err := s.store.ListRounds(tx, id, sub.Code)
			if err != nil {
				return err
			}
			var selected domain.Round
			var snapshot *domain.RoundSnapshot
			for _, round := range rounds {
				snapshots := append([]domain.RoundSnapshot(nil), round.Snapshots...)
				if round.FinalSnapshot != nil {
					found := false
					for _, snap := range snapshots {
						if snap.ID == round.FinalSnapshot.ID {
							found = true
						}
					}
					if !found {
						snapshots = append(snapshots, *round.FinalSnapshot)
					}
				}
				for _, snap := range snapshots {
					r.ArchiveSnapshots = append(r.ArchiveSnapshots, ReportSnapshotReference{ID: snap.ID, RoundNumber: round.Number, Revision: snap.Revision, Statistics: snap.Statistics, RoundingPolicy: snap.RoundingPolicy, CandidateVersion: snap.CandidateVersion, CreatedAt: snap.CreatedAt})
				}
				if number > 0 && round.Number != number {
					continue
				}
				if revision > 0 {
					for i := range snapshots {
						if snapshots[i].Revision == revision {
							copy := snapshots[i]
							snapshot = &copy
							selected = round
						}
					}
					continue
				}
				if number == 0 && round.Status != "finalized" {
					continue
				}
				if round.Number > selected.Number {
					selected = round
					snapshot = round.FinalSnapshot
				}
			}
			sort.Slice(r.ArchiveSnapshots, func(i, j int) bool {
				a, b := r.ArchiveSnapshots[i], r.ArchiveSnapshots[j]
				if a.RoundNumber != b.RoundNumber {
					return a.RoundNumber < b.RoundNumber
				}
				return a.Revision < b.Revision
			})
			r.Round = ReportRound{Number: selected.Number, Revision: selected.Revision, Status: selected.Status}
			if r.Round.Revision == 0 {
				r.Round.Revision = 1
			}
			if selected.Number == 0 || (revision == 0 && selected.Status != "finalized") {
				r.Warning = "Belum ada hasil final pada putaran/revisi ini"
				result.Subjects = append(result.Subjects, r)
				continue
			}
			r.Historical = snapshot != nil && (selected.Status != "finalized" || selected.FinalSnapshot == nil || selected.FinalSnapshot.ID != snapshot.ID)
			if r.Historical {
				r.Warning = "Snapshot revisi historis; bukan ambang operasional terbaru."
			}

			if snapshot == nil {
				r.Legacy = true
				r.Warning = "Data historis tanpa snapshot: roster dan asal statistik tidak lengkap; tidak dapat digunakan untuk dampak kelulusan."
				rows, err := s.store.ListSubmissions(tx, id, sub.Code, selected.Number, "")
				if err != nil {
					return err
				}
				for _, row := range rows {
					r.Responses = append(r.Responses, ReportResponse{Submission: row})
				}
			} else {
				r.Available = true
				r.SnapshotID = snapshot.ID
				r.Statistics = snapshot.Statistics
				r.ItemStatistics = snapshot.ItemStatistics
				r.RoundingPolicy = snapshot.RoundingPolicy
				r.CandidateDefinition = snapshot.CandidateDefinition
				r.CandidateVersion = snapshot.CandidateVersion
				r.Round.Revision = snapshot.Revision
				seen := map[string]bool{}
				roster := append([]domain.SnapshotPanelist(nil), snapshot.Roster...)
				r.RosterComplete = len(roster) > 0
				if !r.RosterComplete {
					for _, uid := range snapshot.PanelistIDs {
						roster = append(roster, domain.SnapshotPanelist{PanelistID: uid, Included: true})
					}
					if r.Warning != "" {
						r.Warning += " "
					}
					r.Warning += "Snapshot lama belum menyimpan roster lengkap; peserta tanpa respons yang tidak masuk statistik tidak dapat direkonstruksi."
				}
				rosterByID := map[string]domain.SnapshotPanelist{}
				for _, person := range roster {
					rosterByID[person.PanelistID] = person
				}
				for _, row := range snapshot.Submissions {
					reviewed := row.ReviewRevision == snapshot.Revision || (snapshot.Revision <= 1 && row.ReviewRevision == 0)
					response := ReportResponse{Submission: row, Included: contains(snapshot.PanelistIDs, row.PanelistID), Reviewed: reviewed}
					if person, ok := rosterByID[row.PanelistID]; ok && r.RosterComplete {
						response.Roster = &person
					}
					r.Responses = append(r.Responses, response)
					seen[row.PanelistID+"\x00"+row.ItemID] = true
				}
				for _, person := range roster {
					for itemID := range snapshot.ItemStatistics {
						if !seen[person.PanelistID+"\x00"+itemID] {
							row := domain.Submission{ProjectID: id, SubjectCode: sub.Code, PanelistID: person.PanelistID, PanelistName: person.PanelistName, ItemID: itemID, RoundNumber: snapshot.RoundNumber, CandidateVersion: snapshot.CandidateVersion}
							item, e := s.store.GetItem(tx, itemID)
							if e == nil {
								row.Answer.QuestionType = item.QuestionType
							}
							response := ReportResponse{Submission: row, Included: person.Included, Missing: true}
							if r.RosterComplete {
								response.Roster = &person
							}
							r.Responses = append(r.Responses, response)
						}
					}
				}
			}
			sort.Slice(r.Responses, func(i, j int) bool {
				a, b := r.Responses[i].Submission, r.Responses[j].Submission
				if a.PanelistID != b.PanelistID {
					return a.PanelistID < b.PanelistID
				}
				return a.ItemID < b.ItemID
			})
			result.Subjects = append(result.Subjects, r)
		}
		return nil
	})
	result.Manifest = ReportManifest{SchemaVersion: 1, GeneratedAt: result.GeneratedAt, RequestedRound: number, RequestedRevision: revision, Source: "final_snapshot; legacy rows explicitly labelled"}
	return result, err
}
func csvCell(value any, locale string) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case float64:
		s := strconv.FormatFloat(v, 'f', -1, 64)
		if locale == "id" {
			s = strings.ReplaceAll(s, ".", ",")
		}
		return s
	case *float64:
		if v == nil {
			return ""
		}
		return csvCell(*v, locale)
	case int:
		return strconv.Itoa(v)
	case bool:
		return strconv.FormatBool(v)
	}
	text := fmt.Sprint(value)
	trimmed := strings.TrimLeftFunc(text, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
	if len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + text
	}
	// Numeric text identifiers need protection from Excel type/precision guessing.
	if text != "" && strings.Trim(text, "0123456789") == "" {
		return "'" + text
	}
	return text
}
func encodeReportCSV(headers []string, rows [][]any, locale string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(&b)
	w.UseCRLF = true
	if locale == "id" {
		w.Comma = ';'
	}
	if err := w.Write(headers); err != nil {
		return nil, err
	}
	for _, row := range rows {
		values := make([]string, len(row))
		for i, v := range row {
			values[i] = csvCell(v, locale)
		}
		if err := w.Write(values); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return b.Bytes(), w.Error()
}
func (s *Service) ExportReport(ctx context.Context, id string, number int, locale, kind string) ([]byte, error) {
	return s.ExportReportRevision(ctx, id, number, 0, locale, kind)
}
func (s *Service) ExportReportRevision(ctx context.Context, id string, number, revision int, locale, kind string) ([]byte, error) {
	if locale != "id" && locale != "standard" {
		return nil, errors.New("format harus id atau standard")
	}
	if kind != "raw" && kind != "summary" {
		return nil, errors.New("jenis ekspor harus raw atau summary")
	}
	report, err := s.ProjectReportRevision(ctx, id, number, revision)
	if err != nil {
		return nil, err
	}
	rows := [][]any{}
	headers := []string{"schemaVersion", "projectId", "projectCode", "subjectCode", "round", "revision", "snapshotId", "legacy", "historical", "n", "cutoff", "roundingPolicy", "operationalCutoff", "mean", "median", "populationSD", "sampleSD", "standardErrorPP", "minimum", "maximum", "generatedAt"}
	for _, sub := range report.Subjects {
		if kind == "summary" {
			if !sub.Available && !sub.Legacy {
				continue
			}
			st := sub.Statistics
			var count, cutoff, operational, mean, median, sd, sample, se, minimum, maximum any
			if sub.Available {
				count = st.PanelistCount
				cutoff = st.Cutoff
				operational = st.OperationalCutoff
				mean = st.Mean
				median = st.Median
				sd = st.StandardDev
				sample = st.SampleStandardDeviation
				se = st.StandardError
				minimum = st.Minimum
				maximum = st.Maximum
			}
			rows = append(rows, []any{1, id, report.Project.Code, sub.Subject.Code, sub.Round.Number, sub.Round.Revision, sub.SnapshotID, sub.Legacy, sub.Historical, count, cutoff, sub.RoundingPolicy, operational, mean, median, sd, sample, se, minimum, maximum, report.GeneratedAt.Format(time.RFC3339)})
			continue
		}
		for _, response := range sub.Responses {
			row := response.Submission
			var val any
			if row.Answer.Value != nil {
				val = *row.Answer.Value
			}
			distribution := ""
			if len(row.Answer.Distribution) > 0 {
				b, _ := json.Marshal(row.Answer.Distribution)
				distribution = string(b)
			}
			updated := ""
			if !row.UpdatedAt.IsZero() {
				updated = row.UpdatedAt.Format(time.RFC3339Nano)
			}
			var included, reviewed, missing any = response.Included, response.Reviewed, response.Missing
			if sub.Legacy {
				included = nil
				reviewed = nil
				missing = nil
			}
			var active, removed, reason any
			if response.Roster != nil {
				active = response.Roster.Active
				removed = response.Roster.Removed
				reason = response.Roster.ExclusionReason
			}
			rows = append(rows, []any{1, id, report.Project.Code, sub.Subject.Code, sub.Round.Number, sub.Round.Revision, sub.SnapshotID, sub.Legacy, sub.Historical, row.PanelistID, row.PanelistName, row.ItemID, string(row.Answer.QuestionType), val, distribution, row.Status, included, reviewed, missing, row.Version, row.CandidateVersion, sub.RoundingPolicy, updated, report.GeneratedAt.Format(time.RFC3339), sub.RosterComplete, active, removed, reason})
		}
	}
	if kind == "raw" {
		headers = []string{"schemaVersion", "projectId", "projectCode", "subjectCode", "round", "revision", "snapshotId", "legacy", "historical", "panelistId", "panelistName", "itemId", "questionType", "value", "distributionJSON", "status", "included", "reviewed", "missing", "submissionVersion", "candidateVersion", "roundingPolicy", "updatedAt", "generatedAt", "rosterComplete", "activeAtSnapshot", "removedAtSnapshot", "exclusionReason"}
	}
	return encodeReportCSV(headers, rows, locale)
}
