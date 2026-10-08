package report

import (
	"fmt"
	"math"
	"strings"
)

// NamedReport is a report and the name of the run that produced it.
type NamedReport struct {
	Name   string
	Report Report
}

// summaryHeader names the columns; the results note uses the same names.
var summaryHeader = []string{
	"Run", "Peak RSS (MB)", "RSS at peak sample (MB)", "Go resident at peak (MB)", "DuckDB at peak (MB)",
	"Residual at peak (MB)", "Peak in flush", "HWM / peak sample", "Flush p95 (ms)", "Commit p99 (ms)",
	"Worst query p99 (ms)",
}

// Summarize renders one markdown table row per report, in the given order.
// Memory is in decimal MB, latencies in ms, both rounded to whole numbers.
// Columns a report has no data for, such as the peak sample of a report
// that predates memory sampling, stay blank.
func Summarize(named []NamedReport) string {
	var b strings.Builder
	writeRow(&b, summaryHeader)
	writeRow(&b, []string{"---", "---:", "---:", "---:", "---:", "---:", "---", "---:", "---:", "---:", "---:"})
	for _, n := range named {
		writeRow(&b, summaryRow(n))
	}
	return b.String()
}

func summaryRow(n NamedReport) []string {
	r := n.Report
	row := []string{n.Name, mb(r.PeakRSSBytes)}
	if p := r.MemoryPeak; p == nil {
		row = append(row, "", "", "", "", "", "")
	} else {
		var duck int64
		for _, v := range p.DuckDB {
			duck += v
		}
		inFlush := "no"
		if p.InFlush {
			inFlush = "yes"
		}
		row = append(row, mb(p.RSS), mb(p.GoResident), mb(duck), mb(p.Residual),
			inFlush, fmt.Sprintf("%.2f", r.MemoryPeakVsHWM))
	}
	worstQuery := ""
	if len(r.Queries) > 0 {
		var worst float64
		for _, q := range r.Queries {
			worst = max(worst, q.P99)
		}
		worstQuery = whole(worst)
	}
	return append(row, whole(r.Flushes.P95), whole(r.Commits.P99), worstQuery)
}

func writeRow(b *strings.Builder, cells []string) {
	b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
}

func mb(bytes int64) string { return whole(float64(bytes) / 1e6) }

func whole(v float64) string { return fmt.Sprintf("%d", int64(math.Round(v))) }
