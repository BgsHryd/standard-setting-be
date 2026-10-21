package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/xuri/excelize/v2"
	"io"
	"math"
	"path"
	"reflect"
	"standard-setting/backend/internal/domain"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type impactStore interface {
	SaveImpactBatch(context.Context, domain.ImpactBatch) error
	ListImpactBatches(context.Context, string) ([]domain.ImpactBatch, error)
}
type ImpactPreview struct {
	Valid    bool                   `json:"valid"`
	Count    int                    `json:"count"`
	Errors   []WorkbookError        `json:"errors"`
	Binding  domain.ImpactBinding   `json:"binding"`
	Scores   []domain.ImpactScore   `json:"scores"`
	Subjects []domain.ImpactSubject `json:"subjects"`
}

var impactHeaders = []string{"peserta_id", "mata_pelajaran", "skor", "skala", "maksimum"}

func impactBindings(report ProjectReport) []domain.ImpactSnapshotBinding {
	result := []domain.ImpactSnapshotBinding{}
	for _, r := range report.Subjects {
		if r.Available && !r.Historical {
			result = append(result, domain.ImpactSnapshotBinding{SubjectCode: r.Subject.Code, SnapshotID: r.SnapshotID, RoundNumber: r.Round.Number, Revision: r.Round.Revision})
		}
	}
	return result
}
func (s *Service) ImpactTemplate(ctx context.Context, id string) ([]byte, error) {
	report, err := s.ProjectReport(ctx, id, 0)
	if err != nil {
		return nil, err
	}
	bindings := impactBindings(report)
	if len(bindings) == 0 {
		return nil, errors.New("belum ada snapshot hasil final untuk analisis dampak")
	}
	f := excelize.NewFile()
	defer f.Close()
	f.SetSheetName("Sheet1", "Data")
	f.NewSheet("_binding")
	for i, h := range impactHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellStr("Data", cell, h)
	}
	manifest, _ := json.Marshal(bindings)
	f.SetCellStr("_binding", "A1", id)
	f.SetCellInt("_binding", "B1", int64(report.Project.Version))
	f.SetCellStr("_binding", "C1", string(manifest))
	f.SetCellStr("_binding", "D1", "impact-1")
	style, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "FFFFFF", Size: 12}, Fill: excelize.Fill{Type: "pattern", Color: []string{"377D7A"}, Pattern: 1}})
	f.SetCellStyle("Data", "A1", "E1", style)
	textStyle, _ := f.NewStyle(&excelize.Style{NumFmt: 49})
	f.SetCellStyle("Data", "A2", "B5001", textStyle)
	f.SetColWidth("Data", "A", "E", 24)
	f.SetPanes("Data", &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2"})
	f.SetSheetVisible("_binding", false, true)
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// validateImpactArchive inspects XML directly, including formulas whose cached
// result is empty, relationship targets, macros and zip expansion limits.
func validateImpactArchive(data []byte) error {
	if len(data) == 0 || len(data) > workbookLimit {
		return errors.New("file .xlsx maksimal 5 MB")
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return errors.New("file harus workbook .xlsx")
	}
	if len(archive.File) > 1000 {
		return errors.New("terlalu banyak bagian workbook")
	}
	var total uint64
	seen := map[string]bool{}
	for _, file := range archive.File {
		name := strings.ToLower(file.Name)
		if seen[name] || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || path.Clean(name) != name || strings.HasPrefix(name, "../") {
			return errors.New("struktur workbook tidak valid")
		}
		seen[name] = true
		if file.UncompressedSize64 > 25*1024*1024-total {
			return errors.New("isi workbook maksimal 25 MB")
		}
		total += file.UncompressedSize64
		if strings.Contains(name, "vba") || strings.Contains(name, "externallinks/") || strings.Contains(name, "embeddings/") || strings.Contains(name, "activex/") {
			return errors.New("macro, objek tertanam dan tautan eksternal tidak diterima")
		}
		if !strings.HasSuffix(name, ".xml") && !strings.HasSuffix(name, ".rels") {
			continue
		}
		r, err := file.Open()
		if err != nil {
			return errors.New("bagian workbook tidak dapat dibaca")
		}
		decoder := xml.NewDecoder(io.LimitReader(r, 25*1024*1024+1))
		for {
			tok, e := decoder.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				r.Close()
				return errors.New("XML workbook tidak valid")
			}
			start, ok := tok.(xml.StartElement)
			if !ok {
				continue
			}
			if start.Name.Local == "f" {
				r.Close()
				return errors.New("formula tidak diperbolehkan pada workbook")
			}
			for _, a := range start.Attr {
				if strings.EqualFold(a.Name.Local, "TargetMode") && strings.EqualFold(a.Value, "External") || strings.Contains(strings.ToLower(a.Value), "macroenabled") {
					r.Close()
					return errors.New("macro dan tautan eksternal tidak diterima")
				}
			}
		}
		r.Close()
	}
	return nil
}
func (s *Service) PreviewImpact(ctx context.Context, id string, data []byte) (ImpactPreview, error) {
	report, err := s.ProjectReport(ctx, id, 0)
	if err != nil {
		return ImpactPreview{}, err
	}
	return parseImpact(report, data)
}
func parseImpact(report ProjectReport, data []byte) (ImpactPreview, error) {
	result := ImpactPreview{Errors: []WorkbookError{}, Scores: []domain.ImpactScore{}, Subjects: []domain.ImpactSubject{}}
	if err := validateImpactArchive(data); err != nil {
		return result, err
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: 25 * 1024 * 1024, UnzipXMLSizeLimit: 10 * 1024 * 1024})
	if err != nil {
		return result, errors.New("workbook tidak dapat dibaca")
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) != 2 || !contains(sheets, "Data") || !contains(sheets, "_binding") {
		return result, errors.New("sheet wajib tepat Data dan _binding dari template")
	}
	id, _ := f.GetCellValue("_binding", "A1")
	version, _ := f.GetCellValue("_binding", "B1")
	snapshots, _ := f.GetCellValue("_binding", "C1")
	kind, _ := f.GetCellValue("_binding", "D1")
	var bound []domain.ImpactSnapshotBinding
	if json.Unmarshal([]byte(snapshots), &bound) != nil || id != report.Project.ID || version != strconv.Itoa(report.Project.Version) || kind != "impact-1" || !reflect.DeepEqual(bound, impactBindings(report)) {
		return result, errors.New("template atau snapshot kedaluwarsa; unduh template terbaru")
	}
	sum := sha256.Sum256(data)
	result.Binding = domain.ImpactBinding{ProjectID: id, ProjectVersion: report.Project.Version, FileHash: hex.EncodeToString(sum[:]), Snapshots: bound}
	rows, err := f.GetRows("Data", excelize.Options{RawCellValue: true})
	if err != nil || len(rows) == 0 || !reflect.DeepEqual(rows[0], impactHeaders) {
		return result, errors.New("kolom wajib tepat: " + strings.Join(impactHeaders, ", "))
	}
	if len(rows) > workbookRows+1 {
		return result, errors.New("maksimal 5000 baris data")
	}
	problems := func(n int, col, value, message string) {
		result.Errors = append(result.Errors, WorkbookError{Row: n, Column: col, Value: value, Message: message})
	}
	bySubject := map[string]ReportSubject{}
	for _, sub := range report.Subjects {
		if sub.Available {
			bySubject[sub.Subject.Code] = sub
		}
	}
	seen := map[string]bool{}
	aggregates := map[string]*domain.ImpactSubject{}
	value := func(row []string, col int) string {
		if col < len(row) {
			return strings.TrimSpace(row[col])
		}
		return ""
	}
	for i, row := range rows[1:] {
		n := i + 2
		empty := true
		for _, v := range row {
			if strings.TrimSpace(v) != "" {
				empty = false
			}
		}
		if empty {
			continue
		}
		result.Count++
		before := len(result.Errors)
		if len(row) > len(impactHeaders) {
			problems(n, "header", "", "kolom tambahan tidak diperbolehkan")
		}
		uid, sub, scoreText, scale, maxText := value(row, 0), value(row, 1), value(row, 2), value(row, 3), value(row, 4)
		if uid == "" || utf8.RuneCountInString(uid) > 128 || strings.ContainsAny(uid, "\r\n\t\x00") || strings.ContainsRune("=+-@", firstRune(uid)) {
			problems(n, "peserta_id", uid, "ID pseudonim wajib, maksimal 128 karakter, tanpa formula atau karakter kontrol")
		}
		for _, r := range uid {
			if unicode.IsControl(r) {
				problems(n, "peserta_id", uid, "karakter kontrol tidak diterima")
				break
			}
		}
		source, ok := bySubject[sub]
		if !ok {
			problems(n, "mata_pelajaran", sub, "gunakan kode mapel dengan snapshot hasil final pada proyek ini")
		}
		key := uid + "\x00" + sub
		if seen[key] {
			problems(n, "peserta_id", uid, "duplikat peserta-mapel dalam file")
		}
		seen[key] = true
		score, e := strconv.ParseFloat(scoreText, 64)
		if e != nil || math.IsNaN(score) || math.IsInf(score, 0) {
			problems(n, "skor", scoreText, "skor harus angka terbatas, gunakan titik desimal")
		}
		maximum, e := strconv.ParseFloat(maxText, 64)
		if e != nil || math.IsNaN(maximum) || math.IsInf(maximum, 0) || maximum <= 0 {
			problems(n, "maksimum", maxText, "maksimum wajib angka positif")
		}
		if scale != "persen" && scale != "mentah" {
			problems(n, "skala", scale, "skala harus persen atau mentah")
		}
		if scale == "persen" && maximum != 100 {
			problems(n, "maksimum", maxText, "skala persen wajib maksimum 100")
		}
		if score < 0 || score > maximum {
			problems(n, "skor", scoreText, "skor di luar skala 0–maksimum")
		}
		if len(result.Errors) != before {
			continue
		}
		percent := score / maximum * 100
		if scale == "persen" {
			percent = score
		}
		// Normalize only representational noise at the recorded decision boundary.
		if math.Abs(percent-source.Statistics.OperationalCutoff) <= 1e-10 {
			percent = source.Statistics.OperationalCutoff
		}
		passed := percent >= source.Statistics.OperationalCutoff
		result.Scores = append(result.Scores, domain.ImpactScore{ParticipantID: uid, SubjectCode: sub, Score: score, Scale: scale, Maximum: maximum, Percentage: percent, Passed: passed})
		agg := aggregates[sub]
		if agg == nil {
			agg = &domain.ImpactSubject{SubjectCode: sub, CutoffSnapshotID: source.SnapshotID, RoundingPolicy: source.RoundingPolicy, Cutoff: source.Statistics.Cutoff, OperationalCutoff: source.Statistics.OperationalCutoff, Distribution: make([]int, 10)}
			aggregates[sub] = agg
		}
		agg.Count++
		if passed {
			agg.Passed++
		} else {
			agg.Failed++
		}
		bin := min(9, int(percent/10))
		agg.Distribution[bin]++
	}
	for _, sub := range report.Subjects {
		if agg := aggregates[sub.Subject.Code]; agg != nil {
			agg.Percentage = float64(agg.Passed) / float64(agg.Count) * 100
			result.Subjects = append(result.Subjects, *agg)
		}
	}
	if result.Count == 0 {
		problems(0, "baris", "", "file tidak memiliki data")
	}
	result.Valid = len(result.Errors) == 0
	return result, nil
}
func firstRune(v string) rune {
	for _, r := range v {
		return r
	}
	return 0
}
func (s *Service) ApplyImpact(ctx context.Context, id, actor string, data []byte, binding domain.ImpactBinding) (domain.ImpactBatch, error) {
	var batch domain.ImpactBatch
	st, ok := s.store.(impactStore)
	if !ok {
		return batch, errors.New("penyimpanan dampak tidak tersedia")
	}
	err := s.store.WithTransaction(ctx, func(tx context.Context) error {
		report, err := s.ProjectReport(tx, id, 0)
		if err != nil {
			return err
		}
		preview, err := parseImpact(report, data)
		if err != nil {
			return err
		}
		if !preview.Valid {
			return errors.New("impor ditolak seluruhnya: perbaiki semua baris pada pratinjau")
		}
		if !reflect.DeepEqual(binding, preview.Binding) {
			return errors.New("binding pratinjau, file, proyek atau snapshot berubah; lakukan pratinjau ulang")
		}
		if err = s.store.TouchProject(tx, id); err != nil {
			return err
		}
		batch = domain.ImpactBatch{ID: newID("impact"), ProjectID: id, Binding: binding, Subjects: preview.Subjects, Scores: preview.Scores, CreatedBy: actor, CreatedAt: time.Now().UTC()}
		return st.SaveImpactBatch(tx, batch)
	})
	return batch, err
}
func (s *Service) ImpactResults(ctx context.Context, id string) ([]domain.ImpactBatch, error) {
	report, err := s.ProjectReport(ctx, id, 0)
	if err != nil {
		return nil, err
	}
	st, ok := s.store.(impactStore)
	if !ok {
		return nil, errors.New("penyimpanan dampak tidak tersedia")
	}
	batches, err := st.ListImpactBatches(ctx, id)
	if err != nil {
		return nil, err
	}
	latest := map[string]string{}
	for _, sub := range report.Subjects {
		latest[sub.Subject.Code] = sub.SnapshotID
	}
	for i := range batches {
		for j := range batches[i].Subjects {
			r := &batches[i].Subjects[j]
			r.Stale = latest[r.SubjectCode] != r.CutoffSnapshotID
		}
	}
	return batches, nil
}
func (s *Service) EvaluationCSV(ctx context.Context, id, locale string) ([]byte, error) {
	if locale != "id" && locale != "standard" {
		return nil, fmt.Errorf("format harus id atau standard")
	}
	summary, err := s.EvaluationSummary(ctx, id)
	if err != nil {
		return nil, err
	}
	rows := [][]any{}
	for _, q := range summary.Questions {
		rows = append(rows, []any{id, 1, summary.Invited, summary.Responded, summary.Missing, false, q.Key, q.Median, q.Distribution[0], q.Distribution[1], q.Distribution[2], q.Distribution[3], q.Distribution[4]})
	}
	if summary.SmallPanel {
		rows = append(rows, []any{id, 1, summary.Invited, summary.Responded, summary.Missing, true, "", "", "", "", "", "", ""})
	}
	return encodeReportCSV([]string{"projectId", "instrumentVersion", "invited", "responded", "missing", "smallPanel", "question", "median", "score1", "score2", "score3", "score4", "score5"}, rows, locale)
}
