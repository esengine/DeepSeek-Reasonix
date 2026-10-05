package main

import "slices"

// Interval math shared by the trajectory summarizer: wall-clock spans,
// overlap-free unions, and subtraction for the disjoint wall decomposition.

// intervalSpan returns the batch's wall clock (max end − min start) and
// whether any two intervals actually overlapped (true parallelism).
func intervalSpan(intervals [][2]int64) (wall int64, overlapped bool) {
	sorted := append([][2]int64(nil), intervals...)
	slices.SortFunc(sorted, func(a, b [2]int64) int {
		switch {
		case a[0] != b[0]:
			return int(a[0] - b[0])
		default:
			return int(a[1] - b[1])
		}
	})
	minStart, maxEnd := sorted[0][0], sorted[0][1]
	for _, iv := range sorted[1:] {
		if iv[0] < maxEnd {
			overlapped = true
		}
		maxEnd = max(maxEnd, iv[1])
	}
	return maxEnd - minStart, overlapped
}

// intervalUnion is the merged length of all intervals, so concurrent tool
// executions count wall-clock once.
func intervalUnion(intervals [][2]int64) int64 {
	return ivsLen(mergeIntervals(intervals))
}

// mergeIntervals returns a sorted, overlap-free copy of intervals.
func mergeIntervals(intervals [][2]int64) [][2]int64 {
	if len(intervals) == 0 {
		return nil
	}
	sorted := append([][2]int64(nil), intervals...)
	slices.SortFunc(sorted, func(a, b [2]int64) int {
		switch {
		case a[0] != b[0]:
			return int(a[0] - b[0])
		default:
			return int(a[1] - b[1])
		}
	})
	out := [][2]int64{sorted[0]}
	for _, iv := range sorted[1:] {
		if last := &out[len(out)-1]; iv[0] <= last[1] {
			last[1] = max(last[1], iv[1])
			continue
		}
		out = append(out, iv)
	}
	return out
}

// clipIntervals returns base minus covered; both are merged internally.
func clipIntervals(base, covered [][2]int64) [][2]int64 {
	base = mergeIntervals(base)
	covered = mergeIntervals(covered)
	var out [][2]int64
	j := 0
	for _, iv := range base {
		lo := iv[0]
		for j < len(covered) && covered[j][1] <= lo {
			j++
		}
		for k := j; k < len(covered) && covered[k][0] < iv[1]; k++ {
			if covered[k][0] > lo {
				out = append(out, [2]int64{lo, covered[k][0]})
			}
			if lo = max(lo, covered[k][1]); lo >= iv[1] {
				break
			}
		}
		if lo < iv[1] {
			out = append(out, [2]int64{lo, iv[1]})
		}
	}
	return out
}

func ivsLen(intervals [][2]int64) int64 {
	var total int64
	for _, iv := range intervals {
		total += iv[1] - iv[0]
	}
	return total
}
