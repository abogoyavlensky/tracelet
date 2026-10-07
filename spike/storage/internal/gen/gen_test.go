package gen_test

import (
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/gen"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/telemetry"
)

var start = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func TestSameSeedSameBatches(t *testing.T) {
	a := gen.NewGenerator(gen.Busy, 42, 0)
	b := gen.NewGenerator(gen.Busy, 42, 0)
	for i := range 20 {
		now := start.Add(time.Duration(i) * time.Second)
		assert.Equal(t, a.Next(now), b.Next(now))
	}
}

func TestRatesMatchProfile(t *testing.T) {
	g := gen.NewGenerator(gen.Busy, 1, 0)
	for i := range 10 {
		b := g.Next(start.Add(time.Duration(i) * time.Second))
		assert.InDelta(t, gen.Busy.LogsPerSec, len(b.Logs), 0.2*float64(gen.Busy.LogsPerSec))
		assert.InDelta(t, gen.Busy.SpansPerSec, len(b.Spans), 0.2*float64(gen.Busy.SpansPerSec))
		assert.InDelta(t, gen.Busy.PointsPerSec, len(b.Points), 0.2*float64(gen.Busy.PointsPerSec))
	}
}

func TestSpanShape(t *testing.T) {
	hex32 := regexp.MustCompile(`^[0-9a-f]{32}$`)
	hex16 := regexp.MustCompile(`^[0-9a-f]{16}$`)
	g := gen.NewGenerator(gen.Busy, 7, 0)

	for i := range 10 {
		b := g.Next(start.Add(time.Duration(i) * time.Second))
		byID := map[string]telemetry.Span{}
		for _, s := range b.Spans {
			byID[s.SpanID] = s
		}
		for _, s := range b.Spans {
			assert.Regexp(t, hex32, s.TraceID)
			assert.Regexp(t, hex16, s.SpanID)
			if s.ParentSpanID == "" {
				assert.Equal(t, int16(2), s.Kind, "roots are HTTP server spans")
				continue
			}
			parent, ok := byID[s.ParentSpanID]
			require.True(t, ok, "parent %s not in batch", s.ParentSpanID)
			assert.Equal(t, s.TraceID, parent.TraceID)
		}
	}
}

func TestLateLogs(t *testing.T) {
	g := gen.NewGenerator(gen.Busy, 3, 0)
	var total, late int
	for i := range 200 {
		now := start.Add(time.Duration(i) * time.Second)
		for _, l := range g.Next(now).Logs {
			total++
			if l.TS.Before(now.Add(-10 * time.Minute)) {
				late++
				assert.False(t, l.TS.Before(now.Add(-3*time.Hour)), "no more than three hours late")
			}
		}
	}
	share := float64(late) / float64(total)
	assert.InDelta(t, 0.01, share, 0.005, "late share %.4f", share)
}

func TestJobsCounterResets(t *testing.T) {
	g := gen.NewGenerator(gen.Small, 5, 0)

	type state struct {
		value   int64
		startTS time.Time
	}
	last := map[uint64]state{}
	resets := 0
	// 13 simulated hours, stepping one series interval at a time, crosses at
	// least two resets per instance.
	for i := range int(13 * time.Hour / time.Second) {
		now := start.Add(time.Duration(i) * time.Second)
		for _, p := range g.Next(now).Points {
			if p.Name != gen.MetricJobs {
				continue
			}
			require.NotNil(t, p.ValueInt)
			prev, seen := last[p.SeriesHash]
			if seen {
				if *p.ValueInt < prev.value {
					resets++
					assert.NotEqual(t, prev.startTS, p.StartTS, "a drop comes with a new start_ts")
				} else {
					assert.Equal(t, prev.startTS, p.StartTS, "no drop keeps start_ts")
				}
			}
			last[p.SeriesHash] = state{*p.ValueInt, p.StartTS}
		}
	}
	assert.Positive(t, resets)
}

func TestSeqConsecutive(t *testing.T) {
	g := gen.NewGenerator(gen.Small, 9, 1000)
	var seqs []int64
	for i := range 2 {
		for _, l := range g.Next(start.Add(time.Duration(i) * time.Second)).Logs {
			var attrs struct {
				Seq int64 `json:"seq"`
			}
			require.NoError(t, json.Unmarshal([]byte(l.Attributes), &attrs))
			seqs = append(seqs, attrs.Seq)
		}
	}
	require.Len(t, seqs, 2*gen.Small.LogsPerSec)
	for i, s := range seqs {
		assert.Equal(t, int64(1000+i), s)
	}
	assert.Equal(t, int64(1000+len(seqs)), g.Seq())
}

func TestSeriesHashesUnique(t *testing.T) {
	g := gen.NewGenerator(gen.Busy, 1, 0)
	seen := map[uint64]bool{}
	for i := range 10 {
		for _, p := range g.Next(start.Add(time.Duration(i) * time.Second)).Points {
			assert.False(t, seen[p.SeriesHash], "series emitted twice in one interval")
			seen[p.SeriesHash] = true
		}
	}
	assert.Len(t, seen, gen.Busy.Series)
}
