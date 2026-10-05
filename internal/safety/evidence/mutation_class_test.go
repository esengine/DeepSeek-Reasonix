package evidence

import (
	"encoding/json"
	"testing"
)

func bashClassOf(t *testing.T, command string) string {
	t.Helper()
	args, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return ToolCallMutationClass("bash", args, false)
}

// The read-only check commands a real project runs after an edit. Grading them
// as proven writes is what pushed delivery review to a high-risk gate whose
// only instruction — name the changed files — had no answer.
func TestBashMutationClassChecksAreNotProvenWrites(t *testing.T) {
	for _, command := range []string{
		"gofmt -l .",
		"gofmt -l parse.go stats.go",
		"prettier --check .",
		"eslint .",
		"cargo fmt --check",
		"black --check .",
	} {
		if got := bashClassOf(t, command); got == MutationProven {
			t.Errorf("%q classified as %s, want unknown or none", command, got)
		}
	}
}

func TestBashMutationClassProvenWrites(t *testing.T) {
	for _, command := range []string{
		"printf hi > out.log",
		"go test ./... > report.txt 2>&1",
		"cat notes.md >> journal.md",
		"find . -name '*.tmp' -delete",
		"sort names.txt -o names.txt",
	} {
		if got := bashClassOf(t, command); got != MutationProven {
			t.Errorf("%q classified as %s, want %s", command, got, MutationProven)
		}
	}
}

func TestBashMutationClassProvenReadOnly(t *testing.T) {
	for _, command := range []string{
		"go test ./...",
		"go vet ./... && go test ./...",
		"go test ./... 2>&1 | tail -5",
		"cat parse.go",
		"ls -la",
		"go test ./... > /dev/null",
	} {
		if got := bashClassOf(t, command); got != MutationNone {
			t.Errorf("%q classified as %s, want %s", command, got, MutationNone)
		}
	}
}

// Unknown is a class of its own, not a synonym for either neighbour: the host
// can neither run the command's contract nor parse it into one.
func TestBashMutationClassUnknown(t *testing.T) {
	for _, command := range []string{
		"some-unknown-writer",
		"sed -i '' 's/a/b/' parse.go",
		"./deploy.sh",
	} {
		if got := bashClassOf(t, command); got != MutationUnknown {
			t.Errorf("%q classified as %s, want %s", command, got, MutationUnknown)
		}
	}
}

// The boolean floors that permission and delivery rely on must not loosen:
// unknown still counts as a mutation everywhere Mutation is consulted.
func TestUnknownStillCountsAsMutation(t *testing.T) {
	args := json.RawMessage(`{"command":"sed -n '1p' out.txt"}`)
	if !ToolCallMutates("bash", args, false) {
		t.Fatal("unknown command must still count as a mutation")
	}
	r := ReceiptFromToolCall("bash", args, true, ToolFacts{})
	if !r.Mutation {
		t.Fatal("receipt must record the mutation")
	}
	if r.MutationEvidence != MutationUnknown {
		t.Fatalf("MutationEvidence = %q, want %q", r.MutationEvidence, MutationUnknown)
	}
}

// A path-less mutation from an older session carries no grade, and an ungraded
// receipt cannot retroactively prove it wrote something.
func TestUngradedMutationDoesNotScoreOpaque(t *testing.T) {
	legacy := []Receipt{{ToolName: "bash", Success: true, Mutation: true, Command: "some-unknown-writer"}}
	if got := ClassifyMutationRisk(legacy, 0, nil); got != RiskLow {
		t.Fatalf("ungraded risk = %s, want %s", got, RiskLow)
	}
}

// The run C shape: an edit whose paths are known, then a check the host cannot
// prove read-only. The check must not become the change set the reviewer sees.
func TestCheckAfterEditKeepsPathScoredRisk(t *testing.T) {
	receipts := []Receipt{
		ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/runtime/agent/agent.go"}`), true, ToolFacts{WritesNamedPaths: true}),
		ReceiptFromToolCall("bash", json.RawMessage(`{"command":"gofmt -l ."}`), true, ToolFacts{}),
	}
	if got := ClassifyMutationRisk(receipts, 0, nil); got != RiskMedium {
		t.Fatalf("risk after check = %s, want %s", got, RiskMedium)
	}
}

// A tool the host cannot see into is unknown until the workspace answers; a
// writer that names its paths stays proven, and read-only stays none.
func TestToolCallMutationClassWithUnstatedEffects(t *testing.T) {
	args := json.RawMessage(`{}`)
	cases := []struct {
		facts ToolFacts
		want  string
	}{
		{ToolFacts{EffectsUnstated: true}, MutationUnknown},
		{ToolFacts{EffectsUnstated: true, ReadOnly: true}, MutationNone},
		{ToolFacts{WritesNamedPaths: true}, MutationProven},
		{ToolFacts{}, MutationProven},
	}
	for _, c := range cases {
		if got := ToolCallMutationClassWith("mcp__ops__deploy", args, c.facts); got != c.want {
			t.Fatalf("%+v: class = %q, want %q", c.facts, got, c.want)
		}
	}
	if r := ReceiptFromToolCall("mcp__ops__deploy", args, true, ToolFacts{EffectsUnstated: true}); !r.Mutation || r.MutationEvidence != MutationUnknown {
		t.Fatalf("receipt = %+v, want an unproven mutation for the workspace to settle", r)
	}
}
