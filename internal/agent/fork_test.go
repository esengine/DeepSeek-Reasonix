package agent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	_ "reasonix/internal/tool/builtin"
)

func forkScriptTurns() [][]provider.Chunk {
	return [][]provider.Chunk{
		{toolCallChunk("c1", "write_file", `{"path":"a.py","content":"x"}`)},
		// Keep this fixture single-target: it measures the evidence-blindness
		// fork, not the cumulative multi-file precondition guard.
		{toolCallChunk("c2", "write_file", `{"path":"a.py","content":"y"}`)},
		{toolCallChunk("c3", "write_file", `{"path":"a.py","content":"z"}`)},
		{{Type: provider.ChunkText, Text: "done"}},
	}
}

func forkRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false})
	return reg
}

func captureForkFixture(t *testing.T) (*ForkBundle, *scriptedProvider) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("REASONIX_EXPERIMENT_FORK_CAPTURE_DIR", dir)
	prov := &scriptedProvider{name: "p", turns: forkScriptTurns()}
	a := New(prov, forkRegistry(), NewSession("sys"), Options{}, event.Discard)
	if err := a.Run(withNoClosedLoop(context.Background()), "fix the widget"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	b, err := LoadForkBundle(filepath.Join(dir, "bundle.json"))
	if err != nil {
		t.Fatalf("LoadForkBundle: %v", err)
	}
	return b, prov
}

func TestForkControlContinuationMatchesOriginalRequest(t *testing.T) {
	b, orig := captureForkFixture(t)
	if len(orig.requests) != 4 {
		t.Fatalf("original run made %d requests, want 4", len(orig.requests))
	}
	if b.BlindAtFork != 3 || b.DebtAtFork == 0 || b.EligibleRound != 3 {
		t.Fatalf("bundle = blind %d debt %d round %d, want 3/>0/3", b.BlindAtFork, b.DebtAtFork, b.EligibleRound)
	}
	if !b.RunwayObserved || b.RunwayBalance <= 0 {
		t.Fatalf("bundle runway = observed %v balance %d, want an observed solvent account", b.RunwayObserved, b.RunwayBalance)
	}
	if b.Input != "fix the widget" {
		t.Fatalf("bundle input = %q", b.Input)
	}

	os.Unsetenv("REASONIX_EXPERIMENT_FORK_CAPTURE_DIR")
	cont := &scriptedProvider{name: "p", turns: [][]provider.Chunk{{{Type: provider.ChunkText, Text: "done"}}}}
	a := New(cont, forkRegistry(), NewSession("sys"), Options{}, event.Discard)
	a.armForkContinuation(b, "")
	if err := a.Run(withNoClosedLoop(context.Background()), b.Input); err != nil {
		t.Fatalf("continuation Run: %v", err)
	}
	if len(cont.requests) == 0 {
		t.Fatal("continuation made no requests")
	}
	// The invariant: the control fork's first provider request equals the one
	// the uninterrupted run made after the eligible round. Any drift is fork
	// contamination (re-classification, duplicate injection, state leakage).
	want, got := orig.requests[3], cont.requests[0]
	if !reflect.DeepEqual(want.Messages, got.Messages) {
		t.Fatalf("control fork payload diverged from original continuation:\nwant %d messages\ngot  %d messages", len(want.Messages), len(got.Messages))
	}
}

