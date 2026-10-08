package report_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/report"
)

func TestSummarize(t *testing.T) {
	sampled := report.Report{
		Mode:         "accelerated",
		PeakRSSBytes: 932_400_000,
		Flushes:      report.Summary{P95: 1820.4},
		Commits:      report.Summary{P99: 46.6},
		MemoryPeak: &report.MemorySample{
			RSS:        910_000_000,
			GoResident: 120_300_000,
			DuckDB:     map[string]int64{"BASE_TABLE": 200_000_000, "ORDER_BY": 50_200_000},
			Residual:   539_500_000,
			InFlush:    true,
		},
		MemoryPeakVsHWM: 1.0246,
	}
	old := report.Report{
		Mode:         "real-time",
		PeakRSSBytes: 852_115_456,
		Flushes:      report.Summary{P95: 900},
		Commits:      report.Summary{P99: 46},
		Queries: map[string]report.Summary{
			"jobs_rate":   {P99: 2049.6},
			"trace_by_id": {P99: 120},
		},
	}

	got := report.Summarize([]report.NamedReport{{Name: "E0", Report: sampled}, {Name: "full-rt", Report: old}})

	lines := strings.Split(strings.TrimSpace(got), "\n")
	assert.Equal(t, []string{
		"| Run | Peak RSS (MB) | RSS at peak sample (MB) | Go resident at peak (MB) | DuckDB at peak (MB) | Residual at peak (MB) | Peak in flush | HWM / peak sample | Flush p95 (ms) | Commit p99 (ms) | Worst query p99 (ms) |",
		"| --- | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |",
		"| E0 | 932 | 910 | 120 | 250 | 540 | yes | 1.02 | 1820 | 47 |  |",
		"| full-rt | 852 |  |  |  |  |  |  | 900 | 46 | 2050 |",
	}, lines)
}
