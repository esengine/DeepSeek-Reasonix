package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"reasonix/internal/provider"
)

// probeModelRef is both the catalog ref and the model name boot is given, so
// syntheticEntryFromResolver matches it without touching any config file.
const probeModelRef = "probe/scripted"

// scripted answers completions from a fixed script so the construct phase can
// establish host state without a network call. A request carrying no tools is a
// host-internal completion (the compaction summary); everything else is a turn.
type scripted struct{ turnCalls atomic.Int64 }

func (s *scripted) Name() string { return "probe" }

func (s *scripted) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 8)
	go func() {
		defer close(ch)
		for _, c := range s.script(req) {
			select {
			case ch <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (s *scripted) script(req provider.Request) []provider.Chunk {
	if len(req.Tools) == 0 {
		return append(text(summaryBody), done())
	}
	if s.turnCalls.Add(1) == 1 && hasTool(req.Tools, "todo_write") {
		return append(todoCall(), done())
	}
	return append(text(turnBody), done())
}

func hasTool(schemas []provider.ToolSchema, name string) bool {
	for _, t := range schemas {
		if t.Name == name {
			return true
		}
	}
	return false
}

func text(s string) []provider.Chunk {
	return []provider.Chunk{{Type: provider.ChunkText, Text: s}}
}

func done() provider.Chunk { return provider.Chunk{Type: provider.ChunkDone} }

// todoCall writes the list the resume phase looks for. The step ids are the
// point: they are the host identity a completion has to cite. A first write
// may not open with a completed item — the host rejects that shape, and a
// rejected call would leave the row unmeasured rather than answered.
func todoCall() []provider.Chunk {
	args, _ := json.Marshal(map[string]any{"todos": probeTodos()})
	return []provider.Chunk{{
		Type:     provider.ChunkToolCall,
		ToolCall: &provider.ToolCall{ID: "probe_todo_1", Name: "todo_write", Arguments: string(args)},
	}}
}

func probeTodos() []map[string]any {
	return []map[string]any{
		{"content": "Establish probe state", "status": "in_progress", "activeForm": "Establishing probe state", "step_id": "probe_step_01"},
		{"content": "Cross the process boundary", "status": "pending", "step_id": "probe_step_02"},
		{"content": "Report the resurrection matrix", "status": "pending", "step_id": "probe_step_03"},
	}
}

// turnBody is fat on purpose: the fold keeps a recent verbatim tail, so thin
// replies leave no region between the pinned head and that tail.
var turnBody = "Probe state established. " + strings.Repeat("The probe records deterministic filler so the fold has material to work on. ", 60)

var summaryBody = strings.Join([]string{
	"## Standing facts & constraints",
	"The runtime-resume probe is measuring what survives a process boundary.",
	"",
	"## Goal",
	"Establish host state, exit, and observe what a new process can still prove.",
}, "\n")

func newResolver() *provider.StaticResolver {
	return &provider.StaticResolver{
		Descriptors: []provider.Descriptor{{
			Ref: probeModelRef, DisplayName: "probe", Model: "scripted",
			ContextWindow: 128_000, Tools: true,
		}},
		Providers: map[string]provider.Provider{probeModelRef: &scripted{}},
	}
}

func errUnexpected(what string, got any) error {
	return fmt.Errorf("probe could not establish %s (got %v); the arm measures nothing and is reported invalid", what, got)
}
