package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// capturingProvider records the exact message list of every request it
// receives, marshaled at capture time, so tests can compare request bytes.
type capturingProvider struct {
	mu       sync.Mutex
	requests [][]byte
}

func (p *capturingProvider) Name() string { return "capturing" }

func (p *capturingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	b, err := json.Marshal(req.Messages)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.requests = append(p.requests, b)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	close(ch)
	return ch, nil
}

func (p *capturingProvider) lastRequestMessages(t *testing.T) []provider.Message {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		t.Fatal("provider captured no requests")
	}
	var msgs []provider.Message
	if err := json.Unmarshal(p.requests[len(p.requests)-1], &msgs); err != nil {
		t.Fatalf("unmarshal captured request: %v", err)
	}
	return msgs
}

func marshalMessages(t *testing.T, msgs []provider.Message) []byte {
	t.Helper()
	b, err := json.Marshal(msgs)
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}
	return b
}

// copySessionFiles clones a saved transcript (checkpoint anchor, event log,
// meta sidecar) to an independent path, so a rebind can load the state saved
// at that moment while the original controller keeps running and autosaving.
func copySessionFiles(t *testing.T, from, to string) {
	t.Helper()
	copied := false
	for _, suffix := range []string{"", ".events.jsonl", ".meta"} {
		b, err := os.ReadFile(from + suffix)
		if err != nil {
			continue
		}
		if err := os.WriteFile(to+suffix, b, 0o644); err != nil {
			t.Fatalf("copy session file %s: %v", suffix, err)
		}
		copied = true
	}
	if !copied {
		t.Fatalf("no session files found at %s", from)
	}
}

// TestRebindReproducesRequestBytes is the desktop-level byte-stability guard
// for the provider prefix cache. It builds the strongest comparison available:
// from ONE saved transcript, run the same follow-up turn twice — once on the
// original controller (no rebind, the provider-cache-warm baseline) and once
// after the desktop rebind path (agent.LoadSession + sessionWithFreshSystemPrompt
// + Resume on a freshly built controller, the shape of tabs.go's restore). The
// two requests must be byte-identical END TO END — system prompt, prior user
// AND assistant turns, and the composed follow-up. Any divergence means a
// desktop rebuild cold-starts the conversation's provider cache at 10x miss
// pricing (#2945, #5614).
func TestRebindReproducesRequestBytes(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	const systemPrompt = "SYSPROMPT stable bytes"

	prov := &capturingProvider{}
	exec := agent.New(prov, tool.NewRegistry(), agent.NewSession(systemPrompt), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Runner: exec, Executor: exec, SystemPrompt: systemPrompt, SessionDir: dir, SessionPath: path, Label: "test", Sink: event.Discard})

	if err := ctrl.RunTurn(context.Background(), "first question"); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if err := ctrl.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// Freeze the after-turn-one transcript on an independent path: the
	// baseline turn below autosaves onto the original path, and the rebind
	// must load the state as saved at this moment.
	rebindPath := filepath.Join(dir, "rebind.jsonl")
	copySessionFiles(t, path, rebindPath)

	// Baseline: the follow-up turn on the ORIGINAL controller — the exact
	// request an uninterrupted (cache-warm) session would send.
	if err := ctrl.RunTurn(context.Background(), "second question"); err != nil {
		t.Fatalf("baseline second turn: %v", err)
	}
	baseline := prov.lastRequestMessages(t)
	baselineBytes := marshalMessages(t, baseline)
	if len(baseline) < 4 {
		t.Fatalf("baseline request has %d messages, want system + first exchange + follow-up", len(baseline))
	}

	// Rebind from the transcript saved after turn one: a NEW controller
	// composes its (identical) system prompt, the persisted transcript is
	// loaded, the fresh prompt is swapped in, and the controller resumes —
	// then sends the same follow-up.
	prov2 := &capturingProvider{}
	exec2 := agent.New(prov2, tool.NewRegistry(), agent.NewSession(systemPrompt), agent.Options{}, event.Discard)
	ctrl2 := control.New(control.Options{Runner: exec2, Executor: exec2, SystemPrompt: systemPrompt, SessionDir: dir, SessionPath: rebindPath, Label: "test", Sink: event.Discard})
	loaded, err := agent.LoadSession(rebindPath)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	ctrl2.Resume(sessionWithFreshSystemPrompt(loaded, systemPromptFrom(ctrl2.History())), rebindPath)

	if err := ctrl2.RunTurn(context.Background(), "second question"); err != nil {
		t.Fatalf("post-rebind second turn: %v", err)
	}
	rebound := prov2.lastRequestMessages(t)
	reboundBytes := marshalMessages(t, rebound)
	if string(reboundBytes) != string(baselineBytes) {
		t.Fatalf("rebind changed the request bytes — the provider prefix cache is invalidated:\nbaseline: %s\nrebound:  %s", baselineBytes, reboundBytes)
	}
}

// TestRebindWithDriftedPromptBreaksRequestPrefix pins the failure mode the
// guard above protects against: when the freshly composed prompt differs from
// the one the transcript was recorded with, the swap rewrites the first
// message and the request diverges from the no-rebind baseline. If a future
// change moves the swap policy to keep the persisted prompt for resumed
// conversations, this test should be updated to assert the bytes survive
// instead.
func TestRebindWithDriftedPromptBreaksRequestPrefix(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	prov := &capturingProvider{}
	exec := agent.New(prov, tool.NewRegistry(), agent.NewSession("SYSPROMPT v1"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Runner: exec, Executor: exec, SystemPrompt: "SYSPROMPT v1", SessionDir: dir, SessionPath: path, Label: "test", Sink: event.Discard})
	if err := ctrl.RunTurn(context.Background(), "first question"); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if err := ctrl.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	rebindPath := filepath.Join(dir, "rebind.jsonl")
	copySessionFiles(t, path, rebindPath)
	if err := ctrl.RunTurn(context.Background(), "second question"); err != nil {
		t.Fatalf("baseline second turn: %v", err)
	}
	baseline := prov.lastRequestMessages(t)
	baselineBytes := marshalMessages(t, baseline)

	prov2 := &capturingProvider{}
	exec2 := agent.New(prov2, tool.NewRegistry(), agent.NewSession("SYSPROMPT v2 drifted"), agent.Options{}, event.Discard)
	ctrl2 := control.New(control.Options{Runner: exec2, Executor: exec2, SystemPrompt: "SYSPROMPT v2 drifted", SessionDir: dir, SessionPath: rebindPath, Label: "test", Sink: event.Discard})
	loaded, err := agent.LoadSession(rebindPath)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	ctrl2.Resume(sessionWithFreshSystemPrompt(loaded, systemPromptFrom(ctrl2.History())), rebindPath)
	if err := ctrl2.RunTurn(context.Background(), "second question"); err != nil {
		t.Fatalf("post-rebind turn: %v", err)
	}
	rebound := prov2.lastRequestMessages(t)
	if string(marshalMessages(t, rebound)) == string(baselineBytes) {
		t.Fatal("drifted prompt unexpectedly reproduced the baseline request — the swap policy changed; update these guards")
	}
	if len(rebound) == 0 || rebound[0].Content == baseline[0].Content {
		t.Fatalf("drift should surface in the leading system message; got %q", rebound[0].Content)
	}
}
