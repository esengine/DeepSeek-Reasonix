package main

import "testing"

// The oracle has to be able to fail, or "no hole" says nothing. These are the
// shapes a fold can produce and the shapes only a loss can.
func TestMarkerHoleDiscriminates(t *testing.T) {
	echo := func(ns ...int) []string {
		out := make([]string, 0, len(ns))
		for _, n := range ns {
			out = append(out, echoPrefix+pad(n))
		}
		return out
	}
	cases := []struct {
		name  string
		in    []string
		total int
		holed bool
	}{
		{"nothing folded", echo(1, 2, 3, 4, 5), 5, false},
		{"middle folded away", echo(4, 5), 5, false},
		{"pinned head kept beside the tail", echo(1, 4, 5), 5, false},
		{"a reply dropped inside the kept tail", echo(3, 5), 5, true},
		{"two gaps", echo(1, 3, 5), 5, true},
		{"newest turn missing", echo(3, 4), 5, true},
		{"head kept but not the first turn", echo(2, 4, 5), 5, true},
		{"empty view", nil, 5, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason := markerHole(tc.in, echoPrefix, tc.total)
			if holed := reason != ""; holed != tc.holed {
				t.Fatalf("markerHole(%v) = %q, want holed=%v", tc.in, reason, tc.holed)
			}
		})
	}
}

func TestMarkerShapeRendersRuns(t *testing.T) {
	in := []string{echoPrefix + pad(1), echoPrefix + pad(9), echoPrefix + pad(10), echoPrefix + pad(11)}
	if got := markerShape(in, echoPrefix); got != "1, 9-11" {
		t.Fatalf("markerShape = %q, want %q", got, "1, 9-11")
	}
}
