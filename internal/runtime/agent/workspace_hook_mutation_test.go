package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/hook"
	"reasonix/internal/state/sessionstore"
)

type workspaceEffectHooks struct {
	typedNilHooks
	pre     func() (bool, string)
	post    func()
	failure func()
}

func (h workspaceEffectHooks) PreToolUse(context.Context, string, json.RawMessage) (bool, string) {
	if h.pre != nil {
		return h.pre()
	}
	return false, ""
}
func (h workspaceEffectHooks) PostToolUse(context.Context, string, json.RawMessage, string) {
	if h.post != nil {
		h.post()
	}
}
func (h workspaceEffectHooks) PostToolUseFailure(context.Context, string, json.RawMessage, string, error) {
	if h.failure != nil {
		h.failure()
	}
}

func assertHookInvalidation(t *testing.T, sink *workspaceSignalSink, id string) {
	t.Helper()
	select {
	case m := <-sink.mutations:
		if m.ToolID != id || !m.AllPaths || !m.Content || !m.Tree || !m.WorkingTree || !m.GitMeta || len(m.Paths) != 0 {
			t.Fatalf("hook invalidation = %+v", m)
		}
	default:
		t.Fatal("hook effects did not reach the workspace sink")
	}
	select {
	case m := <-sink.mutations:
		t.Fatalf("invocation published twice: %+v", m)
	default:
	}
}

func TestWorkspaceHookEffectsSurviveToolClassificationAndRefusal(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		readOnly, deny, fail, post bool
	}{
		{name: "denied writer", deny: true},
		{name: "denied reader", readOnly: true, deny: true},
		{name: "successful reader", readOnly: true},
		{name: "successful writer"},
		{name: "post reader", readOnly: true, post: true},
		{name: "failure hook", readOnly: true, fail: true, post: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "hook-side.txt")
			write := func() {
				if err := os.WriteFile(path, []byte("hook changed bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			hooks := workspaceEffectHooks{}
			hooks.pre = func() (bool, string) {
				if !tc.post {
					write()
				}
				return tc.deny, "hook refusal"
			}
			if tc.post {
				hooks.post, hooks.failure = write, write
			}
			var toolErr error
			if tc.fail {
				toolErr = errors.New("tool failed")
			}
			reg := tool.NewRegistry()
			reg.Add(fakeTool{name: "probe", readOnly: tc.readOnly, err: toolErr})
			sink := newWorkspaceSignalSink()
			a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: &hooks}, sink)
			batch := a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{{ID: "call", Name: "probe", Arguments: `{"path":"tool-only.txt"}`}})
			if data, err := os.ReadFile(path); err != nil || string(data) != "hook changed bytes" {
				t.Fatalf("hook did not write: %q %v", data, err)
			}
			if batch.outcomes[0].executed == tc.deny {
				t.Fatalf("tool execution misreported: %+v", batch.outcomes[0])
			}
			assertHookInvalidation(t, sink, "call")
			results := sink.kinds(event.ToolResult)
			if len(results) != 1 || !results[0].Tool.WorkspaceMutation || !results[0].Tool.WorkspaceAllPaths || results[0].Tool.Executed == tc.deny {
				t.Fatalf("effect and tool execution conflated: %+v", results)
			}
		})
	}
}

func TestWorkspaceHookPanicStillInvalidates(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "probe", readOnly: true})
	sink := newWorkspaceSignalSink()
	path := filepath.Join(t.TempDir(), "partial.txt")
	a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: &workspaceEffectHooks{pre: func() (bool, string) {
		if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
		panic("hook callback failed")
	}}}, sink)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("callback panic was swallowed")
			}
		}()
		a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "panic", Name: "probe", Arguments: `{}`})
	}()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	assertHookInvalidation(t, sink, "panic")
}

func TestWorkspaceHooksNotEnteredDoNotInvalidate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hooks ToolHooks
		deny  bool
	}{
		{name: "nil"},
		{name: "empty runner", hooks: hook.NewRunner(nil, t.TempDir(), nil, nil)},
		{name: "session-only runner", hooks: hook.NewRunner([]hook.ResolvedHook{{Event: hook.SessionStart}}, t.TempDir(), nil, nil)},
		{name: "permission denied", hooks: &workspaceEffectHooks{pre: func() (bool, string) { t.Fatal("denied call reached hooks"); return false, "" }}, deny: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := tool.NewRegistry()
			reg.Add(fakeTool{name: "probe", readOnly: true})
			sink := newWorkspaceSignalSink()
			opts := Options{Hooks: tc.hooks}
			if tc.deny {
				opts.CheckTargetAccess = func(context.Context, tool.Tool, json.RawMessage) error { return errors.New("access denied") }
			}
			a := New(nil, reg, sessionstore.NewSession(""), opts, sink)
			a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "noop", Name: "probe", Arguments: `{}`})
			select {
			case m := <-sink.mutations:
				t.Fatalf("no entered writer/hook: %+v", m)
			default:
			}
		})
	}
}

func TestWorkspaceNestedWriterPublishesOwnInvalidation(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "edit_file"})
	reg.Add(accessNestedCaller{args: json.RawMessage(`{"path":"nested.txt"}`)})
	sink := newWorkspaceSignalSink()
	a := New(nil, reg, sessionstore.NewSession(""), Options{}, sink)
	a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "parent", Name: "access_nested", Arguments: `{}`})
	select {
	case m := <-sink.mutations:
		if m.ToolName != "edit_file" || len(m.Paths) != 1 || m.Paths[0] != "nested.txt" {
			t.Fatalf("nested mutation: %+v", m)
		}
	default:
		t.Fatal("nested writer had no workspace invalidation")
	}
	results := sink.kinds(event.ToolResult)
	if len(results) != 1 || !results[0].Tool.WorkspaceMutation || len(results[0].Tool.WorkspacePaths) != 1 || results[0].Tool.WorkspacePaths[0] != "nested.txt" {
		t.Fatalf("nested ToolResult lost mutation: %+v", results)
	}
}
