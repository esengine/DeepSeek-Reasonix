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
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/builtin"
)

type accessProbeTool struct {
	tool.Tool
	previews, executions int
}

func (p *accessProbeTool) WritePaths(args json.RawMessage) ([]string, error) {
	return p.Tool.(tool.WritePathResolver).WritePaths(args)
}
func (p *accessProbeTool) ReadTarget(args json.RawMessage) string {
	if reader, ok := p.Tool.(tool.ReadTargeter); ok {
		return reader.ReadTarget(args)
	}
	return ""
}
func (p *accessProbeTool) WritesNamedPaths() bool { return tool.WritesNamedPaths(p.Tool) }
func (p *accessProbeTool) Preview(ctx context.Context, args json.RawMessage) (diff.Change, error) {
	p.previews++
	if pv, ok := p.Tool.(tool.Previewer); ok {
		return pv.Preview(ctx, args)
	}
	return diff.Change{}, nil
}
func (p *accessProbeTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	p.executions++
	return p.Tool.Execute(ctx, args)
}

func TestPreToolAccessRejectsForbiddenFileTools(t *testing.T) {
	root := testenv.TempDir(t)
	private := filepath.Join(root, "private")
	if err := os.MkdirAll(private, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(private, "note.go")
	const original = "package main\n// ACCESS_CANARY_DO_NOT_DISCLOSE\nfunc RemoveMe() {}\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := builtin.Workspace{Dir: root, ForbidReadRoots: []string{private}}
	for _, name := range []string{"read_file", "edit_file", "multi_edit", "write_file", "notebook_edit", "delete_range", "delete_symbol"} {
		t.Run(name, func(t *testing.T) {
			tl := ws.Tools(name)[0]
			probe := &accessProbeTool{Tool: tl}
			reg := tool.NewRegistry()
			reg.Add(probe)
			h := &stubHooks{}
			a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: h, CheckTargetAccess: ws.TargetAccessCheck()}, event.Discard)
			args, _ := json.Marshal(map[string]any{"path": path, "old_string": "not present", "new_string": "changed", "content": "changed", "edits": []map[string]string{{"old_string": "not present", "new_string": "changed"}}, "cell_number": 0, "new_source": "changed", "start_anchor": "package main", "end_anchor": "func RemoveMe() {}", "name": "RemoveMe", "source_path": path, "destination_path": filepath.Join(root, "moved.go")})
			out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "blocked", Name: name, Arguments: string(args)})
			message, code := "Cannot read target file: permission denied.", builtin.CodeReadForbidden
			if name == "write_file" {
				message, code = "Cannot overwrite target file: permission to read existing contents is required.", builtin.CodeOverwriteReadForbidden
			}
			if !out.blocked || out.output != message || out.refusalCode != code {
				t.Fatalf("wrong refusal: %+v", out)
			}
			if probe.previews != 0 || probe.executions != 0 || len(h.preSeen) != 0 {
				t.Fatalf("denied access reached hook or tool: previews=%d execute=%d hooks=%v", probe.previews, probe.executions, h.preSeen)
			}
			if b, err := os.ReadFile(path); err != nil || string(b) != original {
				t.Fatalf("protected file changed: %q %v", b, err)
			}
		})
	}
}

