package boot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/workspaceid"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/trustedstate"
	"reasonix/internal/tools/builtin"
)

type bundleAuditSink struct {
	mu  sync.Mutex
	got []event.EvidenceBundleAudit
}

func (s *bundleAuditSink) Emit(event.Event) {}

func (s *bundleAuditSink) RecordEvidenceBundle(a event.EvidenceBundleAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, a)
}

func (s *bundleAuditSink) audits() []event.EvidenceBundleAudit {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]event.EvidenceBundleAudit(nil), s.got...)
}

// writeThenReplyProvider writes one file on the first round and replies after.
type writeThenReplyProvider struct {
	mu    sync.Mutex
	round int
}

func (p *writeThenReplyProvider) Name() string { return "boot-evidence-seal" }

func (p *writeThenReplyProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	i := p.round
	p.round++
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	if i == 0 {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "w1", Name: "write_file", Arguments: `{"path":"note.txt","content":"SEALED-BODY-MUST-NOT-PERSIST"}`,
		}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// Every turn through the real assembly seals a shadow bundle that a separate
// process can verify from disk, and the bundle keeps what a tool call proved
// without keeping the bytes it wrote.
func TestEffectEveryTurnSealsAVerifiableShadowBundle(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	provider.Register("boot-evidence-seal", func(provider.Config) (provider.Provider, error) {
		return &writeThenReplyProvider{}, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-evidence-seal"
model = "x"
`)
	approveWorkspace(t, dir)
	sink := &bundleAuditSink{}
	ctrl, err := Build(context.Background(), Options{Sink: sink, HeadlessApprovalMode: control.ToolApprovalAuto})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	// The write has no verification after it, so the readiness gate stops the
	// first turn. A turn the host stopped is still a turn with evidence.
	if err := ctrl.Run(context.Background(), "write the note"); err == nil {
		t.Fatal("the readiness gate let an unverified write finish")
	}
	if err := ctrl.Run(context.Background(), "and reply"); err != nil {
		t.Fatalf("second turn: %v", err)
	}
	writeFile(t, dir, "edited-by-hand.txt", "the user, between turns")
	if err := ctrl.Run(context.Background(), "and again"); err != nil {
		t.Fatalf("third turn: %v", err)
	}

	audits := sink.audits()
	if len(audits) != 3 {
		t.Fatalf("got %d bundle audits, want one per turn: %+v", len(audits), audits)
	}
	var last uint64
	for i, a := range audits {
		if !a.Sealed || a.FailureCode != "" || a.Generation <= last {
			t.Fatalf("turn %d audit = %+v, want sealed after generation %d", i+1, a, last)
		}
		last = a.Generation
	}
	if audits[0].Receipts == 0 {
		t.Fatal("the writing turn sealed no receipts")
	}
	for i, a := range audits {
		if !a.SnapshotComplete {
			t.Fatalf("turn %d left the workspace snapshot incomplete", i+1)
		}
	}
	if audits[0].UnobservedCompared {
		t.Fatal("the first turn compared against a snapshot nobody sealed")
	}
	if !audits[1].UnobservedCompared || audits[1].UnobservedChanges != 0 {
		t.Fatalf("turn 2 audit = %+v, want compared with nothing changed between turns", audits[1])
	}
	if !audits[2].UnobservedCompared || audits[2].UnobservedChanges != 1 {
		t.Fatalf("turn 3 audit = %+v, want the one file edited between turns", audits[2])
	}

	store := trustedstate.Open(filepath.Join(config.MemoryUserDir(), builtin.TrustedStateDir), nil)
	stream := workspaceid.PathFingerprint(dir)
	head, err := store.Verify(stream)
	if err != nil {
		t.Fatalf("a fresh process cannot verify the chain: %v", err)
	}
	if head.Generation != last || string(head.Record) != audits[2].Record || string(head.Integrity) != audits[2].Integrity {
		t.Fatalf("head = %+v, want the third audit's record %s", head, audits[2].Record)
	}

	rec, err := store.Record(trustedstate.Digest(audits[0].Record))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := store.Object(rec.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "SEALED-BODY-MUST-NOT-PERSIST") {
		t.Fatal("the bundle kept the bytes a tool call wrote")
	}
	var bundle struct {
		WithinTurn struct {
			Paths []string `json:"paths"`
		} `json:"within_turn"`
		Kind     string `json:"kind"`
		Blocked  bool   `json:"blocked"`
		Receipts []struct {
			ToolName   string `json:"tool_name"`
			ArgsDigest string `json:"args_digest"`
			Paths      []string
		} `json:"receipts"`
	}
	if err := json.Unmarshal(payload, &bundle); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range bundle.Receipts {
		if r.ToolName == "write_file" {
			found = r.ArgsDigest != ""
		}
	}
	if !slices.Contains(bundle.WithinTurn.Paths, "note.txt") {
		t.Fatalf("the writing turn's snapshot delta = %v, want note.txt", bundle.WithinTurn.Paths)
	}
	if bundle.Kind != "shadow_bundle/1" || !found || !bundle.Blocked {
		t.Fatalf("bundle = %s, want a blocked turn's write_file receipt carrying only its argument digest", payload)
	}
}

// scriptedProvider issues one tool call per round from calls, then replies.
type scriptedProvider struct {
	mu    sync.Mutex
	round int
	calls []provider.ToolCall
}

func (p *scriptedProvider) Name() string { return "boot-scripted" }

func (p *scriptedProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	i := p.round
	p.round++
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	if i < len(p.calls) {
		call := p.calls[i]
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &call}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// A todo the model marks completed is its own breakdown of the work, not a
// criterion anyone accepted. With nothing else done, the report gives no
// verdict rather than "done", and nothing is counted as satisfied by a claim.
func TestEffectAMarkedTodoProvesNothingThroughRealBuild(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	provider.Register("boot-claim-only", func(provider.Config) (provider.Provider, error) {
		return &scriptedProvider{calls: []provider.ToolCall{{
			ID: "t1", Name: "todo_write", Arguments: `{"todos":[{"content":"ship the feature","status":"completed"}]}`,
		}}}, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-claim-only"
model = "x"
`)
	approveWorkspace(t, dir)
	sink := &bundleAuditSink{}
	ctrl, err := Build(context.Background(), Options{Sink: sink, HeadlessApprovalMode: control.ToolApprovalAuto})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "ship the feature"); err != nil {
		t.Fatalf("the existing path let the claim finish the turn, so Run must succeed: %v", err)
	}
	audits := sink.audits()
	if len(audits) != 1 {
		t.Fatalf("got %d bundle audits, want 1", len(audits))
	}
	a := audits[0]
	if slices.Contains(a.DivergenceReasons, "claim_only") || a.DivergenceClass != "agree" {
		t.Fatalf("audit = %+v, want no claim-only satisfaction and the two verdicts agreeing", a)
	}

	store := trustedstate.Open(filepath.Join(config.MemoryUserDir(), builtin.TrustedStateDir), nil)
	rec, err := store.Record(trustedstate.Digest(a.Record))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := store.Object(rec.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct {
		Divergence struct {
			Old string `json:"old"`
		} `json:"divergence"`
	}
	if err := json.Unmarshal(payload, &bundle); err != nil || bundle.Divergence.Old != "unknown" {
		t.Fatalf("sealed divergence = %s (%v), want the report to give no verdict for a turn that only marked a todo", payload, err)
	}
}
