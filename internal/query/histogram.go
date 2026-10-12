package query

import (
	"cmp"
	"slices"
)

// Interpolate returns the q-quantile of an explicit-bucket histogram. counts
// has one more element than bounds; bucket i covers (bounds[i-1], bounds[i]],
// the first starts at 0 (or at its bound if that is not positive), and the
// last is unbounded. A rank in the unbounded bucket returns the last finite
// bound, as Prometheus does. ok is false for an empty histogram and for one
// with no finite bounds, which has nothing to interpolate between.
func Interpolate(bounds, counts []float64, q float64) (float64, bool) {
	total := sum(counts)
	if total <= 0 || len(bounds) == 0 || len(counts) != len(bounds)+1 {
		return 0, false
	}
	rank := q * total
	var before float64
	for i, n := range counts {
		if n <= 0 || before+n < rank {
			before += n
			continue
		}
		if i == len(bounds) {
			return bounds[len(bounds)-1], true
		}
		upper := bounds[i]
		lower := 0.0
		if i > 0 {
			lower = bounds[i-1]
		} else if upper <= 0 {
			return upper, true
		}
		return lower + (upper-lower)*(rank-before)/n, true
	}
	return bounds[len(bounds)-1], true
}

// bucketGroup is the series of one histogram group that share bucket bounds,
// and their total count over the window.
type bucketGroup struct {
	bounds []float64
	series []uint64
	total  float64
}

// mergedBounds picks the bounds whose buckets are merged: those the most
// distinct series share, then the larger total count, then the bounds that
// sort first, so the choice is stable. It returns the chosen group and how
// many distinct series the others hold; those are reported, not merged.
func mergedBounds(groups []bucketGroup) (bucketGroup, int) {
	if len(groups) == 0 {
		return bucketGroup{}, 0
	}
	sorted := slices.Clone(groups)
	slices.SortStableFunc(sorted, func(a, b bucketGroup) int {
		if c := cmp.Compare(len(b.series), len(a.series)); c != 0 {
			return c
		}
		if c := cmp.Compare(b.total, a.total); c != 0 {
			return c
		}
		return slices.Compare(a.bounds, b.bounds)
	})
	others := map[uint64]bool{}
	for _, g := range sorted[1:] {
		for _, s := range g.series {
			others[s] = true
		}
	}
	return sorted[0], len(others)
}

func sum(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s
}
