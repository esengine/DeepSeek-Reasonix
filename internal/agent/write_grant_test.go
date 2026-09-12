package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/permission"
	"reasonix/internal/testenv"
	"reasonix/internal/tool"
)

// answerGate records what it was asked and answers with a fixed verdict.
type answerGate struct {
	allow    bool
	reason   string
	asked    []string
	askedFor []string
}

func (g *answerGate) Check(_ context.Context, toolName string, args json.RawMessage, _ bool) (bool, string, error) {
	g.asked = append(g.asked, toolName)
	var body struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(args, &body)
	g.askedFor = append(g.askedFor, body.Path)
	return g.allow, g.reason, nil
}

func fenceFixture(t *testing.T, gate Gate) (root string, writer tool.Tool, inner *recordingWriter) {
	t.Helper()
	root = testenv.TempDir(t)
	if err := os.MkdirAll(filepath.Join(root, "auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	claim, err := NormalizeWritePaths(root, []string{"auth"})
	if err != nil {
		t.Fatal(err)
	}
	inner = &recordingWriter{name: "write_file", writesPaths: true}
	reg := tool.NewRegistry()
	reg.Add(inner)
	bound, _ := BindWritePaths(reg, NewWriteGrant(claim), gate, NewSubagentScheduler(4, 2), root, false)
	return root, mustGet(t, bound, "write_file"), inner
}

// What a run needs to touch is not always knowable before it has read anything.
// A path outside the declaration is a question, and a yes lets the write land.
func TestWideningIsGrantedByTheUserAndTheWriteLands(t *testing.T) {
	gate := &answerGate{allow: true}
	root, writer, inner := fenceFixture(t, gate)
	target := filepath.Join(root, "package.json")

	if _, err := writer.Execute(context.Background(), json.RawMessage(`{"path":`+jsonPath(target)+`,"content":"x"}`)); err != nil {
		t.Fatalf("granted write refused: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner writer ran %d times, want 1", inner.calls)
	}
	// Asked as its own capability, not as another prompt on write_file.
	if len(gate.asked) != 1 || gate.asked[0] != permission.ExtendWritePaths {
		t.Fatalf("gate was asked %v, want one %q", gate.asked, permission.ExtendWritePaths)
	}
	if len(gate.askedFor) != 1 || gate.askedFor[0] != target {
		t.Fatalf("gate was asked about %v, want the resolved target %q", gate.askedFor, target)
	}
}

// A no is a no, and nothing runs behind it.
func TestWideningRefusedLeavesTheFenceClosed(t *testing.T) {
	gate := &answerGate{allow: false, reason: "not this one"}
	root, writer, inner := fenceFixture(t, gate)

	_, err := writer.Execute(context.Background(), json.RawMessage(`{"path":`+jsonPath(filepath.Join(root, "package.json"))+`,"content":"x"}`))
	if !errors.Is(err, ErrWriteFenceClosed) {
		t.Fatalf("refused widening = %v, want ErrWriteFenceClosed", err)
	}
	if inner.calls != 0 {
		t.Fatalf("inner writer ran %d times behind a refusal", inner.calls)
	}
}

// The user is asked once per path, not once per write: a granted path stays
// granted for the rest of the run.
func TestAGrantedPathIsNotAskedAboutTwice(t *testing.T) {
	gate := &answerGate{allow: true}
	root, writer, _ := fenceFixture(t, gate)
	args := json.RawMessage(`{"path":` + jsonPath(filepath.Join(root, "package.json")) + `,"content":"x"}`)

	for i := range 3 {
		if _, err := writer.Execute(context.Background(), args); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if len(gate.asked) != 1 {
		t.Fatalf("user was asked %d times for one path, want 1", len(gate.asked))
	}
}

// A path inside the declaration was proven non-overlapping before anything
// started; asking about it would be a prompt for something already settled.
func TestADeclaredPathIsNeverAskedAbout(t *testing.T) {
	gate := &answerGate{allow: true}
	root, writer, inner := fenceFixture(t, gate)

	if _, err := writer.Execute(context.Background(), json.RawMessage(`{"path":`+jsonPath(filepath.Join(root, "auth", "token.go"))+`,"content":"x"}`)); err != nil {
		t.Fatal(err)
	}
	if len(gate.asked) != 0 {
		t.Fatalf("declared path raised %d questions, want none", len(gate.asked))
	}
	if inner.calls != 1 {
		t.Fatalf("inner writer ran %d times, want 1", inner.calls)
	}
}

// The audit compares observed mutations against everything the run was allowed
// to write. A path the user granted must not surface as an escape.
func TestGrantedPathsCountAsAllowedInTheAudit(t *testing.T) {
	root := testenv.TempDir(t)
	if err := os.MkdirAll(filepath.Join(root, "auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "package.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	claim, err := NormalizeWritePaths(root, []string{"auth"})
	if err != nil {
		t.Fatal(err)
	}
	grant := NewWriteGrant(claim)
	if grant.Scope().AllowsPath(outside) {
		t.Fatal("an ungranted path is already in scope")
	}
	grant.Add(outside)
	if !grant.Scope().AllowsPath(outside) {
		t.Error("a granted path is missing from the audit scope; it would be reported as an escape")
	}
	// Scheduling still reads only what was declared: nothing added later was
	// proven against the runs that had already started.
	if grant.Declared().AllowsPath(outside) {
		t.Error("a granted path leaked into the declared set that scheduling parallelizes on")
	}
}
