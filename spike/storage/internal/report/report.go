// Package report holds the JSON report every spike run prints.
package report

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Latencies records durations. It is not safe for concurrent use.
type Latencies struct {
	ds []time.Duration
}

// Add records one duration.
func (l *Latencies) Add(d time.Duration) { l.ds = append(l.ds, d) }

// Merge adds every duration of other.
func (l *Latencies) Merge(other *Latencies) { l.ds = append(l.ds, other.ds...) }

// Summary is a latency distribution in milliseconds.
type Summary struct {
	Count int     `json:"count"`
	P50   float64 `json:"p50_ms"`
	P95   float64 `json:"p95_ms"`
	P99   float64 `json:"p99_ms"`
	Max   float64 `json:"max_ms"`
	Total float64 `json:"total_ms"`
}

// Summary returns nearest-rank percentiles of the recorded durations.
func (l *Latencies) Summary() Summary {
	if len(l.ds) == 0 {
		return Summary{}
	}
	ds := slices.Clone(l.ds)
	slices.Sort(ds)
	rank := func(p float64) float64 {
		i := int(p*float64(len(ds))+0.999999) - 1
		return ms(ds[max(0, min(i, len(ds)-1))])
	}
	var total time.Duration
	for _, d := range ds {
		total += d
	}
	return Summary{
		Count: len(ds),
		P50:   rank(0.50),
		P95:   rank(0.95),
		P99:   rank(0.99),
		Max:   ms(ds[len(ds)-1]),
		Total: ms(total),
	}
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// Sample is one point of the disk-usage time series, taken every simulated
// hour.
type Sample struct {
	SimTime       time.Time        `json:"sim_time"`
	ManifestBytes int64            `json:"manifest_bytes"`
	DirBytes      int64            `json:"dir_bytes"` // filesystem usage of the whole data dir
	Entries       map[string]int64 `json:"entries"`   // filesystem usage by top-level entry
	Files         int              `json:"files"`
}

// Report is the result of one CLI run.
type Report struct {
	Profile         string             `json:"profile"`
	Mode            string             `json:"mode"`
	SimulatedStart  time.Time          `json:"simulated_start"`
	SimulatedEnd    time.Time          `json:"simulated_end"`
	SimulatedHours  float64            `json:"simulated_hours"`
	WallSeconds     float64            `json:"wall_seconds"`
	Rows            map[string]int64   `json:"rows"`
	Commits         Summary            `json:"commit_latency"`
	CatchUpSeconds  int                `json:"catch_up_seconds,omitempty"` // real-time seconds generated late after dropped ticks
	Flushes         Summary            `json:"flush_duration"`
	Deletes         Summary            `json:"flush_delete_duration"`
	RetentionRuns   Summary            `json:"retention_duration"`
	HoursRetained   int                `json:"retention_hours_deleted"`
	FilesPerHour    map[int]int        `json:"files_per_hour"` // files in one (signal, hour) -> how many such hours
	Files           int                `json:"files"`
	ParquetBytes    map[string]int64   `json:"parquet_bytes"`
	DirBytes        map[string]int64   `json:"dir_bytes"`
	MaxDirBytes     int64              `json:"max_dir_bytes"`
	PeakRSSBytes    int64              `json:"peak_rss_bytes"`
	Queries         map[string]Summary `json:"queries,omitempty"`
	QueryErrors     int                `json:"query_errors,omitempty"`
	QueryLastError  string             `json:"query_last_error,omitempty"`
	Samples         []Sample           `json:"samples,omitempty"`
	StartupRepairs  any                `json:"startup_reconcile,omitempty"`
	StartupRetained any                `json:"startup_retention,omitempty"`
}

// WriteJSON writes v as indented JSON.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
}

// WriteJSON writes the report as indented JSON.
func (r Report) WriteJSON(w io.Writer) error { return WriteJSON(w, r) }

// PeakRSS returns the process's peak resident set size (VmHWM) in bytes.
func PeakRSS() (int64, error) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0, fmt.Errorf("open status: %w", err)
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		rest, ok := strings.CutPrefix(s.Text(), "VmHWM:")
		if !ok {
			continue
		}
		kb, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse VmHWM %q: %w", rest, err)
		}
		return kb * 1024, nil
	}
	if err := s.Err(); err != nil {
		return 0, fmt.Errorf("read status: %w", err)
	}
	return 0, fmt.Errorf("VmHWM not found")
}
