package delegation

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

func TestTaskExplicitWritePathsCannotBypassBoundaryThroughCapabilityProxy(t *testing.T) {
	root := testenv.TempDir(t)
	var writerCalls int32
	target := parallelResolvedWriterTarget{calls: &writerCalls}
	parent := tool.NewRegistry()
	parent.Add(readOnlyBoundaryProxy{resolved: tool.ResolvedCall{
		ProxyAction: "call",
		TargetName:  target.Name(),
		Target:      target,
		ReadOnly:    false,
		Args:        json.RawMessage(`{}`),
	}})
	task := newTestTaskTool(t, proxyWriterCallingProvider{}, parent, "sys", "", "", nil).
		WithTranscripts(NewSubagentStore(testenv.TempDir(t)), root, "base-model", "base-effort")
	out, err := task.Execute(testTaskContext(), json.RawMessage(`{
		"prompt":"attempt dynamic writer",
		"write_paths":["frontend"]
	}`))
	if err != nil {
		t.Fatalf("task Execute: %v\n%s", err, out)
	}
	if writerCalls != 0 {
		t.Fatalf("path-bound task executed MCP writer %d times, want zero", writerCalls)
	}
	if !strings.Contains(out, "writer blocked") {
		t.Fatalf("task did not recover after host boundary block:\n%s", out)
	}
}