func TestLoadLegacyForkBundleLeavesRunwayUnobserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-bundle.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"policy":"ebm","input":"x","messages":[]}`), 0o600); err != nil {
		t.Fatalf("write legacy bundle: %v", err)
	}
	b, err := LoadForkBundle(path)
	if err != nil {
		t.Fatalf("LoadForkBundle: %v", err)
	}
	if b.RunwayObserved || b.RunwayBalance != 0 || b.RunwayDry != 0 || b.RunwayIdle != 0 {
		t.Fatalf("legacy runway fields = %+v, want unobserved zero values", b)
	}
}

func TestForkTreatmentDiffersOnlyByNudge(t *testing.T) {
	b, orig := captureForkFixture(t)
	os.Unsetenv("REASONIX_EXPERIMENT_FORK_CAPTURE_DIR")
	cont := &scriptedProvider{name: "p", turns: [][]provider.Chunk{{{Type: provider.ChunkText, Text: "done"}}}}
	a := New(cont, forkRegistry(), NewSession("sys"), Options{}, event.Discard)
	a.armForkContinuation(b, ebmNudge)
	if err := a.Run(withNoClosedLoop(context.Background()), b.Input); err != nil {
		t.Fatalf("treatment Run: %v", err)
	}
	want, got := orig.requests[3], cont.requests[0]
	if len(want.Messages) != len(got.Messages) {
		t.Fatalf("treatment changed message count: %d vs %d", len(want.Messages), len(got.Messages))
	}
	diffs := 0
	for i := range want.Messages {
		if reflect.DeepEqual(want.Messages[i], got.Messages[i]) {
			continue
		}
		diffs++
		if got.Messages[i].Role != provider.RoleTool ||
			got.Messages[i].Content != want.Messages[i].Content+"\n\n"+ebmNudge {
			t.Fatalf("message %d changed beyond the nudge:\nwant %q\ngot  %q", i, want.Messages[i].Content, got.Messages[i].Content)
		}
	}
	if diffs != 1 {
		t.Fatalf("treatment touched %d messages, want exactly 1", diffs)
	}
}

func TestForkCaptureRefusesTreatedState(t *testing.T) {
	old := ebmEnabled
	ebmEnabled = true
	defer func() { ebmEnabled = old }()

	dir := t.TempDir()
	t.Setenv("REASONIX_EXPERIMENT_FORK_CAPTURE_DIR", dir)
	prov := &scriptedProvider{name: "p", turns: forkScriptTurns()}
	a := New(prov, forkRegistry(), NewSession("sys"), Options{}, event.Discard)
	if err := a.Run(withNoClosedLoop(context.Background()), "fix the widget"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The live run nudged (treatment applied)…
	nudged := false
	for _, m := range prov.requests[len(prov.requests)-1].Messages {
		if m.Role == provider.RoleTool && strings.Contains(m.Content, "[evidence nudge]") {
			nudged = true
		}
	}
	if !nudged {
		t.Fatal("enforcement-on run never nudged; refusal test proved nothing")
	}
	// …so no bundle may exist: a treated state must never become a fork origin.
	if _, err := os.Stat(filepath.Join(dir, "bundle.json")); !os.IsNotExist(err) {
		t.Fatal("capture wrote a bundle under live enforcement")
	}
}

func TestGovernorCaptureFreezesExpensiveExplorationState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REASONIX_EXPERIMENT_FORK_CAPTURE_DIR", dir)
	t.Setenv("REASONIX_EXPERIMENT_FORK_POLICY", "governor")
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_probe", readOnly: true})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkUsage, Usage: &provider.Usage{ReasoningTokens: 2400}},
			toolCallChunk("c1", "read_probe", `{"path":"a.go"}`)},
		{toolCallChunk("c2", "read_probe", `{"path":"b.go"}`)},
		{{Type: provider.ChunkText, Text: "done"}},
	}}
	a := New(prov, reg, NewSession("sys"), Options{}, event.Discard)
	if err := a.Run(withNoClosedLoop(context.Background()), "find the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	b, err := LoadForkBundle(filepath.Join(dir, "bundle.json"))
	if err != nil {
		t.Fatalf("governor bundle missing: %v", err)
	}
	if b.Policy != "governor" || b.DebtAtFork != 0 || b.LocalExecSeen {
		t.Fatalf("bundle = %+v, want governor policy in debt-free no-exec state", b)
	}
	if b.EligibleRound != 1 {
		t.Fatalf("eligible round = %d, want 1 (the expensive exploration round)", b.EligibleRound)
	}
}

func TestForkWorkspaceCopyExcludesHarnessArtifacts(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	os.WriteFile(filepath.Join(src, "code.py"), []byte("v"), 0o644)
	os.WriteFile(filepath.Join(src, ".run-metrics.json"), []byte("{}"), 0o644)
	if err := copyWorkspace(src, filepath.Join(dst, "ws")); err != nil {
		t.Fatalf("copyWorkspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "ws", "code.py")); err != nil {
		t.Fatal("workspace file missing from copy")
	}
	if _, err := os.Stat(filepath.Join(dst, "ws", ".run-metrics.json")); !os.IsNotExist(err) {
		t.Fatal("harness sidecar must not enter the bundle workspace")
	}
}
