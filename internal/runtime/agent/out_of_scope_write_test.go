package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/sessionstore"
)

// outsideWriter stands in for a tool whose call succeeds and names the file it
// wrote, whatever that file is. It is the shape of a turn that put something on
// the desktop while its workspace was somewhere else.
type outsideWriter struct{}

func (outsideWriter) Name() string        { return "write_file" }
func (outsideWriter) Description() string { return "write a file" }
func (outsideWriter) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (outsideWriter) ReadOnly() bool         { return false }
func (outsideWriter) WritesNamedPaths() bool { return true }
func (outsideWriter) Execute(context.Context, json.RawMessage) (string, error) {
	return "wrote", nil
}

// A turn that read, then wrote a file the host cannot place in the work
// product, then read again, and closed on one short sentence: nothing in the
// evidence says the request finished, and nothing says it did not. Reporting
// nothing at all is the failure this pins — the person reads an ordinary ending
// and finds the work half done later.
func TestTurnWhoseWritesFallOutsideTheWorkspaceIsNotReportedAsFinished(t *testing.T) {
	workspace := t.TempDir()
	// Deliberately not t.TempDir(): a temp directory is exempt from this rule,
	// and the case being pinned is a write to a place the person meant to keep.
	// The volume comes from the host, so this is absolute on Windows too.
	target := filepath.Join(filepath.VolumeName(workspace)+string(filepath.Separator), "outside-watch", "Reasonix CLI.desktop")

	reg := tool.NewRegistry()
	reg.Add(readProbe{})
	reg.Add(outsideWriter{})
	prov := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "r1", Name: "read_file", Arguments: `{"path":"a.go"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "w1", Name: "write_file", Arguments: fmt.Sprintf(`{"path":%q}`, target)}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "r2", Name: "read_file", Arguments: `{"path":"b.go"}`}}},
		testutil.Turn{Text: "改用更稳妥的写法。"},
	)
	a := New(prov, reg, sessionstore.NewSession("sys"), Options{WriteWorkspaceRoot: workspace, CheckTargetAccess: allowTestFileAccess}, event.Discard)

	if err := a.Run(context.Background(), "把快捷方式放到桌面，然后核实它能启动"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	receipt := a.CompletionReceipt()
	if receipt == nil {
		t.Fatal("a turn that wrote outside its workspace ended with no completion record at all")
	}
	if receipt.Verdict == "done" {
		t.Fatalf("verdict = done for a turn the host cannot call finished: %+v", receipt)
	}
	if len(receipt.Gaps) == 0 {
		t.Fatalf("receipt carries no gap; a write the host cannot attribute reads as silence: %+v", receipt)
	}
	if want := filepath.Base(evidence.NormalizePath(target)); !strings.Contains(receipt.Gaps[0].Detail, want) {
		t.Fatalf("gap detail = %q, want it to name the file the host could not place", receipt.Gaps[0].Detail)
	}
}

// The other side of the same rule: a turn that changed nothing outside the work
// product, or changed nothing at all, still ends without a caveat. An absent
// caveat has to mean something, or the one that matters stops being read.
func TestTurnThatStaysInsideTheWorkspaceKeepsItsQuietEnding(t *testing.T) {
	workspace := t.TempDir()

	reg := tool.NewRegistry()
	reg.Add(readProbe{})
	reg.Add(outsideWriter{})
	prov := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "r1", Name: "read_file", Arguments: `{"path":"a.go"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "w1", Name: "write_file", Arguments: fmt.Sprintf(`{"path":%q}`, filepath.Join(workspace, "a.go"))}}},
		testutil.Turn{Text: "已写入 a.go。"},
	)
	a := New(prov, reg, sessionstore.NewSession("sys"), Options{WriteWorkspaceRoot: workspace}, event.Discard)

	if err := a.Run(context.Background(), "写 a.go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	receipt := a.CompletionReceipt()
	if receipt == nil {
		return
	}
	for _, gap := range receipt.Gaps {
		if strings.Contains(gap.Detail, "outside") {
			t.Fatalf("a write inside the workspace was reported as outside observation: %+v", gap)
		}
	}
}
