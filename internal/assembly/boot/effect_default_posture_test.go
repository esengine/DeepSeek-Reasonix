package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/permission"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
)

// postureProvider sends its calls in the first round, then finishes.
type postureProvider struct {
	mu    sync.Mutex
	calls []provider.ToolCall
	turn  int
	reqs  []provider.Request
}

func (p *postureProvider) Name() string { return "boot-posture" }

func (p *postureProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.turn++
	turn := p.turn
	p.mu.Unlock()
	ch := make(chan provider.Chunk, len(p.calls)+2)
	if turn == 1 {
		for i := range p.calls {
			ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &p.calls[i]}
		}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

var (
	postureRegister sync.Once
	postureMu       sync.Mutex
	postureCurrent  *postureProvider
)

type postureRun struct {
	dir     string
	ctrl    *control.Controller
	rec     *postureProvider
	results map[string]event.Tool
}

// buildPostureRun assembles a headless session the way `reasonix run` does:
// built failing closed, then moved to the posture it settles on.
func buildPostureRun(t *testing.T, userConfig string, calls ...provider.ToolCall) *postureRun {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &postureProvider{calls: calls}
	postureRegister.Do(func() {
		provider.Register("boot-posture", func(provider.Config) (provider.Provider, error) {
			postureMu.Lock()
			defer postureMu.Unlock()
			return postureCurrent, nil
		})
	})
	postureMu.Lock()
	postureCurrent = rec
	postureMu.Unlock()
	writeUserConfig(t, userConfig)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-posture"
model = "x"
`)
	approveWorkspace(t, dir)
	run := &postureRun{dir: dir, rec: rec, results: map[string]event.Tool{}}
	var mu sync.Mutex
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ToolResult {
			mu.Lock()
			run.results[e.Tool.ID] = e.Tool
			mu.Unlock()
		}
	})
	ctrl, err := Build(context.Background(), Options{Sink: sink, HeadlessApprovalMode: control.ToolApprovalAsk})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { ctrl.Close() })
	run.ctrl = ctrl
	return run
}

func (r *postureRun) runIn(t *testing.T, mode string) {
	t.Helper()
	r.ctrl.ApplyHeadlessApprovalMode(mode)
	_ = r.ctrl.Run(context.Background(), "do the task")
}

// modelSaw is the tool result the provider was handed for id.
func (r *postureRun) modelSaw(t *testing.T, id string) string {
	t.Helper()
	r.rec.mu.Lock()
	defer r.rec.mu.Unlock()
	last := r.rec.reqs[len(r.rec.reqs)-1]
	for _, m := range last.Messages {
		if m.Role == provider.RoleTool && m.ToolCallID == id {
			return m.Content
		}
	}
	t.Fatalf("no result for %s reached the provider", id)
	return ""
}

func writeCall(id, path string) provider.ToolCall {
	return provider.ToolCall{ID: id, Name: "write_file", Arguments: `{"path":` + jsonString(path) + `,"content":"x\n"}`}
}

func bashCall(id, command string) provider.ToolCall {
	return provider.ToolCall{ID: id, Name: "bash", Arguments: `{"command":` + jsonString(command) + `}`}
}

// The default posture is decided by the build's sandbox claim and the folder's
// trust, and what it decides is what reaches the file tools: a write in the
// workspace lands only where both hold, and is otherwise refused with an
// identity the run reports.
func TestEffectDefaultPostureWritesOnlyWhereConfinedAndTrusted(t *testing.T) {
	cases := []struct {
		name    string
		bash    string
		trusted bool
	}{
		{"unconfined trusted", "off", true},
		{"enforce untrusted", "enforce", false},
		{"enforce trusted", "enforce", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := buildPostureRun(t, "[sandbox]\nbash = \""+tc.bash+"\"\n", writeCall("w", "notes.md"))
			if tc.trusted {
				if err := run.ctrl.SetWorkspaceTrust(config.WorkspaceTrusted); err != nil {
					t.Fatal(err)
				}
			}
			wantConfined := tc.bash == "enforce" && sandbox.Available()
			if got := run.ctrl.WritesConfined(); got != wantConfined {
				t.Fatalf("WritesConfined = %v, want %v: the claim must come from the backend", got, wantConfined)
			}
			mode := run.ctrl.DefaultApprovalMode()
			wantWrite := wantConfined && tc.trusted
			if (mode == control.ToolApprovalAuto) != wantWrite {
				t.Fatalf("default posture %q; auto is owed exactly when confined and trusted (%v)", mode, wantWrite)
			}
			run.runIn(t, mode)
			_, err := os.Stat(filepath.Join(run.dir, "notes.md"))
			if landed := err == nil; landed != wantWrite {
				t.Fatalf("write landed = %v, want %v under %q", landed, wantWrite, mode)
			}
			if wantWrite {
				return
			}
			if got := run.results["w"].RefusalCode; got != permission.RefusalUnattended {
				t.Fatalf("the refused write carries %q, want %q", got, permission.RefusalUnattended)
			}
			if saw := run.modelSaw(t, "w"); !strings.Contains(saw, "no interactive approver") {
				t.Fatalf("the model is not told nobody could approve it: %q", saw)
			}
		})
	}
}

// Read-only refuses every writer, an allow rule included, and lets reads run.
func TestEffectReadOnlyRefusesWritesWhateverTheRulesAllow(t *testing.T) {
	for _, write := range []provider.ToolCall{writeCall("w", "out.txt"), bashCall("w", "echo hi > out.txt")} {
		t.Run(write.Name, func(t *testing.T) {
			run := buildPostureRun(t, "[permissions]\nallow = [\"write_file\", \"Bash\"]\n", bashCall("r", "git -C . status"), write)
			run.runIn(t, control.ToolApprovalReadOnly)
			if _, err := os.Stat(filepath.Join(run.dir, "out.txt")); err == nil {
				t.Fatal("out.txt was written in read-only mode")
			}
			if got := run.results["w"].RefusalCode; got != permission.RefusalReadOnly {
				t.Fatalf("the write carries %q, want %q (err %q)", got, permission.RefusalReadOnly, run.results["w"].Err)
			}
			if saw := run.modelSaw(t, "w"); !strings.Contains(saw, "read-only mode") {
				t.Fatalf("the model is not told the cause: %q", saw)
			}
			if got := run.results["r"].RefusalCode; got != "" {
				t.Fatalf("a read was refused in read-only mode: %q", got)
			}
		})
	}
}

// YOLO skips prompts, not rules: a deny rule still refuses under it.
func TestEffectYoloStillHonoursDenyRules(t *testing.T) {
	run := buildPostureRun(t, "[permissions]\ndeny = [\"write_file\"]\n", writeCall("w", "notes.md"))
	run.runIn(t, control.ToolApprovalYolo)
	if _, err := os.Stat(filepath.Join(run.dir, "notes.md")); err == nil {
		t.Fatal("YOLO wrote through a deny rule")
	}
	if got := run.results["w"].RefusalCode; got != permission.RefusalDenyRule {
		t.Fatalf("the refusal carries %q, want %q", got, permission.RefusalDenyRule)
	}
}

// YOLO leaves the sandbox in place: a confined command still cannot write
// outside the workspace.
func TestEffectYoloKeepsTheSandbox(t *testing.T) {
	if !sandbox.Available() {
		t.Skip("no OS sandbox backend on this host; the Linux/macOS legs hold this")
	}
	// Host temp is a write root, so the target sits in this package's own
	// directory, which no write root covers.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	escape, err := os.MkdirTemp(wd, "yolo-escape-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(escape) })
	outside := filepath.Join(escape, "escaped.txt")
	if !sandbox.WriteProtects(sandbox.Spec{Mode: "enforce"}, escape) {
		t.Skipf("%s lies under a directory the sandbox leaves writable on this host", escape)
	}
	run := buildPostureRun(t, "[sandbox]\nbash = \"enforce\"\n", bashCall("b", "echo x > "+shellQuoteForTest(outside)))
	run.runIn(t, control.ToolApprovalYolo)
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("a YOLO shell command wrote outside the sandbox's write roots")
	}
}

// A headless run nobody named a mode for opens on the asking posture, and where
// that is because the folder holds no trust decision, the refusal says so: the
// code the run reports, and the cause and remedy the model reads.
func TestEffectDefaultHeadlessPostureNamesTheUntrustedFolder(t *testing.T) {
	if !sandbox.Available() {
		t.Skip("no OS sandbox backend; the default asks here for a different reason")
	}
	run := buildPostureRun(t, "[sandbox]\nbash = \"enforce\"\n", writeCall("w", "notes.md"))
	if got := run.ctrl.ApplyDefaultHeadlessApprovalMode(); got != control.ToolApprovalAsk {
		t.Fatalf("default headless posture %q, want ask", got)
	}
	_ = run.ctrl.Run(context.Background(), "do the task")
	if _, err := os.Stat(filepath.Join(run.dir, "notes.md")); err == nil {
		t.Fatal("the write landed in an untrusted folder")
	}
	if got := run.results["w"].RefusalCode; got != permission.RefusalUntrustedFolder {
		t.Fatalf("the refused write carries %q, want %q", got, permission.RefusalUntrustedFolder)
	}
	saw := run.modelSaw(t, "w")
	for _, want := range []string{"no trust decision", control.UntrustedFolderRemedy(run.ctrl.WorkspaceRoot(), false)} {
		if !strings.Contains(saw, want) {
			t.Fatalf("the model read %q, want %q", saw, want)
		}
	}
}
