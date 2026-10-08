package report_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/report"
)

const procStatus = `Name:	spike
Umask:	0022
State:	R (running)
VmPeak:	 2516708 kB
VmSize:	 2450132 kB
VmHWM:	  973812 kB
VmRSS:	  651204 kB
RssAnon:	  610000 kB
Threads:	12
`

func TestParseProcStatus(t *testing.T) {
	rss, hwm, err := report.ParseProcStatus(strings.NewReader(procStatus))
	require.NoError(t, err)
	assert.Equal(t, int64(651204*1024), rss)
	assert.Equal(t, int64(973812*1024), hwm)
}

func TestParseProcStatusMissingField(t *testing.T) {
	_, _, err := report.ParseProcStatus(strings.NewReader("Name:\tspike\nVmRSS:\t 10 kB\n"))
	assert.ErrorContains(t, err, "VmHWM")
}

func TestReadProcMemory(t *testing.T) {
	rss, hwm, err := report.ReadProcMemory()
	require.NoError(t, err)
	assert.Greater(t, rss, int64(1<<20))
	assert.GreaterOrEqual(t, hwm, rss)
}

func TestGoMemory(t *testing.T) {
	m := report.GoMemory()
	assert.Positive(t, m.Resident)
	assert.LessOrEqual(t, m.Resident, m.Sys)
	assert.Positive(t, m.HeapInuse)
}
