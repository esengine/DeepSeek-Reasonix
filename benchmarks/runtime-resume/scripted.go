package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"reasonix/internal/provider"
)

// newScripted builds the provider with its synchronisation channels open. The
// scheduler arms coordinate through them; every other arm never reads them.
func newScripted(arm string) *scripted {
	return &scripted{arm: arm, held: make(chan struct{}), release: make(chan struct{})}
}

// probeModelRef is both the catalog ref and the model name boot is given, so
// syntheticEntryFromResolver matches it without touching any config file.
const probeModelRef = "probe/scripted"

// scripted answers completions from a fixed script so the construct phase can
// establish host state without a network call. A request carrying no tools is a
// host-internal completion (the compaction summary); everything else is a turn.
type scripted struct {
	// arm is which run this provider is scripting. Only the fan-out arms read
	// it: the pair fleet is the same dispatch either way, and the arm decides
	// whether its second item finishes or is still executing at death.
	arm       string
	turnCalls atomic.Int64
	// retodo latches the one reply that replaces the task list. The sentinel
	// stays in history after the call, so without it every later round would
	// rewrite the list again.
	retodo atomic.Bool
	// asked latches the one reply that opens a question and, in the same round,
	// tries to write. Calling ask ends the round, so the write is the effect a
	// decision is holding back.
	asked atomic.Bool
	// The fan-out latches. A fleet's aggregate returns into the same round that
	// dispatched it, so without these the reply that follows would read its own
	// sentinel off the still-current user turn and dispatch the fleet again.
	warmed atomic.Bool
	paired atomic.Bool
	mixed  atomic.Bool
	// The scheduler arms dispatch two fleets and must not race them. held is
	// closed by the holder's own child, which only runs once its slot is
	// granted; release lets the arm free capacity while a refusal stands.
	holder  atomic.Bool
	refused atomic.Bool
	holding sync.Once
	held    chan struct{}
	release chan struct{}
}

func (s *scripted) Name() string { return "probe" }

func (s *scripted) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 8)
	go func() {
		defer close(ch)
		for _, c := range s.script(ctx, req) {
			select {
			case ch <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (s *scripted) script(ctx context.Context, req provider.Request) []provider.Chunk {
	if len(req.Tools) == 0 {
		return append(text(summaryBody), done())
	}
	// A delegated run answers first: the parent's transcript quotes every fleet
	// argument, so any later branch that scanned the conversation would make
	// the parent answer as one of its own children.
	if sentinel := childSentinel(req); sentinel != "" {
		return s.childScript(ctx, sentinel)
	}
	if chunks, ok := s.fanOut(req); ok {
		return chunks
	}
	if s.turnCalls.Add(1) == 1 && hasTool(req.Tools, "todo_write") {
		return append(todoCall(firstTodos()), done())
	}
	if hasTool(req.Tools, "todo_write") && requestMentions(req, retodoSentinel) && !s.retodo.Swap(true) {
		return append(todoCall(secondTodos()), done())
	}
	if requestMentions(req, askSentinel) && !s.asked.Swap(true) {
		return append(askAndWrite(), done())
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

// askSentinel asks for a question the host must put to a person, alongside the
// write that question is holding back.
const (
	askSentinel      = "PROBE-ASK"
	deferredEffect   = "probe-deferred-effect.txt"
	deferredContents = "this write was not held back by the decision"
)

// askAndWrite opens a question and asks to write in the same round. The round
// ends at the question, so the write must not run — not while the answer is
// pending, and not because a later process reloaded the session.
func askAndWrite() []provider.Chunk {
	ask, _ := json.Marshal(map[string]any{"questions": []map[string]any{{
		"header":   "Boundary",
		"question": "Which side of the fold should the probe measure?",
		"reason":   "user_decision",
		"options": []map[string]any{
			{"label": "Below the fold", "description": "removes folded history"},
			{"label": "Above the fold", "description": "leaves coverage intact"},
		},
	}}})
	write, _ := json.Marshal(map[string]string{"path": deferredEffect, "content": deferredContents})
	return []provider.Chunk{
		{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "probe_ask_1", Name: "ask", Arguments: string(ask)}},
		{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "probe_write_1", Name: "write_file", Arguments: string(write)}},
	}
}

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

func resolverFor(prov *scripted) *provider.StaticResolver {
	return &provider.StaticResolver{
		Descriptors: []provider.Descriptor{{
			Ref: probeModelRef, DisplayName: "probe", Model: "scripted",
			ContextWindow: 128_000, Tools: true,
		}},
		Providers: map[string]provider.Provider{probeModelRef: prov},
	}
}

func errUnexpected(what string, got any) error {
	return fmt.Errorf("probe could not establish %s (got %v); the arm measures nothing and is reported invalid", what, got)
}
