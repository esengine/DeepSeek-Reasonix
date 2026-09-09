// Package readcoord tracks what a read actually delivered and decides whether
// a read requirement is met. It is host-only: it renders nothing, mutates no
// file, and never changes provider bytes.
package readcoord

import (
	"slices"

	"reasonix/internal/tool"
)

// Normalize drops empty ranges and merges overlaps and adjacency into the
// smallest equivalent set.
func Normalize(ranges []tool.ReadRange) []tool.ReadRange {
	out := make([]tool.ReadRange, 0, len(ranges))
	for _, r := range ranges {
		if !r.Empty() {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.SortFunc(out, func(a, b tool.ReadRange) int {
		if a.Start != b.Start {
			return a.Start - b.Start
		}
		return a.End - b.End
	})
	merged := out[:1]
	for _, r := range out[1:] {
		last := &merged[len(merged)-1]
		if r.Start <= last.End {
			last.End = max(last.End, r.End)
			continue
		}
		merged = append(merged, r)
	}
	return merged
}

// Subtract returns the parts of want that have does not cover.
func Subtract(want, have []tool.ReadRange) []tool.ReadRange {
	have = Normalize(have)
	var out []tool.ReadRange
	for _, w := range Normalize(want) {
		cur := w
		for _, h := range have {
			if h.End <= cur.Start {
				continue
			}
			if h.Start >= cur.End {
				break
			}
			if h.Start > cur.Start {
				out = append(out, tool.ReadRange{Start: cur.Start, End: h.Start})
			}
			cur.Start = max(cur.Start, h.End)
			if cur.Empty() {
				break
			}
		}
		if !cur.Empty() {
			out = append(out, cur)
		}
	}
	return out
}

// Covers reports whether have covers every line of want.
func Covers(have, want []tool.ReadRange) bool {
	return len(Subtract(want, have)) == 0
}
