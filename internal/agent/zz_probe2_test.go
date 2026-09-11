package agent

import (
	"context"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type auditProbe2 struct {
	event.Sink
	got []evidence.DelegationAudit
}

func (p *auditProbe2) RecordDelegationAudit(a evidence.DelegationAudit) { p.got = append(p.got, a) }

func TestZZProbeThroughTaskTool(t *testing.T) {
	root := t.TempDir()
	probe := &auditProbe2{Sink: event.Discard}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("1", "read_file", `{"path":"a.go"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(NewSubagentScheduler(4, 4))
	ctx := withCallContext(context.Background(), "call-1", probe, nil, false)
	if _, err := task.Execute(ctx, []byte(`{"prompt":"inspect a.go"}`)); err != nil {
		t.Fatalf("task: %v", err)
	}
	t.Logf("audits через TaskTool: %d", len(probe.got))
	if len(probe.got) == 0 {
		t.Fatal("no audit through the TaskTool path")
	}
}
