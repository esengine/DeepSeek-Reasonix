package script

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
)

// batchInvoker answers like fakeInvoker and also carries result references.
type batchInvoker struct {
	fakeInvoker
	batches [][]string
}

func (b *batchInvoker) InvokeResult(ctx context.Context, c tool.NestedCall) tool.NestedResult {
	out, err := b.Invoke(ctx, c.Name, c.Args)
	ref := ""
	if c.Name == "mcp" {
		ref = "tool-result:" + strings.Repeat("a", 64)
	}
	return tool.NestedResult{Output: out, ResultRef: ref, Err: err}
}

func (b *batchInvoker) InvokeMany(ctx context.Context, calls []tool.NestedCall) []tool.NestedResult {
	names := make([]string, len(calls))
	out := make([]tool.NestedResult, len(calls))
	for i, c := range calls {
		names[i] = c.Name
		out[i] = b.InvokeResult(ctx, c)
	}
	b.batches = append(b.batches, names)
	return out
}

func TestTryCallCarriesTheRetainedResultReference(t *testing.T) {
	out, err := runWith(t, &batchInvoker{}, `
r = try_call("mcp", prompt="shot 1")
print(r.ok, r.result_ref)
`)
	if err != nil || !strings.HasPrefix(out, "True tool-result:aaaa") {
		t.Fatalf("out %q, err %v", out, err)
	}
}

func TestCallManyHandsOneBatchToTheHostAndKeepsOrder(t *testing.T) {
	inv := &batchInvoker{}
	out, err := runWith(t, inv, `
rs = call_many([{"name": "read_file", "args": {"path": "a"}}, {"name": "bash", "args": {"command": "x"}}, {"name": "mcp", "args": {}}])
for r in rs:
    print(r.ok, r.output, r.result_ref != "")
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.batches) != 1 || strings.Join(inv.batches[0], ",") != "read_file,bash,mcp" {
		t.Fatalf("batches %v", inv.batches)
	}
	want := "True contents of a False\nFalse exit status 1 False\nTrue {\"items\":[1,2,3]} True\n--- 3 tool call(s): bash ×1, mcp ×1, read_file ×1"
	if out != want {
		t.Fatalf("out = %q\nwant %q", out, want)
	}
}

func TestCallManyCountsAgainstTheCallLimitBeforeRunning(t *testing.T) {
	inv := &batchInvoker{}
	_, err := runWith(t, inv, `call_many([{"name": "read_file", "args": {"path": str(i)}} for i in range(65)])`)
	if err == nil || len(inv.batches) != 0 {
		t.Fatalf("a batch over the limit ran: %v %v", err, inv.batches)
	}
}

func TestCallManyNeedsAHostThatSchedulesBatches(t *testing.T) {
	_, err := runWith(t, &fakeInvoker{}, `call_many([{"name": "read_file", "args": {}}])`)
	if err == nil || !strings.Contains(err.Error(), "call_many") {
		t.Fatalf("plain invoker accepted a batch: %v", err)
	}
	if _, jerr := json.Marshal(err.Error()); jerr != nil {
		t.Fatal(jerr)
	}
}
