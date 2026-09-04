package agent

import "testing"

// The verdict here is the translation itself — canonical coverage and the body
// remainder — and nothing downstream of it. Token counts, projection reuse and
// marker continuity answer later questions; letting one of them decide this
// table would make it pass for reasons that are not the mapping.
func TestMapFoldBoundary(t *testing.T) {
	cases := []struct {
		name         string
		start        int
		bodyLen      int
		priorCovered int
		projected    bool
		want         foldBoundary
	}{{
		name:  "no projection: the view is the transcript",
		start: 10, want: foldBoundary{Covered: 10},
	}, {
		name:  "no projection ignores a stale body and prior coverage",
		start: 4, bodyLen: 15, priorCovered: 19,
		want: foldBoundary{Covered: 4},
	}, {
		// The case that decides how much the live-tail split has to do: view
		// index 9 names no canonical message, so coverage stays where the
		// previous fold left it and body[9:15] must survive the new digest.
		name:  "boundary inside the frozen body",
		start: 9, bodyLen: 15, priorCovered: 19, projected: true,
		want: foldBoundary{Covered: 19, BodySuffixFrom: 9},
	}, {
		name:  "boundary exactly at the end of the body",
		start: 15, bodyLen: 15, priorCovered: 19, projected: true,
		want: foldBoundary{Covered: 19, BodySuffixFrom: 15},
	}, {
		name:  "boundary in the live tail",
		start: 18, bodyLen: 15, priorCovered: 19, projected: true,
		want: foldBoundary{Covered: 22, BodySuffixFrom: 15},
	}, {
		name:  "boundary at zero keeps the whole body",
		start: 0, bodyLen: 15, priorCovered: 19, projected: true,
		want: foldBoundary{Covered: 19},
	}, {
		name:  "projection with an empty body is pure tail arithmetic",
		start: 3, bodyLen: 0, priorCovered: 19, projected: true,
		want: foldBoundary{Covered: 22},
	}, {
		name:  "a negative boundary is the start of the view",
		start: -1, bodyLen: 15, priorCovered: 19, projected: true,
		want: foldBoundary{Covered: 19},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapFoldBoundary(tc.start, tc.bodyLen, tc.priorCovered, tc.projected)
			if got != tc.want {
				t.Fatalf("mapFoldBoundary(%d, %d, %d, %v) = %+v, want %+v",
					tc.start, tc.bodyLen, tc.priorCovered, tc.projected, got, tc.want)
			}
		})
	}
}

// Coverage may not retreat past what an earlier fold already claimed: the
// digest for that range is what the projection body carries in its place.
func TestMapFoldBoundaryNeverUncoversFoldedHistory(t *testing.T) {
	const bodyLen, priorCovered = 15, 19
	for start := range bodyLen + 8 {
		got := mapFoldBoundary(start, bodyLen, priorCovered, true)
		if got.Covered < priorCovered {
			t.Fatalf("start %d: covered %d fell below prior coverage %d", start, got.Covered, priorCovered)
		}
		if got.BodySuffixFrom < 0 || got.BodySuffixFrom > bodyLen {
			t.Fatalf("start %d: body suffix %d outside the body", start, got.BodySuffixFrom)
		}
		// Past the body the two coordinate systems must agree offset for offset.
		if start >= bodyLen && got.Covered != priorCovered+(start-bodyLen) {
			t.Fatalf("start %d: covered %d does not track the live tail", start, got.Covered)
		}
	}
}
