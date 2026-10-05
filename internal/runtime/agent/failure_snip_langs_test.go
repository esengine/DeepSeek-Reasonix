package agent

import (
	"fmt"
	"strings"
	"testing"
)

func padded(prefix string, n int) []string {
	out := make([]string, n)
	for i := range n {
		out[i] = fmt.Sprintf("%s%03d", prefix, i)
	}
	return out
}

// The failure markers are a heuristic, so the toolchains a coding agent
// actually runs are the coverage that matters: each case buries its detail in
// the middle of otherwise unremarkable output.
func TestSnipFailureAcrossToolchains(t *testing.T) {
	for _, tc := range []struct {
		name   string
		middle []string
		want   []string
	}{
		{
			name: "pytest",
			middle: []string{
				"=================================== FAILURES ===================================",
				"_____________________________ test_quoted_values ______________________________",
				">       assert config.dumps(cfg) == 'beta-7d21'",
				"E       AssertionError: assert 'gamma-4a88' == 'beta-7d21'",
				"tests/test_format.py:412: AssertionError",
			},
			want: []string{"beta-7d21", "gamma-4a88", "test_format.py:412"},
		},
		{
			name: "cargo",
			middle: []string{
				"error[E0308]: mismatched types",
				"  --> src/config.rs:412:9",
				"412 |         beta_7d21",
				"    |         ^^^^^^^^^ expected `String`, found `&str`",
			},
			want: []string{"E0308", "config.rs:412", "expected"},
		},
		{
			name: "tsc",
			middle: []string{
				"src/config.ts(412,9): error TS2322: Type 'string' is not assignable to type 'number'.",
			},
			want: []string{"TS2322", "config.ts(412,9)"},
		},
		{
			name: "jest",
			middle: []string{
				"  ● Config › round-trips quoted values",
				"    expect(received).toBe(expected) // Object.is equality",
				"    Expected: \"beta-7d21\"",
				"    Received: \"gamma-4a88\"",
				"      at Object.<anonymous> (src/config.test.ts:412:24)",
			},
			want: []string{"beta-7d21", "gamma-4a88", "config.test.ts:412"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := padded("collecting item ", 120)
			lines = append(lines, tc.middle...)
			lines = append(lines, padded("ok  case ", 120)...)
			content := strings.Join(lines, "\n")

			got := snipFailureResult(content)
			if got == content {
				t.Fatalf("nothing elided from %d lines of mostly-quiet output", len(lines))
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("detail %q did not survive:\n%s", want, got)
				}
			}
			if len(got) >= len(content) {
				t.Errorf("result grew: %d -> %d bytes", len(content), len(got))
			}
		})
	}
}
