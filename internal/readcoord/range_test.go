package readcoord

import (
	"math/rand"
	"slices"
	"testing"

	"reasonix/internal/tool"
)

func ranges(pairs ...int) []tool.ReadRange {
	out := make([]tool.ReadRange, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, tool.ReadRange{Start: pairs[i], End: pairs[i+1]})
	}
	return out
}

func sameRanges(a, b []tool.ReadRange) bool { return slices.Equal(Normalize(a), Normalize(b)) }

func TestNormalizeMergesOverlapsAndAdjacency(t *testing.T) {
	cases := []struct {
		name string
		in   []tool.ReadRange
		want []tool.ReadRange
	}{
		{"empty dropped", ranges(5, 5, 7, 3), nil},
		{"adjacent merge", ranges(0, 5, 5, 9), ranges(0, 9)},
		{"overlap merge", ranges(0, 6, 4, 9), ranges(0, 9)},
		{"disjoint kept", ranges(0, 2, 5, 7), ranges(0, 2, 5, 7)},
		{"contained dropped", ranges(0, 10, 3, 5), ranges(0, 10)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Normalize(tc.in); !sameRanges(got, tc.want) {
				t.Fatalf("Normalize(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestSubtractReturnsUncoveredParts(t *testing.T) {
	cases := []struct {
		name      string
		want      []tool.ReadRange
		have      []tool.ReadRange
		wantLeft  []tool.ReadRange
		wantCover bool
	}{
		{"nothing covered", ranges(0, 10), nil, ranges(0, 10), false},
		{"prefix covered", ranges(0, 10), ranges(0, 4), ranges(4, 10), false},
		{"middle hole", ranges(0, 10), ranges(0, 4, 6, 10), ranges(4, 6), false},
		{"fully covered", ranges(2, 5), ranges(0, 10), nil, true},
		{"tail covered", ranges(0, 10), ranges(4, 10), ranges(0, 4), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Subtract(tc.want, tc.have)
			if !sameRanges(got, tc.wantLeft) {
				t.Fatalf("Subtract(%+v, %+v) = %+v, want %+v", tc.want, tc.have, got, tc.wantLeft)
			}
			if Covers(tc.have, tc.want) != tc.wantCover {
				t.Fatalf("Covers(%+v, %+v) = %v, want %v", tc.have, tc.want, !tc.wantCover, tc.wantCover)
			}
		})
	}
}

func randomRanges(rng *rand.Rand, n int) []tool.ReadRange {
	out := make([]tool.ReadRange, 0, n)
	for range n {
		start := rng.Intn(40)
		out = append(out, tool.ReadRange{Start: start, End: start + rng.Intn(12)})
	}
	return out
}

// TestCoverageAlgebraHoldsOverRandomSplits checks the property every paging
// decision depends on: the uncovered parts of a requirement, added back to what
// was already covered, cover the requirement exactly, and share no line with it.
func TestCoverageAlgebraHoldsOverRandomSplits(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for i := range 2000 {
		want := randomRanges(rng, 1+rng.Intn(5))
		have := randomRanges(rng, 1+rng.Intn(5))
		gaps := Subtract(want, have)
		if !Covers(append(append([]tool.ReadRange(nil), have...), gaps...), want) {
			t.Fatalf("case %d: covered %+v plus gaps %+v does not cover %+v", i, have, gaps, want)
		}
		for _, gap := range gaps {
			for _, h := range Normalize(have) {
				if start, end := max(gap.Start, h.Start), min(gap.End, h.End); start < end {
					t.Fatalf("case %d: gap %+v overlaps covered %+v", i, gap, h)
				}
			}
		}
	}
}

func TestNormalizeIsOrderIndependent(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for i := range 500 {
		in := randomRanges(rng, 1+rng.Intn(6))
		want := Normalize(in)
		shuffled := append([]tool.ReadRange(nil), in...)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		if got := Normalize(shuffled); !sameRanges(got, want) {
			t.Fatalf("case %d: Normalize depends on order: %+v vs %+v", i, got, want)
		}
		if again := Normalize(want); !sameRanges(again, want) {
			t.Fatalf("case %d: Normalize is not idempotent: %+v vs %+v", i, again, want)
		}
	}
}

func FuzzCoverageRecombination(f *testing.F) {
	f.Add([]byte{0, 10, 3, 6}, []byte{0, 4, 8, 10})
	f.Fuzz(func(t *testing.T, wantRaw, haveRaw []byte) {
		want := decodeRanges(wantRaw)
		have := decodeRanges(haveRaw)
		gaps := Subtract(want, have)
		if !Covers(append(append([]tool.ReadRange(nil), have...), gaps...), want) {
			t.Fatalf("covered %+v plus gaps %+v does not cover %+v", have, gaps, want)
		}
	})
}

func decodeRanges(raw []byte) []tool.ReadRange {
	out := make([]tool.ReadRange, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		start := int(raw[i])
		out = append(out, tool.ReadRange{Start: start, End: start + int(raw[i+1])})
	}
	return out
}
