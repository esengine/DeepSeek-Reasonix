package cli

import (
	"errors"
	"strings"
	"testing"
)

// A failed run must say why. -p/--print builds a result sink, and text output
// carries only the final response, so the failure had no path to the user at
// all: exit 1, empty stderr.
func TestReportRunFailureAlwaysExplainsTextRuns(t *testing.T) {
	runErr := errors.New("context maintenance is blocked for the current view")
	cases := []struct {
		name       string
		format     runOutputFormat
		structured bool
		want       string
	}{
		{"print mode builds a sink", runOutputText, true, "context maintenance is blocked"},
		{"plain text without a sink", runOutputText, false, "context maintenance is blocked"},
		{"json carries it in the payload", runOutputJSON, true, ""},
		{"json without a sink still reports", runOutputJSON, false, "context maintenance is blocked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			reportRunFailure(&out, tc.format, tc.structured, classifyRunCompletion(runErr), runErr)
			if tc.want == "" {
				if out.Len() != 0 {
					t.Fatalf("wrote %q, want the structured payload to carry it alone", out.String())
				}
				return
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("wrote %q, want it to contain %q", out.String(), tc.want)
			}
		})
	}
}

func TestReportRunFailureStaysSilentOnSuccess(t *testing.T) {
	var out strings.Builder
	reportRunFailure(&out, runOutputText, true, classifyRunCompletion(nil), nil)
	if out.Len() != 0 {
		t.Fatalf("successful run wrote %q", out.String())
	}
}
