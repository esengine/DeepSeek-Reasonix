// capture.go — what the agent actually hands a provider.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reasonix/internal/state/sessionstore"
	"strings"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	_ "reasonix/internal/tools/builtin"
)

// A projection is what the agent believes; a provider.Request is what the model
// is told. Rendering, sanitization and tool-schema construction sit between
// them, so the arm's contract is checked on the request.

type captureProvider struct {
	requests []provider.Request
}

func (p *captureProvider) Name() string { return "contextbench-capture" }

// Stream records the request and answers with a clean terminal response. An
// empty answer would buy an empty-final retry and a second request, which is
// not the one round this is here to inspect.
func (p *captureProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, cloneRequest(req))
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "contextbench preflight complete"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func cloneRequest(req provider.Request) provider.Request {
	out := req
	out.Messages = append([]provider.Message(nil), req.Messages...)
	out.Tools = append([]provider.ToolSchema(nil), req.Tools...)
	return out
}

// captureRequest rebuilds the session the way a resume does — transcript from
// disk, projection from its sidecar, the shipped tool surface at this arm —
// and returns the request the first round hands the provider.
func captureRequest(f builtFixture, arm ablation.Set, prompt string) (provider.Request, error) {
	sess, err := sessionstore.LoadSession(f.Path)
	if err != nil {
		return provider.Request{}, fmt.Errorf("load session: %w", err)
	}
	reg := tool.NewRegistry()
	for _, t := range tool.Builtins() {
		reg.Add(t)
	}
	boot.ApplyUnifiedProviderToolSurface(reg, false, arm)

	cap := &captureProvider{}
	a := agent.New(cap, reg, sess, agent.Options{
		ContextWindow: fixtureWindow, CompactRatio: 0.5, RecentKeep: 2,
		SessionPath: f.Path, KeepPolicy: agent.KeepErrors, Ablation: arm,
	}, event.Discard)
	a.LoadProjectionSidecar(f.Path)

	if err := a.Run(context.Background(), prompt); err != nil {
		return provider.Request{}, fmt.Errorf("run: %w", err)
	}
	// The turn's own request is the one carrying tools. A fold on the way in
	// asks the same provider for a digest, and that request has none — taking
	// the first request blindly would inspect the summarizer's prompt.
	for _, req := range cap.requests {
		if len(req.Tools) > 0 {
			return req, nil
		}
	}
	return provider.Request{}, fmt.Errorf("no request carried tools (%d captured)", len(cap.requests))
}

// surfaceField is one place a model could read a string, named so a leak
// report says where rather than only that.
type surfaceField struct {
	Path string
	Text string
}

// providerSurface enumerates every provider-visible string in a request.
// Enumerated rather than marshalled whole: a Message also carries local-only
// fields, and scanning those would fail on bytes no model ever sees.
func providerSurface(req provider.Request) []surfaceField {
	var out []surfaceField
	add := func(path, text string) {
		if strings.TrimSpace(text) != "" {
			out = append(out, surfaceField{Path: path, Text: text})
		}
	}
	for i, m := range req.Messages {
		add(fmt.Sprintf("messages[%d].content", i), m.Content)
		add(fmt.Sprintf("messages[%d].reasoning_content", i), m.ReasoningContent)
		add(fmt.Sprintf("messages[%d].name", i), m.Name)
		for j, tc := range m.ToolCalls {
			add(fmt.Sprintf("messages[%d].tool_calls[%d].name", i, j), tc.Name)
			add(fmt.Sprintf("messages[%d].tool_calls[%d].arguments", i, j), tc.Arguments)
		}
		for j, item := range m.ResponsesItems {
			add(fmt.Sprintf("messages[%d].responses_items[%d]", i, j), string(item))
		}
	}
	for i, t := range req.Tools {
		add(fmt.Sprintf("tools[%d].name", i), t.Name)
		add(fmt.Sprintf("tools[%d].description", i), t.Description)
		if len(t.Parameters) > 0 {
			b, err := json.Marshal(t.Parameters)
			if err == nil {
				add(fmt.Sprintf("tools[%d].parameters", i), string(b))
			}
		}
	}
	return out
}

// findInSurface reports every place a marker is readable.
func findInSurface(req provider.Request, marker string) []string {
	var hits []string
	for _, f := range providerSurface(req) {
		if strings.Contains(f.Text, marker) {
			hits = append(hits, f.Path)
		}
	}
	return hits
}

// recallToolSchema returns the recall tool as the provider sees it.
func recallToolSchema(req provider.Request) (provider.ToolSchema, bool) {
	for _, t := range req.Tools {
		if t.Name == "recall" {
			return t, true
		}
	}
	return provider.ToolSchema{}, false
}

// localOnlyLeaks reports host-side fields that should never survive the
// provider boundary. A hit here is a boundary regression, marker or no marker.
func localOnlyLeaks(req provider.Request) []string {
	var out []string
	for i, m := range req.Messages {
		switch {
		case m.RawContent != "":
			out = append(out, fmt.Sprintf("messages[%d].raw_content", i))
		case m.LocalOnly:
			out = append(out, fmt.Sprintf("messages[%d].local_only", i))
		case m.DecisionReceipt != nil || len(m.DecisionReceipts) > 0:
			out = append(out, fmt.Sprintf("messages[%d].decision_receipt", i))
		case m.ToolExecution != nil:
			out = append(out, fmt.Sprintf("messages[%d].tool_execution", i))
		case len(m.MemoryCitations) > 0:
			out = append(out, fmt.Sprintf("messages[%d].memory_citations", i))
		}
	}
	return out
}

// searchWording is what a search-off arm must never be told. The arm exists to
// reproduce the runtime before search; an arm offered a capability it does not
// have is a different experiment.
var searchWording = []string{"recall with a query", "search the whole folded region", "query still finds"}
