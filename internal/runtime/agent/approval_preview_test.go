package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/diff"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/builtin"
)

type approvalPreviewGate struct {
	check func()
	allow bool
	calls int
}

func (g *approvalPreviewGate) Check(context.Context, string, json.RawMessage, bool) (bool, string, error) {
	g.calls++
	g.check()
	return g.allow, "synthetic approval", nil
}

func TestPermissionGateReceivesAdmittedPreview(t *testing.T) {
	for _, allow := range []bool{false, true} {
		name := "declined"
		if allow {
			name = "approved"
		}
		t.Run(name, func(t *testing.T) {
			root := testenv.TempDir(t)
			path := filepath.Join(root, "note.txt")
			if err := os.WriteFile(path, []byte("status=draft\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			ws := builtin.Workspace{Dir: root}
			probe := &accessProbeTool{Tool: ws.Tools("edit_file")[0]}
			reg := tool.NewRegistry()
			reg.Add(probe)
			h := &stubHooks{}
			latestDiff, capturedBefore := "", ""
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolDispatch && e.Tool.ID == "preview-edit" {
					latestDiff = e.Tool.Diff
				}
			})
			gate := &approvalPreviewGate{allow: allow, check: func() {
				if !strings.Contains(latestDiff, "-status=draft") || !strings.Contains(latestDiff, "+status=ready") {
					t.Fatalf("Gate.Check received no current file diff: %q", latestDiff)
				}
				if len(h.preSeen) != 0 || capturedBefore != "" || probe.executions != 0 {
					t.Fatal("hook, preimage capture or execution preceded approval")
				}
			}}
			prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
				{toolCallChunk("preview-edit", "edit_file", `{"path":"note.txt","old_string":"draft","new_string":"ready"}`), {Type: provider.ChunkDone}},
				{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
			}}
			a := New(prov, reg, sessionstore.NewSession(""), Options{Hooks: h, Gate: gate, CheckTargetAccess: ws.TargetAccessCheck()}, sink)
			a.SetPreEditHook(func(c diff.Change) {
				if len(h.preSeen) != 1 {
					t.Fatal("preimage capture preceded PreToolUse")
				}
				capturedBefore = c.OldText
			})
			if err := a.Run(context.Background(), "edit note"); err != nil {
				t.Fatal(err)
			}
			if gate.calls != 1 {
				t.Fatalf("Gate.Check calls=%d", gate.calls)
			}
			want, executions := "status=draft\n", 0
			if allow {
				want, executions = "status=ready\n", 1
			}
			if probe.executions != executions || (!allow && (capturedBefore != "" || len(h.preSeen) != 0)) || (allow && capturedBefore != "status=draft\n") {
				t.Fatalf("wrong post-approval effects: executions=%d captured=%q hooks=%v", probe.executions, capturedBefore, h.preSeen)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != want {
				t.Fatalf("file=%q error=%v", got, err)
			}
		})
	}
}

func TestAutoGuardReceivesAdmittedPreview(t *testing.T) {
	root := testenv.TempDir(t)
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("status=draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := builtin.Workspace{Dir: root}
	probe := &accessProbeTool{Tool: ws.Tools("edit_file")[0]}
	reg := tool.NewRegistry()
	reg.Add(probe)
	guard := &recordingRecoveryGate{decision: RecoveryDecision{Blocked: true}}
	h := &stubHooks{}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: h, RecoveryGate: guard, CheckTargetAccess: ws.TargetAccessCheck()}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "guard-edit", Name: "edit_file", Arguments: `{"path":"note.txt","old_string":"draft","new_string":"ready"}`})
	if !out.blocked || len(guard.proposals) != 1 || !strings.Contains(guard.proposals[0].Preview, "+status=ready") {
		t.Fatalf("Auto Guard received no file diff: %+v", guard.proposals)
	}
	if probe.executions != 0 || len(h.preSeen) != 0 {
		t.Fatal("Auto Guard refusal reached hook or execution")
	}
}

type approvalMutatingHooks struct {
	*stubHooks
	path string
}

func (h *approvalMutatingHooks) PreToolUse(ctx context.Context, name string, args json.RawMessage) (bool, string) {
	if err := os.WriteFile(h.path, []byte("status=draft\nhook=ran\n"), 0o644); err != nil {
		return true, err.Error()
	}
	return h.stubHooks.PreToolUse(ctx, name, args)
}

func TestPreimageUsesFileStateAfterPreToolUse(t *testing.T) {
	root := testenv.TempDir(t)
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("status=draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := builtin.Workspace{Dir: root}
	reg := tool.NewRegistry()
	reg.Add(ws.Tools("edit_file")[0])
	h := &approvalMutatingHooks{stubHooks: &stubHooks{}, path: path}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: h, CheckTargetAccess: ws.TargetAccessCheck()}, event.Discard)
	captured := ""
	a.SetPreEditHook(func(change diff.Change) { captured = change.OldText })
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "hook-edit", Name: "edit_file", Arguments: `{"path":"note.txt","old_string":"draft","new_string":"ready"}`})
	if out.blocked || out.errMsg != "" || captured != "status=draft\nhook=ran\n" {
		t.Fatalf("preimage used stale approval state: captured=%q outcome=%+v", captured, out)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "status=ready\nhook=ran\n" {
		t.Fatalf("file=%q error=%v", got, err)
	}
}
