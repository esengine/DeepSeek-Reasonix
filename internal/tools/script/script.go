// Package script is the run_script tool: the model writes a short Starlark
// program that calls tools, loops over their results and prints what it
// needs, so several dependent steps cost one round trip and only the printed
// summary enters the conversation. Every call inside the script goes through
// the agent's own invoker, so it is approved, hooked and observed exactly as
// if the model had made it; the interpreter itself can reach nothing else.
package script

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"

	starjson "go.starlark.net/lib/json"

	"reasonix/internal/contract/tool"
)

// Name is the tool name the model calls.
const Name = "run_script"

const (
	maxSteps       = 20_000_000
	maxCalls       = 64
	maxPrintBytes  = 64 * 1024
	maxScriptBytes = 32 * 1024
)

var (
	// ErrNoInvoker is a script run where no agent offers tool calls.
	ErrNoInvoker = errors.New("run_script: no tool invoker is bound to this call")
	// ErrNested is run_script called from inside a script.
	ErrNested = errors.New("run_script cannot be called from inside a script")
	// ErrScriptFailed is a script that stopped on an error; the result carries
	// what it printed first.
	ErrScriptFailed = errors.New("script failed")
)

var fileOptions = &syntax.FileOptions{Set: true, While: true, TopLevelControl: true, GlobalReassign: true}

type runScript struct{}

// New returns the run_script tool.
func New() tool.Tool { return runScript{} }

func (runScript) Name() string { return Name }

func (runScript) Description() string {
	return "Run a short Starlark (Python-like) script that calls tools, so several dependent steps take one round trip and only what the script prints comes back. " +
		"Use it to read or search many files and keep only what matters, run a command and act on its output, or repeat one tool over a list. " +
		"In the script: call(name, **args) runs a tool and returns its output as a string, stopping the script if the call fails; " +
		"try_call(name, **args) returns a struct with ok, output and error instead of stopping; json.decode/json.encode convert JSON; print(...) writes the result. " +
		"Every call is checked and approved exactly as if you made it directly. The script has no file, network or clock access of its own, " +
		"at most 64 tool calls, and cannot call run_script. Prefer ordinary calls for one or two independent steps."
}

func (runScript) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"script":{"type":"string","description":"Starlark source. Example: for p in [\"a.go\", \"b.go\"]:\n    src = call(\"read_file\", path=p)\n    print(p, len(src.splitlines()))"}},"required":["script"],"additionalProperties":false}`)
}

func (runScript) ReadOnly() bool { return false }

func (runScript) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Script string `json:"script"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.Script) == "" {
		return "", errors.New("invalid args: script is required")
	}
	if len(p.Script) > maxScriptBytes {
		return "", fmt.Errorf("invalid args: script is over %d bytes", maxScriptBytes)
	}
	if tool.IsNested(ctx) {
		return "", ErrNested
	}
	inv, ok := tool.InvokerFrom(ctx)
	if !ok {
		return "", ErrNoInvoker
	}
	r := &run{ctx: ctx, inv: inv, counts: map[string]int{}}
	return r.exec(p.Script)
}

// run is one script execution: what it printed and which tools it called.
type run struct {
	ctx     context.Context
	inv     tool.Invoker
	out     strings.Builder
	clipped bool
	calls   int
	counts  map[string]int
}

func (r *run) exec(src string) (string, error) {
	thread := &starlark.Thread{Name: Name, Print: r.print}
	thread.SetMaxExecutionSteps(maxSteps)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-r.ctx.Done():
			thread.Cancel("the turn was cancelled")
		case <-done:
		}
	}()
	predeclared := starlark.StringDict{
		"call":     starlark.NewBuiltin("call", r.call),
		"try_call": starlark.NewBuiltin("try_call", r.tryCall),
		"json":     starjson.Module,
		"struct":   starlark.NewBuiltin("struct", starlarkstruct.Make),
	}
	_, err := starlark.ExecFileOptions(fileOptions, thread, "script.star", src, predeclared)
	result := r.result()
	if err != nil {
		msg := err.Error()
		var evalErr *starlark.EvalError
		if errors.As(err, &evalErr) {
			msg = evalErr.Backtrace()
		}
		return result, fmt.Errorf("%w: %s", ErrScriptFailed, msg)
	}
	return result, nil
}

func (r *run) print(_ *starlark.Thread, msg string) {
	if r.clipped {
		return
	}
	if r.out.Len()+len(msg)+1 > maxPrintBytes {
		r.out.WriteString("[… output past 64 KiB left out …]\n")
		r.clipped = true
		return
	}
	r.out.WriteString(msg + "\n")
}

// result is what the script printed, then one line naming the calls it made.
func (r *run) result() string {
	var b strings.Builder
	b.WriteString(r.out.String())
	if r.out.Len() == 0 {
		b.WriteString("(the script printed nothing)\n")
	}
	names := make([]string, 0, len(r.counts))
	for name := range r.counts {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s ×%d", name, r.counts[name]))
	}
	fmt.Fprintf(&b, "--- %d tool call(s)", r.calls)
	if len(parts) > 0 {
		b.WriteString(": " + strings.Join(parts, ", "))
	}
	return b.String()
}

func (r *run) call(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	out, err := r.invoke(thread, fn, args, kwargs)
	if err != nil {
		return nil, err
	}
	return starlark.String(out), nil
}

func (r *run) tryCall(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	out, err := r.invoke(thread, fn, args, kwargs)
	errText := ""
	if err != nil {
		if !errors.Is(err, tool.ErrNestedCallFailed) {
			return nil, err
		}
		errText = err.Error()
	}
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"ok": starlark.Bool(err == nil), "output": starlark.String(out), "error": starlark.String(errText),
	}), nil
}

// invoke reads call(name, **args) or call(name, {args}) and runs it. A failed
// call returns its output with an error wrapping tool.ErrNestedCallFailed.
func (r *run) invoke(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (string, error) {
	if len(args) < 1 || len(args) > 2 {
		return "", fmt.Errorf("%s: want a tool name, then keyword arguments or one dict", fn.Name())
	}
	name, ok := starlark.AsString(args[0])
	if !ok || strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("%s: the tool name must be a string", fn.Name())
	}
	if name == Name {
		return "", ErrNested
	}
	var payload starlark.Value
	switch {
	case len(args) == 2 && len(kwargs) > 0:
		return "", fmt.Errorf("%s: pass arguments as keywords or as one dict, not both", fn.Name())
	case len(args) == 2:
		payload = args[1]
	default:
		d := starlark.NewDict(len(kwargs))
		for _, kv := range kwargs {
			if err := d.SetKey(kv[0], kv[1]); err != nil {
				return "", err
			}
		}
		payload = d
	}
	encoded, err := starlark.Call(thread, starjson.Module.Members["encode"], starlark.Tuple{payload}, nil)
	if err != nil {
		return "", fmt.Errorf("%s(%q): arguments are not JSON: %w", fn.Name(), name, err)
	}
	if r.calls >= maxCalls {
		return "", fmt.Errorf("%s: the script reached its limit of %d tool calls", fn.Name(), maxCalls)
	}
	r.calls++
	r.counts[name]++
	raw, _ := starlark.AsString(encoded)
	return r.inv.Invoke(r.ctx, name, json.RawMessage(raw))
}
