package flush

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCopyStatement(t *testing.T) {
	hour := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	cutoff := hour.Add(90 * time.Minute)
	want := "COPY (SELECT * FROM logs WHERE ts >= TIMESTAMP '2026-10-01 03:00:00' AND ts < TIMESTAMP '2026-10-01 04:00:00'" +
		" AND ingest_ts <= TIMESTAMP '2026-10-01 04:30:00' ORDER BY ts) TO '/d/x.tmp' (FORMAT parquet, COMPRESSION zstd)"
	assert.Equal(t, want, copyStatement("logs", hour, cutoff, "/d/x.tmp"))
}
