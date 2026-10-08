package flush

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCopyStatement(t *testing.T) {
	hour := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	cutoff := hour.Add(90 * time.Minute)
	tests := []struct {
		name    string
		flusher Flusher
		want    string
	}{
		{
			name: "defaults",
			want: "COPY (SELECT * FROM logs WHERE ts >= TIMESTAMP '2026-10-01 03:00:00' AND ts < TIMESTAMP '2026-10-01 04:00:00'" +
				" AND ingest_ts <= TIMESTAMP '2026-10-01 04:30:00' ORDER BY ts) TO '/d/x.tmp' (FORMAT parquet, COMPRESSION zstd)",
		},
		{
			name:    "row group size and unsorted",
			flusher: Flusher{RowGroupSize: 30000, Unsorted: true},
			want: "COPY (SELECT * FROM logs WHERE ts >= TIMESTAMP '2026-10-01 03:00:00' AND ts < TIMESTAMP '2026-10-01 04:00:00'" +
				" AND ingest_ts <= TIMESTAMP '2026-10-01 04:30:00') TO '/d/x.tmp' (FORMAT parquet, COMPRESSION zstd, ROW_GROUP_SIZE 30000)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.flusher.copyStatement("logs", hour, cutoff, "/d/x.tmp"))
		})
	}
}
