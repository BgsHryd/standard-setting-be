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
	w := newRoundWorkbookSheet("Summary", 4)
	for i, title := range []string{"Putaran", "Cut-off test", "Stdev", "ICC"} {
		w.cell(i+1, 1, title)
	}
	last := 1
	for i, data := range bundle.rounds {
		row := i + 2
		last = row
		w.cell(1, row, fmt.Sprintf("Putaran %d", data.round.Number))
		if data.statistics != nil {
			w.cell(2, row, data.statistics.Cutoff)
			if sd := data.statistics.SampleStandardDeviation; sd != nil {
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
	w := newRoundWorkbookSheet("Respons", columns)
	w.check(w.file.SetColWidth(w.name, "A", "A", 32))
	w.check(w.file.SetColWidth(w.name, w.lastColumn, w.lastColumn, 78))
	w.cell(1, 1, "ID butir")
	headerHeight := 36
	for i, panelist := range data.panelists {
		label := fmt.Sprintf("Panelis %d", i+1)
		if panelist.name != "" {
			label += " - " + panelist.name
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
		for j, panelist := range data.panelists {
			if value, ok := data.values[item.ID][panelist.id]; ok {
				w.cell(j+2, row, value)
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
	return w.finish(1 + len(data.items))
}
