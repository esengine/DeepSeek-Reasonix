package agent

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type originProbe struct {
	event.Sink
	got []evidence.DelegationAudit
}

func (p *originProbe) RecordDelegationAudit(a evidence.DelegationAudit) { p.got = append(p.got, a) }

// runDelegationForOrigin spawns one real child through TaskTool that reads
// src/parser.go and src/scanner.go, and returns the audit the sink received.
func runDelegationForOrigin(t *testing.T, prompt string) evidence.DelegationAudit {
	t.Helper()
	probe := &originProbe{Sink: event.Discard}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("1", "read_file", `{"path":"src/parser.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("2", "read_file", `{"path":"src/scanner.go"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), t.TempDir(), "base", "high").
		WithScheduler(NewSubagentScheduler(4, 4))
	ctx := withCallContext(context.Background(), "call-1", probe, nil, false)
	args, err := json.Marshal(map[string]string{"prompt": prompt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := task.Execute(ctx, args); err != nil {
		t.Fatalf("task: %v", err)
	}
	if len(probe.got) != 1 {
		t.Fatalf("got %d audits through the TaskTool path, want 1", len(probe.got))
	}
	return probe.got[0]
}

// A delegation that hands over no location leaves the child to find every file
// it reads. This is the number blind delegation would have to hold at zero.
func TestDelegationAuditScoresBlindDelegationAsFullyDiscovered(t *testing.T) {
	audit := runDelegationForOrigin(t, "Determine why the failing test fails.")
	if audit.ParentNamedFiles != 0 {
		t.Errorf("ParentNamedFiles = %d, want 0", audit.ParentNamedFiles)
	}
	if audit.EvidencePaths != 2 || audit.DiscoveredPaths != 2 {
		t.Errorf("discovered %d/%d, want 2/2", audit.DiscoveredPaths, audit.EvidencePaths)
	}
}

// The parent's own text is the only channel that can leak a location, so a
// prompt naming one file must show up as a hint and cost the child credit for
// the evidence it did not have to find.
func TestDelegationAuditCreditsOnlyWhatTheChildFoundItself(t *testing.T) {
	audit := runDelegationForOrigin(t, "The bug is in src/parser.go — confirm it.")
	if audit.ParentNamedFiles != 1 {
		t.Errorf("ParentNamedFiles = %d, want 1", audit.ParentNamedFiles)
	}
	if audit.EvidencePaths != 2 || audit.DiscoveredPaths != 1 {
		t.Errorf("discovered %d/%d, want 1/2", audit.DiscoveredPaths, audit.EvidencePaths)
	}
}
