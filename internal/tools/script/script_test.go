package script

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
)

// fakeInvoker answers read_file with the path it was asked for, fails bash,
// and records every call it receives.
type fakeInvoker struct{ calls []string }

func (f *fakeInvoker) Invoke(_ context.Context, name string, args json.RawMessage) (string, error) {
	f.calls = append(f.calls, name+" "+string(args))
	switch name {
	case "read_file":
		var p struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(args, &p)
		return "contents of " + p.Path, nil
	case "bash":
		return "exit status 1", fmt.Errorf("%w: bash: exit status 1", tool.ErrNestedCallFailed)
	default:
		return `{"items":[1,2,3]}`, nil
	}
}

func runWith(t *testing.T, inv tool.Invoker, src string) (string, error) {
	t.Helper()
	ctx := t.Context()
	if inv != nil {
		ctx = tool.WithInvoker(ctx, inv)
	}
	args, _ := json.Marshal(map[string]string{"script": src})
	return New().Execute(ctx, args)
}

func TestScriptCallsToolsAndReturnsWhatItPrinted(t *testing.T) {
	inv := &fakeInvoker{}
	out, err := runWith(t, inv, `
for p in ["a.go", "b.go"]:
    print(call("read_file", path=p))
data = json.decode(call("list", {"dir": "."}))
print("items:", len(data["items"]))
`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "contents of a.go\ncontents of b.go\nitems: 3\n--- 3 tool call(s): list ×1, read_file ×2"
	if out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
	if inv.calls[0] != `read_file {"path":"a.go"}` || inv.calls[2] != `list {"dir":"."}` {
		t.Fatalf("calls = %q", inv.calls)
	}
}

// try_call lets a script handle a failure; call stops it, and the result still
// carries what it printed before and says which call stopped it.
func TestScriptFailuresAreHandledOrStopTheScript(t *testing.T) {
	out, err := runWith(t, &fakeInvoker{}, `
r = try_call("bash", command="go test ./...")
print("ok" if r.ok else "failed: " + r.error)
`)
	if err != nil || !strings.HasPrefix(out, "failed: tool call failed: bash: exit status 1") {
		t.Fatalf("try_call: out = %q, err = %v", out, err)
	}
	out, err = runWith(t, &fakeInvoker{}, `
print("before")
call("bash", command="go test ./...")
print("after")
`)
	if !errors.Is(err, ErrScriptFailed) || !strings.Contains(err.Error(), "bash: exit status 1") {
		t.Fatalf("call failure: err = %v", err)
	}
	if !strings.HasPrefix(out, "before\n") || strings.Contains(out, "after") {
		t.Fatalf("call failure: out = %q", out)
	}
}

func TestScriptIsBounded(t *testing.T) {
	if _, err := runWith(t, &fakeInvoker{}, "while True:\n    pass\n"); !errors.Is(err, ErrScriptFailed) {
		t.Fatalf("an endless loop ran to %v", err)
	}
	inv := &fakeInvoker{}
	_, err := runWith(t, inv, "for i in range(100):\n    call(\"read_file\", path=str(i))\n")
	if !errors.Is(err, ErrScriptFailed) || len(inv.calls) != maxCalls {
		t.Fatalf("calls = %d, err = %v; want the script stopped at %d", len(inv.calls), err, maxCalls)
	}
	if _, err := runWith(t, &fakeInvoker{}, `call("run_script", script="print(1)")`); !errors.Is(err, ErrScriptFailed) || !strings.Contains(err.Error(), ErrNested.Error()) {
		t.Fatalf("a script called run_script: %v", err)
	}
}

func TestScriptRefusesWithoutAnInvokerOrInsideAScript(t *testing.T) {
	if _, err := runWith(t, nil, "print(1)"); !errors.Is(err, ErrNoInvoker) {
		t.Fatalf("err = %v, want ErrNoInvoker", err)
	}
	ctx := tool.MarkNested(tool.WithInvoker(t.Context(), &fakeInvoker{}))
	if _, err := New().Execute(ctx, json.RawMessage(`{"script":"print(1)"}`)); !errors.Is(err, ErrNested) {
		t.Fatalf("err = %v, want ErrNested", err)
	}
}

// A cancelled turn stops the interpreter rather than waiting for the script.
func TestScriptStopsWhenTheTurnIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(tool.WithInvoker(t.Context(), &fakeInvoker{}))
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	start := time.Now()
	_, err := New().Execute(ctx, json.RawMessage(`{"script":"x = 0\nfor i in range(1000000000):\n    x += 1\n"}`))
	if !errors.Is(err, ErrScriptFailed) || time.Since(start) > 5*time.Second {
		t.Fatalf("err = %v after %v", err, time.Since(start))
	}
}
