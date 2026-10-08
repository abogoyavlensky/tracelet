package report

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// MemorySample is one point of the memory time series.
type MemorySample struct {
	SimTime    time.Time        `json:"sim_time"`
	Wall       time.Time        `json:"wall"`
	RSS        int64            `json:"rss_bytes"`         // VmRSS
	HWM        int64            `json:"hwm_bytes"`         // VmHWM so far
	GoResident int64            `json:"go_resident_bytes"` // MemStats.Sys - MemStats.HeapReleased: an estimate of Go's resident share
	GoHeapUsed int64            `json:"go_heap_bytes"`     // MemStats.HeapInuse
	DuckDB     map[string]int64 `json:"duckdb_bytes"`      // duckdb_memory() memory_usage_bytes by tag, non-zero tags only
	DuckDBTemp int64            `json:"duckdb_temp_bytes"` // sum of temporary_storage_bytes
	Residual   int64            `json:"residual_bytes"`    // RSS - GoResident - sum(DuckDB); approximate
	InFlush    bool             `json:"in_flush"`          // a flush was running when the sample was taken
}

// GoMem is the Go runtime's view of its own memory, in bytes.
type GoMem struct {
	// Resident estimates Go's resident share: what it reserved from the OS
	// minus what it gave back.
	Resident  int64
	HeapInuse int64
	Sys       int64
}

// GoMemory reads the Go runtime's memory statistics. It stops the world
// briefly, which is fine once a second.
func GoMemory() GoMem {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return GoMem{
		Resident:  int64(ms.Sys - ms.HeapReleased),
		HeapInuse: int64(ms.HeapInuse),
		Sys:       int64(ms.Sys),
	}
}

// ReadProcMemory returns the process's current (VmRSS) and peak (VmHWM)
// resident set size in bytes.
func ReadProcMemory() (rss, hwm int64, err error) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0, 0, fmt.Errorf("open status: %w", err)
	}
	defer f.Close()
	return ParseProcStatus(f)
}

// PeakRSS returns the process's peak resident set size (VmHWM) in bytes.
func PeakRSS() (int64, error) {
	_, hwm, err := ReadProcMemory()
	return hwm, err
}

// ParseProcStatus reads VmRSS and VmHWM, in bytes, from the content of
// /proc/<pid>/status, which reports them in kB.
func ParseProcStatus(r io.Reader) (rss, hwm int64, err error) {
	fields := map[string]*int64{"VmRSS": &rss, "VmHWM": &hwm}
	seen := map[string]bool{}
	s := bufio.NewScanner(r)
	for s.Scan() {
		key, rest, ok := strings.Cut(s.Text(), ":")
		dst, want := fields[key]
		if !ok || !want {
			continue
		}
		kb, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse %s %q: %w", key, rest, err)
		}
		*dst = kb * 1024
		seen[key] = true
	}
	if err := s.Err(); err != nil {
		return 0, 0, fmt.Errorf("read status: %w", err)
	}
	for _, key := range []string{"VmRSS", "VmHWM"} {
		if !seen[key] {
			return 0, 0, fmt.Errorf("%s not found", key)
		}
	}
	return rss, hwm, nil
}
