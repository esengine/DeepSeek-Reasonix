package agent

import (
	"context"
	"testing"

	"reasonix/internal/state/sessionstore"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/instruction"
)

type projectCheckProbeSink struct {
	probes []event.ProjectCheckProbe
}

func (s *projectCheckProbeSink) Emit(event.Event) {}

func (s *projectCheckProbeSink) RecordProjectCheckProbe(p event.ProjectCheckProbe) {
	s.probes = append(s.probes, p)
}

// Outside a delivery scope the checkpoint is left over from whatever restored
// it, so its baseline is not this turn's to owe.
func TestProjectCheckBaselineIgnoredOutsideItsScope(t *testing.T) {
	const began, replacement = "go test ./...", "go test ./internal/foo"
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false, writesPaths: true})
	reg.Add(fakeTool{name: "bash", readOnly: false})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "write_file", `{"path":"changed.go","content":"package main"}`),
			{Type: provider.ChunkDone},
		},
		{
			toolCallChunk("c2", "bash", `{"command":"`+replacement+`"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{
		ProjectChecks: []instruction.VerifyCheck{{Command: replacement, SourcePath: "AGENTS.md", Line: 3}},
	}, &projectCheckProbeSink{})
	a.RestoreDeliveryCheckpoint(evidence.DeliveryCheckpoint{ScopeID: "goal-elsewhere", BaselineChecks: []string{began}})
	if err := a.Run(context.Background(), "edit"); err != nil {
		t.Fatalf("unscoped turn err = %v, want a restored goal's baseline not to bind it", err)
	}
}