func TestPreToolUseReadOnlyDoesNotRecordHookWriteGap(t *testing.T) {
	root := testenv.TempDir(t)
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("status=draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := builtin.Workspace{Dir: root}
	reg := tool.NewRegistry()
	reg.Add(ws.Tools("read_file")[0])
	store := checkpoint.New("", root)
	store.Begin(1, "read the file", 0)
	a := New(nil, reg, sessionstore.NewSession(""), Options{
		Hooks:             &stubHooks{},
		CheckTargetAccess: ws.TargetAccessCheck(),
	}, event.Discard)
	a.SetMutationObserver(checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store}))

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
		ID:        "read-only-hook-gap",
		Name:      "read_file",
		Arguments: `{"path":"note.txt"}`,
	})
	if out.blocked || out.errMsg != "" {
		t.Fatalf("read_file failed: %+v", out)
	}
	metas := store.List()
	if len(metas) != 1 {
		t.Fatalf("checkpoint count = %d, want 1", len(metas))
	}
	for _, gap := range metas[0].CoverageGaps {
		if gap.Reason == checkpoint.GapHookWrite {
			t.Fatalf("read-only call recorded a hook-write gap: %+v", gap)
		}
	}
}
func TestPreToolUseRejectsBeforePreimageAndExecution(t *testing.T) {
	root := testenv.TempDir(t)
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("status=draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := builtin.Workspace{Dir: root}
	probe := &accessProbeTool{Tool: ws.Tools("edit_file")[0]}
	reg := tool.NewRegistry()
	reg.Add(probe)
	h := &stubHooks{blockPre: map[string]bool{"edit_file": true}}
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "edit_file", `{"path":"note.txt","old_string":"draft","new_string":"ready"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "denied"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{Hooks: h, CheckTargetAccess: ws.TargetAccessCheck()}, event.Discard)
	preimages := 0
	a.SetPreEditHook(func(diff.Change) { preimages++ })
	if err := a.Run(context.Background(), "edit note"); err != nil {
		t.Fatal(err)
	}
	if len(h.preSeen) != 1 || probe.previews != 1 || probe.executions != 0 || preimages != 0 {
		t.Fatalf("hook refusal captured or executed a write: hooks=%v previews=%d execute=%d preimages=%d", h.preSeen, probe.previews, probe.executions, preimages)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "status=draft\n" {
		t.Fatalf("hook refusal changed the file: %q %v", got, err)
	}
}

func TestPreToolAccessReadScopeAndAllowedEdit(t *testing.T) {
	root := testenv.TempDir(t)
	outside := testenv.TempDir(t)
	path := filepath.Join(outside, "note.txt")
	if err := os.WriteFile(path, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := builtin.Workspace{Dir: root, WriteRoots: []string{root, outside}, ReadRoots: []string{root}}
	reg := tool.NewRegistry()
	for _, tl := range ws.Tools("read_file", "edit_file") {
		reg.Add(tl)
	}
	a := New(nil, reg, sessionstore.NewSession(""), Options{CheckTargetAccess: ws.TargetAccessCheck()}, event.Discard)
	args, _ := json.Marshal(map[string]string{"path": path, "old_string": "outside", "new_string": "changed"})
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "edit_file", Arguments: string(args)})
	if !out.blocked || out.output != "Cannot read target file: permission denied." || out.refusalCode != builtin.CodeReadOutsideScope {
		t.Fatalf("read scope not enforced: %+v", out)
	}
	local := filepath.Join(root, "note.txt")
	if err := os.WriteFile(local, []byte("draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ = json.Marshal(map[string]string{"path": local, "old_string": "draft", "new_string": "ready"})
	out = a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "allowed", Name: "edit_file", Arguments: string(args)})
	if out.blocked || out.errMsg != "" || out.preview == nil || !strings.Contains(out.preview.Diff, "ready") {
		t.Fatalf("allowed edit failed: %+v", out)
	}
	if b, err := os.ReadFile(local); err != nil || string(b) != "ready\n" {
		t.Fatalf("allowed edit not applied: %q %v", b, err)
	}
}

type accessNestedCaller struct{ args json.RawMessage }

func (accessNestedCaller) Name() string            { return "access_nested" }
func (accessNestedCaller) Description() string     { return "nested access probe" }
func (accessNestedCaller) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (accessNestedCaller) ReadOnly() bool          { return true }
func (n accessNestedCaller) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	inv, _ := tool.InvokerFrom(ctx)
	return inv.Invoke(ctx, "edit_file", n.args)
}

func TestPreToolAccessNestedAndBatch(t *testing.T) {
	root := testenv.TempDir(t)
	private := filepath.Join(root, "private")
	if err := os.MkdirAll(private, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(private, "note.txt")
	const original = "PRIVATE_CANARY=do-not-disclose\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := builtin.Workspace{Dir: root, ForbidReadRoots: []string{private}}
	probe := &accessProbeTool{Tool: ws.Tools("edit_file")[0]}
	reg := tool.NewRegistry()
	reg.Add(probe)
	args, _ := json.Marshal(map[string]string{"path": path, "old_string": "PRIVATE_CANARY", "new_string": "changed"})
	reg.Add(accessNestedCaller{args: args})
	a := New(nil, reg, sessionstore.NewSession(""), Options{CheckTargetAccess: ws.TargetAccessCheck()}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "access_nested", Arguments: `{}`})
	if !strings.Contains(out.output, "Cannot read target file: permission denied.") || probe.previews != 0 || probe.executions != 0 {
		t.Fatalf("nested call bypassed access check: %+v previews=%d exec=%d", out, probe.previews, probe.executions)
	}
	public := filepath.Join(root, "public.txt")
	if err := os.WriteFile(public, []byte("draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, _ := json.Marshal(map[string]string{"path": public, "old_string": "draft", "new_string": "ready"})
	calls := []provider.ToolCall{{ID: "first", Name: "edit_file", Arguments: string(first)}, {ID: "second", Name: "edit_file", Arguments: string(args)}}
	a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: calls})
	result := a.executeBatch(context.Background(), &a.turn, calls)
	if !result.outcomes[1].blocked || probe.previews != 2 || probe.executions != 1 {
		t.Fatalf("batch access failed: previews=%d executions=%d outcomes=%+v", probe.previews, probe.executions, result.outcomes)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != original {
		t.Fatalf("protected file changed: %q %v", b, err)
	}
}

func TestPreToolAccessThroughPathBoundSubagentWriter(t *testing.T) {
	root := testenv.TempDir(t)
	path := filepath.Join(root, "private.txt")
	const original = "INTERNAL_NOTE=review-canary-do-not-disclose\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	ws := builtin.Workspace{Dir: root, ForbidReadRoots: []string{path}}
	probe := &accessProbeTool{Tool: ws.Tools("edit_file")[0]}
	reg := tool.NewRegistry()
	reg.Add(probe)
	claim, err := writeclaim.NormalizeWritePaths(root, []string{"private.txt"})
	if err != nil {
		t.Fatal(err)
	}
	bound, removed := BindWritePaths(reg, writeclaim.NewWriteGrant(claim), nil, writeclaim.NewSubagentScheduler(4, 2), root, false)
	if len(removed) != 0 {
		t.Fatalf("writer removed: %v", removed)
	}
	wrapped, _ := bound.Get("edit_file")
	if _, ok := wrapped.(pathBoundWriter); !ok {
		t.Fatalf("not the production wrapper: %T", wrapped)
	}
	h := &stubHooks{}
	a := New(nil, bound, sessionstore.NewSession(""), Options{Hooks: h, CheckTargetAccess: ws.TargetAccessCheck()}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "edit_file", Arguments: `{"path":"private.txt","old_string":"missing","new_string":"x"}`})
	if !out.blocked || out.output != "Cannot read target file: permission denied." || probe.previews != 0 || probe.executions != 0 || len(h.preSeen) != 0 {
		t.Fatalf("wrapped writer bypassed admission: %+v previews=%d execute=%d hooks=%v", out, probe.previews, probe.executions, h.preSeen)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != original {
		t.Fatalf("private file changed: %q %v", data, err)
	}
}

func TestPreToolAccessChecksResolvedProxyTarget(t *testing.T) {
	root := testenv.TempDir(t)
	path := filepath.Join(root, "private.txt")
	if err := os.WriteFile(path, []byte("INTERNAL_NOTE=do-not-disclose\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws := builtin.Workspace{Dir: root, ForbidReadRoots: []string{path}}
	probe := &accessProbeTool{Tool: ws.Tools("edit_file")[0]}
	args := json.RawMessage(`{"path":"private.txt","old_string":"missing","new_string":"x"}`)
	reg := tool.NewRegistry()
	reg.Add(readOnlyBoundaryProxy{resolved: tool.ResolvedCall{TargetName: "edit_file", Target: probe, Args: args, ReadOnly: false}})
	h := &stubHooks{}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: h, CheckTargetAccess: ws.TargetAccessCheck()}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "use_capability", Arguments: `{"path":"public.txt"}`})
	if !out.blocked || out.refusalCode != builtin.CodeReadForbidden || probe.previews != 0 || probe.executions != 0 || len(h.preSeen) != 0 {
		t.Fatalf("proxy checked its facade rather than the resolved target: %+v previews=%d execute=%d hooks=%v", out, probe.previews, probe.executions, h.preSeen)
	}
}

func TestPreToolAccessRejectsForbiddenWriterAlias(t *testing.T) {
	root := testenv.TempDir(t)
	private := filepath.Join(root, "private")
	if err := os.MkdirAll(private, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(private, "secret.txt")
	if err := os.WriteFile(path, []byte("PRIVATE_CANARY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias.txt")
	if err := os.Symlink(path, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	ws := builtin.Workspace{Dir: root, ForbidReadRoots: []string{private}}
	probe := &accessProbeTool{Tool: ws.Tools("write_file")[0]}
	reg := tool.NewRegistry()
	reg.Add(probe)
	a := New(nil, reg, sessionstore.NewSession(""), Options{CheckTargetAccess: ws.TargetAccessCheck()}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "write_file", Arguments: `{"path":"alias.txt","content":"changed"}`})
	if !out.blocked || out.refusalCode != builtin.CodeOverwriteReadForbidden || probe.previews != 0 || probe.executions != 0 {
		t.Fatalf("ambiguous writer alias skipped read policy: %+v previews=%d execute=%d", out, probe.previews, probe.executions)
	}
}
