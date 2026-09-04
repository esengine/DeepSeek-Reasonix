package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
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
type scripted struct {
	turnCalls atomic.Int64
	// retodo latches the one reply that replaces the task list. The sentinel
	// stays in history after the call, so without it every later round would
	// rewrite the list again.
	retodo atomic.Bool
}

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
		return append(todoCall(firstTodos()), done())
	}
	if hasTool(req.Tools, "todo_write") && requestMentions(req, retodoSentinel) && !s.retodo.Swap(true) {
		return append(todoCall(secondTodos()), done())
	}
	return append(text(echoMarker(req)+turnBody), done())
}

// A turn is tagged twice: the user side by markerPattern, the assistant reply
// by echoPattern. A fold folds assistant work first, so a one-sided tag would
// let the surviving user turn hide the reply that was dropped beside it.
var (
	markerPattern = regexp.MustCompile(`PROBE-MARK-\d+`)
	echoPattern   = regexp.MustCompile(`PROBE-ECHO-\d+`)
)

func marker(n int) string { return fmt.Sprintf("PROBE-MARK-%03d", n) }

// markersIn returns the tags a message set still shows, in order.
func markersIn(texts []string, pat *regexp.Regexp) []string {
	var out []string
	for _, t := range texts {
		out = append(out, pat.FindAllString(t, -1)...)
	}
	return out
}

// echoMarker answers a turn with the assistant-side tag for the same number.
func echoMarker(req provider.Request) string {
	for _, m := range slices.Backward(req.Messages) {
		found := markerPattern.FindAllString(m.Content, -1)
		if len(found) == 0 {
			continue
		}
		return "PROBE-ECHO-" + found[len(found)-1][len("PROBE-MARK-"):] + " reply. "
	}
	return ""
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
func todoCall(todos []map[string]any) []provider.Chunk {
	args, _ := json.Marshal(map[string]any{"todos": todos})
	return []provider.Chunk{{
		Type:     provider.ChunkToolCall,
		ToolCall: &provider.ToolCall{ID: "probe_todo_" + strconv.Itoa(len(todos)), Name: "todo_write", Arguments: string(args)},
	}}
}

// retodoSentinel asks the scripted reply to replace the task list, so a probe
// can move host step identity from one set of ids to another.
const retodoSentinel = "PROBE-RETODO"

func requestMentions(req provider.Request, needle string) bool {
	for _, m := range slices.Backward(req.Messages) {
		if strings.Contains(m.Content, needle) {
			return true
		}
	}
	return false
}

func firstTodos() []map[string]any {
	return []map[string]any{
		{"content": "Establish probe state", "status": "in_progress", "activeForm": "Establishing probe state", "step_id": "probe_step_01"},
		{"content": "Cross the process boundary", "status": "pending", "step_id": "probe_step_02"},
		{"content": "Report the resurrection matrix", "status": "pending", "step_id": "probe_step_03"},
	}
}

// secondTodos replaces the pending work while the in_progress item stays: the
// host refuses to drop an item that is in flight, so this is what a list
// rewrite actually looks like. The two new ids are what a stale note misses.
func secondTodos() []map[string]any {
	return []map[string]any{
		{"content": "Establish probe state", "status": "in_progress", "activeForm": "Establishing probe state", "step_id": "probe_step_01"},
		{"content": "Split the frozen body", "status": "pending", "step_id": "probe_step_21"},
		{"content": "Hold the ordering contract", "status": "pending", "step_id": "probe_step_22"},
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
