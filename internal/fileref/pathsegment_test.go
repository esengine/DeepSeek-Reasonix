package fileref

import "testing"

// TestPathSegmentContains covers the slash-separated path-segment matcher
// behind Search's fallback tier. The path is case-folded per segment; the
// query is expected to arrive already lowercased (Search lowercases it).
func TestPathSegmentContains(t *testing.T) {
	for _, tc := range []struct {
		rel   string
		query string
		want  bool
	}{
		{rel: "src/planind/index.tsx", query: "planind", want: true},
		{rel: "src/planind/index.tsx", query: "src", want: true},       // first segment
		{rel: "src/planind/index.tsx", query: "index", want: true},     // last segment (basename)
		{rel: "src/planind/index.tsx", query: "plan", want: true},      // substring within a segment
		{rel: "Src/PlanInd/Index.tsx", query: "planind", want: true},   // path is case-folded
		{rel: "src/planind/index.tsx", query: "zzz", want: false},      // no segment matches
		{rel: "src/planind/index.tsx", query: "SRC", want: false},      // query must be pre-lowercased
		{rel: "src/planind/index.tsx", query: "src/plan", want: false}, // never spans segments
		{rel: "src/x", query: "", want: true},                          // empty query matches any segment
		{rel: "", query: "", want: true},
	} {
		if got := pathSegmentContains(tc.rel, tc.query); got != tc.want {
			t.Errorf("pathSegmentContains(%q, %q) = %v, want %v", tc.rel, tc.query, got, tc.want)
		}
	}
}
