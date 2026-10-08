package report_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/report"
)

func TestLatencySummary(t *testing.T) {
	var l report.Latencies
	for i := 1; i <= 100; i++ {
		l.Add(time.Duration(i) * time.Millisecond)
	}
	s := l.Summary()
	assert.Equal(t, 100, s.Count)
	assert.InDelta(t, 50, s.P50, 1e-9)
	assert.InDelta(t, 95, s.P95, 1e-9)
	assert.InDelta(t, 99, s.P99, 1e-9)
	assert.InDelta(t, 100, s.Max, 1e-9)

	var empty report.Latencies
	assert.Equal(t, report.Summary{}, empty.Summary())
}

func TestPeakRSS(t *testing.T) {
	rss, err := report.PeakRSS()
	require.NoError(t, err)
	assert.Greater(t, rss, int64(1<<20))
}
