package service

import (
	"fmt"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
	"standard-setting/backend/internal/domain"
)

type roundWorkbookSheet struct {
	file           *excelize.File
	name           string
	lastColumn     string
	err            error
	headerStyle    int
	numberStyle    int
	textStyle      int
	priorityStyle  int
	agreementStyle int
}

func newRoundWorkbookSheet(name string, columns int) *roundWorkbookSheet {
	w := &roundWorkbookSheet{file: excelize.NewFile(), name: name}
	w.lastColumn, w.err = excelize.ColumnNumberToName(columns)
	w.check(w.file.SetSheetName("Sheet1", name))
	w.headerStyle = w.style(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"377D7A"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	format := "0.000"
	w.numberStyle = w.style(&excelize.Style{CustomNumFmt: &format, Alignment: &excelize.Alignment{Vertical: "top"}})
	w.textStyle = w.style(&excelize.Style{Font: &excelize.Font{Size: 11}, Alignment: &excelize.Alignment{Vertical: "top", WrapText: true}})
	w.priorityStyle = w.style(&excelize.Style{
		Font:      &excelize.Font{Color: "991B1B", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"FEE2E2"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
	})
	w.agreementStyle = w.style(&excelize.Style{
		Font:      &excelize.Font{Color: "166534", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"DCFCE7"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
	})
	w.check(w.file.SetColWidth(name, "A", w.lastColumn, 23))
	w.check(w.file.SetRowHeight(name, 1, 36))
	w.check(w.file.SetPanes(name, &excelize.Panes{Freeze: true, XSplit: 1, YSplit: 1, TopLeftCell: "B2", ActivePane: "bottomRight"}))
	return w
}

func (w *roundWorkbookSheet) style(style *excelize.Style) int {
	if w.err != nil {
		return 0
	}
	id, err := w.file.NewStyle(style)
	w.check(err)
	return id
}

func (w *roundWorkbookSheet) check(err error) {
	if w.err == nil {
		w.err = err
	}
}
func (w *roundWorkbookSheet) cell(column, row int, value any) {
	if w.err != nil {
		return
	}
	cell, err := excelize.CoordinatesToCellName(column, row)
	w.check(err)
	if text, ok := value.(string); ok {
		w.check(w.file.SetCellStr(w.name, cell, text)) // Text can never become a formula.
	} else {
		w.check(w.file.SetCellValue(w.name, cell, value))
	}
}
func (w *roundWorkbookSheet) finish(lastRow int) ([]byte, error) {
	defer w.file.Close()
	w.check(w.file.SetCellStyle(w.name, "A1", w.lastColumn+"1", w.headerStyle))
	if lastRow >= 2 {
		w.check(w.file.AutoFilter(w.name, fmt.Sprintf("A1:%s%d", w.lastColumn, lastRow), nil))
	}
	if w.err != nil {
		return nil, w.err
	}
	buf, err := w.file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renderRoundSummary(bundle workbookSubject) ([]byte, error) {
	fields := researchExportFields(nil, nil)
	unitColumn := 8 + len(fields)
	w := newRoundWorkbookSheet("Summary", unitColumn)
	w.cell(unitColumn, 1, "Satuan cut-off dan SD (B–C)")
	for i, field := range fields {
		w.cell(8+i, 1, field.title)
	}
	for i, title := range []string{"Putaran", "Cut-off test", "Stdev", "ICC"} {
		w.cell(i+1, 1, title)
	}
	w.cell(5, 1, "Metode proyek")
	w.cell(6, 1, "Pembulatan")
	w.cell(7, 1, "Ambang operasional (%)")
	last := 1
	for i, data := range bundle.rounds {
		row := i + 2
		last = row
		w.cell(5, row, data.method)
		w.cell(6, row, data.policy)
		if data.statistics != nil {
			w.cell(7, row, data.statistics.OperationalCutoff)
		}
		for j, field := range researchExportFields(&data.review, &data.uncertainty) {
			v := field.value
			if num, ok := v.(*float64); ok {
				if num == nil {
					continue
				}
				v = *num
			}
			if v != nil {
				w.cell(8+j, row, v)
			}
		}
		w.cell(1, row, fmt.Sprintf("Putaran %d", data.round.Number))
		if data.statistics != nil {
			sd := data.statistics.SampleStandardDeviation
			if data.policy == domain.NearestRawPoint {
				w.cell(unitColumn, row, "poin tes")
				if cut := data.review.Mix.CutoffPoints; cut != nil {
					w.cell(2, row, *cut)
				} else {
					w.cell(2, row, "Data poin belum tersedia")
				}
				sd = data.review.Mix.SDPoints
			} else {
				w.cell(unitColumn, row, "% (cut-off), pp (SD)")
				w.cell(2, row, data.statistics.Cutoff)
			}
			if sd != nil {
				w.cell(3, row, *sd)
			} else {
				w.cell(3, row, "Minimal 2 panelis")
			}
		} else {
			label := "Belum lengkap"
			if data.round.Status == "rolled_back" {
				label = "Putaran dibatalkan"
			} else if data.round.Status == "finalized" {
				label = "Snapshot tidak lengkap"
			}
			w.cell(2, row, label)
			w.cell(3, row, label)
		}
		if data.icc != nil {
			w.cell(4, row, *data.icc)
		} else if !data.complete {
			w.cell(4, row, "Belum lengkap")
		} else {
			w.cell(4, row, "Tidak dapat dihitung")
		}
		w.check(w.file.SetCellStyle(w.name, fmt.Sprintf("B%d", row), fmt.Sprintf("D%d", row), w.numberStyle))
	}
	if last == 1 {
		w.cell(1, 2, "Belum ada putaran")
		last = 2
	}
	return w.finish(last)
}

func renderRoundResponses(data workbookRound) ([]byte, error) {
	columns := len(data.panelists) + 2
	lastColumn := columns
	if data.policy == domain.NearestRawPoint {
		lastColumn += 2
	}
	w := newRoundWorkbookSheet("Respons", lastColumn)
	if lastColumn > columns {
		w.cell(columns+1, 1, "Rentang kontribusi (poin)")
		w.cell(columns+2, 1, "Tag diskusi")
	}
	w.check(w.file.SetColWidth(w.name, "A", "A", 32))
	guidanceColumn, err := excelize.ColumnNumberToName(columns)
	w.check(err)
	w.check(w.file.SetColWidth(w.name, guidanceColumn, guidanceColumn, 78))
	w.cell(1, 1, "ID butir")
	headerHeight := 36
	for i, panelist := range data.panelists {
		label := fmt.Sprintf("Panelis %d", i+1)
		if panelist.name != "" {
			label += " - " + panelist.name
		}
		if data.policy == domain.NearestRawPoint {
			label += " · kontribusi (poin)"
		}
		w.cell(i+2, 1, label)
		headerHeight = max(headerHeight, min(400, (1+utf8.RuneCountInString(label)/20)*16))
	}
	w.check(w.file.SetRowHeight(w.name, 1, float64(headerHeight)))
	w.cell(columns, 1, "Arahan")
	for i, item := range data.items {
		row := i + 2
		w.cell(1, row, item.ID)
		w.check(w.file.SetCellStyle(w.name, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row), w.textStyle))
		var minimum, maximum float64
		count := 0
		for j, panelist := range data.panelists {
			if value, ok := data.values[panelist.id][item.ID]; ok {
				if data.policy == domain.NearestRawPoint {
					if count == 0 || value < minimum {
						minimum = value
					}
					if count == 0 || value > maximum {
						maximum = value
					}
					count++
					value = value * float64(item.MaxScore) / 100
				}
				w.cell(j+2, row, value)
			}
		}
		if count >= 2 {
			spread := maximum - minimum
			w.cell(columns+1, row, spread*float64(item.MaxScore)/100)
			if itemDiscussionPriority(item, spread) {
				w.cell(columns+2, row, "*")
			}
		}
		message, style := "Menunggu putaran dikunci.", w.textStyle
		if data.round.Status == "rolled_back" {
			message = "Putaran dibatalkan."
		} else if data.round.Status == "finalized" {
			message = "Hasil putaran belum tersedia."
			for _, guide := range monitoringItemGuidance(data.round, []domain.Item{item}) {
				switch guide.State {
				case "priority":
					message = "Prioritaskan diskusi: minta panelis dengan nilai tertinggi dan terendah menjelaskan alasannya berdasarkan definisi JQC."
					style = w.priorityStyle
				case "agreement":
					message = "Panel cenderung sudah sepakat. Tidak perlu dibahas khusus, cukup konfirmasi (jika diperlukan)."
					style = w.agreementStyle
				}
			}
		}
		w.cell(columns, row, message)
		if style != 0 {
			cell := fmt.Sprintf("%s%d", w.lastColumn, row)
			w.check(w.file.SetCellStyle(w.name, cell, cell, style))
		}
		if len(data.panelists) > 0 {
			last, _ := excelize.ColumnNumberToName(columns - 1)
			w.check(w.file.SetCellStyle(w.name, fmt.Sprintf("B%d", row), fmt.Sprintf("%s%d", last, row), w.numberStyle))
		}
		w.check(w.file.SetRowHeight(w.name, row, float64(max(48, min(400, (1+utf8.RuneCountInString(item.ID)/30)*16)))))
	}
	lastRow := 1 + len(data.items)
	if data.policy == domain.NearestRawPoint && len(data.review.Rows) > 0 {
		titles := []string{"Average Modified (proporsi PG)", "Average Extended (poin per butir)", "Cut-off panelis (poin)"}
		for i, title := range titles {
			w.cell(1, lastRow+1+i, title)
		}
		for j, panelist := range data.review.Rows {
			for i, metric := range []*float64{panelist.ModifiedMean, panelist.ExtendedMean, panelist.RawPoints} {
				if metric != nil {
					w.cell(j+2, lastRow+1+i, *metric)
				}
			}
		}
		lastRow += len(titles)
	}
	return w.finish(lastRow)
}
