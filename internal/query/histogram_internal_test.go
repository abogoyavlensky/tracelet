package query

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var testBounds = []float64{10, 50, 100}

func TestInterpolateQuantiles(t *testing.T) {
	merged := []float64{2, 10, 6, 2} // total 20
	for _, tc := range []struct {
		q, want float64
	}{
		{0.50, 42},  // rank 10 in (10, 50]: 10 + 40 * (10 - 2) / 10
		{0.90, 100}, // rank 18 in (50, 100]: 50 + 50 * (18 - 12) / 6
		{0.99, 100}, // rank 19.8 in (100, +Inf): the last finite bound
	} {
		got, ok := Interpolate(testBounds, merged, tc.q)
		assert.True(t, ok)
		assert.InDelta(t, tc.want, got, 1e-9, "q=%v", tc.q)
	}
}

func TestInterpolateEdges(t *testing.T) {
	_, ok := Interpolate(testBounds, []float64{0, 0, 0, 0}, 0.5)
	assert.False(t, ok)

	_, ok = Interpolate(nil, []float64{5}, 0.5)
	assert.False(t, ok, "no finite bounds")

	v, ok := Interpolate(testBounds, []float64{4, 0, 0, 0}, 0.5)
	assert.True(t, ok)
	assert.InDelta(t, 5, v, 1e-9, "first bucket interpolates from zero")
}

func TestMergedBounds(t *testing.T) {
	main, other := mergedBounds([]bucketGroup{
		{bounds: []float64{5, 10}, series: []uint64{3}, total: 100},
		{bounds: testBounds, series: []uint64{1, 2}, total: 20},
	})
	assert.Equal(t, testBounds, main.bounds, "most series wins")
	assert.Equal(t, 1, other)

	main, _ = mergedBounds([]bucketGroup{
		{bounds: []float64{1}, series: []uint64{1}, total: 5},
		{bounds: []float64{2}, series: []uint64{2}, total: 9},
	})
	assert.Equal(t, []float64{2}, main.bounds, "tie broken by total")

	main, other = mergedBounds([]bucketGroup{
		{bounds: []float64{2}, series: []uint64{2}, total: 5},
		{bounds: []float64{1}, series: []uint64{1}, total: 5},
	})
	assert.Equal(t, []float64{1}, main.bounds, "tie broken by bounds order")
	assert.Equal(t, 1, other)

	main, other = mergedBounds(nil)
	assert.Nil(t, main.bounds)
	assert.Zero(t, other)
}
