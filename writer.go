package main

import (
	"encoding/csv"
	"fmt"
	"os"

	"github.com/xuri/excelize/v2"
)

// batchCols are the sales-facing columns written to a delivered batch.
var batchCols = []string{
	"Company", "Director", "Phone", "MobileFound", "Email", "Address",
	"BusinessType", "WebStatus", "Tags", "Angle", "Source",
}

func leadField(l *Lead, col string) string {
	for i, h := range poolHeaders {
		if h == col {
			return l.row()[i]
		}
	}
	return ""
}

// writeBatch writes a delivered batch as both .csv and .xlsx (base has no ext).
func writeBatch(base string, leads []*Lead) error {
	if err := writeBatchCSV(base+".csv", leads); err != nil {
		return err
	}
	return writeBatchXLSX(base+".xlsx", leads)
}

func writeBatchCSV(path string, leads []*Lead) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write(batchCols); err != nil {
		return err
	}
	for _, l := range leads {
		rec := make([]string, len(batchCols))
		for i, c := range batchCols {
			rec[i] = leadField(l, c)
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	return w.Error()
}

func writeBatchXLSX(path string, leads []*Lead) error {
	x := excelize.NewFile()
	defer x.Close()
	const sheet = "Batch"
	x.SetSheetName(x.GetSheetName(0), sheet)

	hdr := make([]any, len(batchCols))
	for i, c := range batchCols {
		hdr[i] = c
	}
	x.SetSheetRow(sheet, "A1", &hdr)
	for i, l := range leads {
		rec := make([]any, len(batchCols))
		for j, c := range batchCols {
			rec[j] = leadField(l, c)
		}
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		x.SetSheetRow(sheet, cell, &rec)
	}

	style, _ := x.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#DCE6F1"}},
	})
	lastCol, _ := excelize.ColumnNumberToName(len(batchCols))
	x.SetCellStyle(sheet, "A1", fmt.Sprintf("%s1", lastCol), style)
	x.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	x.AutoFilter(sheet, fmt.Sprintf("A1:%s%d", lastCol, len(leads)+1), []excelize.AutoFilterOptions{})
	for col, wd := range map[string]float64{"A": 32, "B": 24, "C": 22, "D": 16, "E": 30, "F": 38, "J": 60} {
		x.SetColWidth(sheet, col, col, wd)
	}
	return x.SaveAs(path)
}
