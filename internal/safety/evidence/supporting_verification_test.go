package evidence

import (
	"slices"
	"testing"
)

func TestSupportingVerificationExemptionRequiresProvenScope(t *testing.T) {
	note := Receipt{ToolName: "write_file", Success: true, Write: true, Mutation: true, MutationEvidence: MutationProven, Paths: []string{"notes.md"}}
	for _, tc := range []struct {
		name       string
		extra      []Receipt
		checks     []string
		wantDebt   bool
		mixed      bool
		unobserved bool
		delivery   bool
	}{
		{name: "supporting only"},
		{name: "move code to prose", extra: []Receipt{{Success: true, Write: true, Mutation: true, MutationEvidence: MutationProven, Paths: []string{"main.go", "main.md"}}}, wantDebt: true},
		{name: "delete code", extra: []Receipt{{Success: true, Mutation: true, MutationEvidence: MutationProven, PathsComplete: true, Paths: []string{"main.go"}}}, wantDebt: true},
		{name: "created code", extra: []Receipt{{Success: true, Write: true, Mutation: true, MutationEvidence: MutationProven, Paths: []string{"other.md"}, Created: []string{"main.go"}}}, wantDebt: true},
		{name: "deleted uppercase document", extra: []Receipt{{Success: true, Write: true, Mutation: true, MutationEvidence: MutationProven, Paths: []string{"gone.MD"}}}, wantDebt: true},
		{name: "blank path", extra: []Receipt{{Success: true, Write: true, Mutation: true, MutationEvidence: MutationProven, Paths: []string{"other.md", ""}}}, wantDebt: true},
		{name: "declared check", checks: []string{"go test ./..."}, wantDebt: true},
		{name: "code before supporting", extra: []Receipt{{ToolName: "write_file", Success: true, Write: true, Mutation: true, MutationEvidence: MutationProven, Paths: []string{"main.go"}}}, mixed: true, wantDebt: true},
		{name: "no complete observation", unobserved: true, wantDebt: true},
		{name: "delivery", delivery: true, wantDebt: true},
		{name: "opaque", extra: []Receipt{{ToolName: "mcp__ops__write", Success: true, Mutation: true, MutationEvidence: MutationProven}}, wantDebt: true},
		{name: "unproven supporting", extra: []Receipt{{ToolName: "bash", Success: true, Mutation: true, MutationEvidence: MutationUnknown, Paths: []string{"other.md"}}}, wantDebt: true},
		{name: "partial observed scope", extra: []Receipt{{ToolName: "bash", Success: true, Mutation: true, MutationEvidence: MutationProven, Paths: []string{"other.md"}}}, wantDebt: true},
		{name: "complete observed supporting", extra: []Receipt{{ToolName: "bash", Success: true, Mutation: true, MutationEvidence: MutationProven, PathsComplete: true, Paths: []string{"other.md"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := ledgerOf(append(tc.extra, note)...)
			contract := CaptureCheckContract(tc.checks, tc.checks).WithWorkspaceProseOnly(!tc.mixed && !tc.unobserved, tc.delivery).WithObserveRoot(t.TempDir())
			if debt := slices.ContainsFunc(l.Obligations(contract), func(o Obligation) bool { return o.Kind == ObligationStaleVerification }); debt != tc.wantDebt {
				t.Fatalf("stale_verification = %v, want %v", debt, tc.wantDebt)
			}
		})
	}
}
