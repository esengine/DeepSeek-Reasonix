package guardian

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type scriptedProvider struct {
	mu        sync.Mutex
	responses []scriptedResponse
	requests  []provider.Request
}

type scriptedResponse struct {
	text  string
	usage *provider.Usage
	err   error
}

func (p *scriptedProvider) Name() string { return "guardian-test" }

func (p *scriptedProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	resp := scriptedResponse{text: `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"ok"}`}
	if len(p.responses) > 0 {
		resp = p.responses[0]
		p.responses = p.responses[1:]
	}
	p.mu.Unlock()

	ch := make(chan provider.Chunk, 3)
	if resp.err != nil {
		close(ch)
		return nil, resp.err
	}
	if resp.text != "" {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: resp.text}
	}
	if resp.usage != nil {
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: resp.usage}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *scriptedProvider) requestsSnapshot() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.requests...)
}

type captureSink struct {
	mu     sync.Mutex
	events []event.Event
}

func (s *captureSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *captureSink) guardianEvents() []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []event.Event
	for _, e := range s.events {
		if e.Kind == event.GuardianAssessment {
			out = append(out, e)
		}
	}
	return out
}

func TestParseAssessmentEnforcesCriticalDeny(t *testing.T) {
	a, err := ParseAssessment(`{"risk_level":"critical","user_authorization":"high","outcome":"allow","rationale":"delete prod secrets"}`)
	if err != nil {
		t.Fatalf("ParseAssessment error: %v", err)
	}
	if a.Outcome != "deny" {
		t.Fatalf("critical outcome = %q, want deny", a.Outcome)
	}
}

func TestParseAssessmentRejectsUnknownEnum(t *testing.T) {
	if _, err := ParseAssessment(`{"risk_level":"spicy","user_authorization":"high","outcome":"allow","rationale":"x"}`); err == nil {
		t.Fatal("ParseAssessment accepted unknown risk_level")
	}
}

func TestTranscriptRenderKeepsFirstAndLastUserAnchors(t *testing.T) {
	entries := []TranscriptEntry{{Kind: "user", Text: "first task"}}
	for i := 0; i < maxRecentEntries+5; i++ {
		entries = append(entries, TranscriptEntry{Kind: "assistant", Text: "assistant detail"})
	}
	entries = append(entries, TranscriptEntry{Kind: "user", Text: "latest instruction"})
	rendered := FormatTranscript(entries)
	if !strings.Contains(rendered, "first task") || !strings.Contains(rendered, "latest instruction") {
		t.Fatalf("rendered transcript lost user anchors:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Some conversation entries were omitted.") {
		t.Fatalf("rendered transcript should mention omissions:\n%s", rendered)
	}
}

func TestGuardianSaveLoadRestoresCursorForDeltaTranscript(t *testing.T) {
	prov := &scriptedProvider{responses: []scriptedResponse{
		{text: `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"first ok"}`},
		{text: `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"second ok"}`},
	}}
	sink := &captureSink{}
	gs := NewSession(prov, tool.NewRegistry(), PolicyPrompt(), "guardian-test", 0, nil, sink)
	parent := agent.NewSession("sys")
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "first user request"})

	if allow, _, err := gs.Review(context.Background(), "write_file", json.RawMessage(`{"file_path":"a.txt"}`), parent); err != nil || !allow {
		t.Fatalf("first Review = allow %v err %v, want allow nil", allow, err)
	}
	path := filepath.Join(t.TempDir(), "session.guardian.jsonl")
	if err := gs.Save(path); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	if data, err := os.ReadFile(cursorPathForGuardianPath(path)); err != nil || !strings.Contains(string(data), `"EntryCount":1`) {
		t.Fatalf("cursor sidecar = %q err %v, want EntryCount 1", data, err)
	}

	loaded := NewSession(prov, tool.NewRegistry(), PolicyPrompt(), "guardian-test", 0, nil, sink)
	if err := loaded.Load(path); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if loaded.cursor.EntryCount != 1 {
		t.Fatalf("loaded cursor = %+v, want EntryCount 1", loaded.cursor)
	}
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "second user request"})
	if allow, _, err := loaded.Review(context.Background(), "write_file", json.RawMessage(`{"file_path":"b.txt"}`), parent); err != nil || !allow {
		t.Fatalf("second Review = allow %v err %v, want allow nil", allow, err)
	}

	reqs := prov.requestsSnapshot()
	if len(reqs) < 2 {
		t.Fatalf("requests = %d, want >= 2", len(reqs))
	}
	var delta string
	for _, req := range reqs {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "TRANSCRIPT DELTA") && strings.Contains(m.Content, "second user request") {
				delta = m.Content
				break
			}
		}
		if delta != "" {
			break
		}
	}
	if delta == "" {
		t.Fatalf("second request did not include a delta transcript")
	}
	if !strings.Contains(delta, "second user request") {
		t.Fatalf("delta transcript missing new parent entry:\n%s", delta)
	}
	if strings.Contains(delta, "first user request") {
		t.Fatalf("delta transcript repeated old parent entry:\n%s", delta)
	}
}

func TestGuardianUsageDoesNotLeakAcrossReviews(t *testing.T) {
	prov := &scriptedProvider{responses: []scriptedResponse{
		{
			text:  `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"first ok"}`,
			usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
		},
		{text: `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"second ok"}`},
	}}
	sink := &captureSink{}
	gs := NewSession(prov, tool.NewRegistry(), PolicyPrompt(), "guardian-test", 0, nil, sink)
	parent := agent.NewSession("sys")
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "do it"})

	if allow, _, err := gs.Review(context.Background(), "write_file", json.RawMessage(`{"file_path":"a.txt"}`), parent); err != nil || !allow {
		t.Fatalf("first Review = allow %v err %v, want allow nil", allow, err)
	}
	if allow, _, err := gs.Review(context.Background(), "write_file", json.RawMessage(`{"file_path":"b.txt"}`), parent); err != nil || !allow {
		t.Fatalf("second Review = allow %v err %v, want allow nil", allow, err)
	}

	events := sink.guardianEvents()
	if len(events) != 2 {
		t.Fatalf("guardian events = %d, want 2", len(events))
	}
	if events[0].Guardian.Usage == nil || events[0].Guardian.Usage.TotalTokens != 12 {
		t.Fatalf("first usage = %+v, want total 12", events[0].Guardian.Usage)
	}
	if events[1].Guardian.Usage != nil {
		t.Fatalf("second usage leaked from first review: %+v", events[1].Guardian.Usage)
	}
}

// TestGuardianReviewTurnsAlternateRoles pins the review request shape: the
// transcript evidence and action request ride in one combined user message per
// review, so the guardian session alternates user/assistant strictly and
// providers that reject consecutive same-role messages can run the guardian.
func TestGuardianReviewTurnsAlternateRoles(t *testing.T) {
	prov := &scriptedProvider{responses: []scriptedResponse{
		{text: `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"first ok"}`},
		{text: `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"second ok"}`},
	}}
	gs := NewSession(prov, tool.NewRegistry(), PolicyPrompt(), "guardian-test", 0, nil, &captureSink{})
	parent := agent.NewSession("sys")
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "do the thing"})

	for i := 0; i < 2; i++ {
		if allow, _, err := gs.Review(context.Background(), "write_file", json.RawMessage(`{"file_path":"a.txt"}`), parent); err != nil || !allow {
			t.Fatalf("review %d = allow %v err %v, want allow nil", i+1, allow, err)
		}
	}

	reqs := prov.requestsSnapshot()
	if len(reqs) < 2 {
		t.Fatalf("requests = %d, want >= 2", len(reqs))
	}
	for r, req := range reqs {
		for i := 1; i < len(req.Messages); i++ {
			if req.Messages[i].Role == provider.RoleUser && req.Messages[i-1].Role == provider.RoleUser {
				t.Fatalf("request %d carries consecutive user messages at index %d", r, i)
			}
		}
	}

	// The combined message must still carry the evidence boundary and the action.
	msgs := reqs[len(reqs)-1].Messages
	var review string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleUser {
			review = msgs[i].Content
			break
		}
	}
	for _, want := range []string{"untrusted evidence", "The agent has requested the following action", "write_file"} {
		if !strings.Contains(review, want) {
			t.Fatalf("combined review message missing %q:\n%s", want, review)
		}
	}
}

// TestGuardianFailedReviewRollsBackSession pins the error-path rollback:
// agent.Run appends the combined review user message before the provider is
// reached, so a failed review must not leave it dangling — the next review
// would otherwise append another user message and strict-alternation providers
// would reject every request from then on.
func TestGuardianFailedReviewRollsBackSession(t *testing.T) {
	prov := &scriptedProvider{responses: []scriptedResponse{
		{err: fmt.Errorf("provider unavailable")},
		{text: `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"ok"}`},
	}}
	gs := NewSession(prov, tool.NewRegistry(), PolicyPrompt(), "guardian-test", 0, nil, &captureSink{})
	parent := agent.NewSession("sys")
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "do the thing"})

	allow, _, err := gs.Review(context.Background(), "write_file", json.RawMessage(`{"file_path":"a.txt"}`), parent)
	if err == nil && allow {
		t.Fatal("first review should fail closed")
	}
	if n := gs.sess.Len(); n != 1 {
		t.Fatalf("guardian session messages = %d after failed review, want rollback to system only", n)
	}

	if allow, _, err := gs.Review(context.Background(), "write_file", json.RawMessage(`{"file_path":"a.txt"}`), parent); err != nil || !allow {
		t.Fatalf("second review = allow %v err %v, want allow nil", allow, err)
	}
	reqs := prov.requestsSnapshot()
	last := reqs[len(reqs)-1]
	for i := 1; i < len(last.Messages); i++ {
		if last.Messages[i].Role == provider.RoleUser && last.Messages[i-1].Role == provider.RoleUser {
			t.Fatalf("request after failed review carries consecutive user messages at index %d", i)
		}
	}
}

// TestGuardianLoadResetsLegacyConsecutiveUserSessions pins the load-time
// normalization: sessions saved by the old multi-message review shape carry
// consecutive user messages that would poison strict-alternation providers, so
// Load starts fresh instead of adopting them.
func TestGuardianLoadResetsLegacyConsecutiveUserSessions(t *testing.T) {
	legacy := agent.NewSession(PolicyPrompt())
	legacy.Add(provider.Message{Role: provider.RoleUser, Content: "transcript evidence"})
	legacy.Add(provider.Message{Role: provider.RoleUser, Content: "action request"})
	legacy.Add(provider.Message{Role: provider.RoleAssistant, Content: `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"ok"}`})
	path := filepath.Join(t.TempDir(), "session.guardian.jsonl")
	if err := legacy.Save(path); err != nil {
		t.Fatalf("Save legacy session: %v", err)
	}

	gs := NewSession(&scriptedProvider{}, tool.NewRegistry(), PolicyPrompt(), "guardian-test", 0, nil, &captureSink{})
	if err := gs.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if n := gs.sess.Len(); n != 1 {
		t.Fatalf("loaded legacy session messages = %d, want reset to system only", n)
	}
	if gs.cursor.EntryCount != 0 {
		t.Fatalf("cursor = %+v, want zeroed after reset", gs.cursor)
	}
}

// TestGuardianSessionAlternatesAfterCompaction reproduces the compaction seam:
// every compactEvery-th review runs CompactNow, and generic compaction inserts
// its digest as a RoleUser message that can land directly before a review's
// user turn — consecutive user roles again. The post-review normalization must
// keep the session strictly alternating across that fold.
func TestGuardianSessionAlternatesAfterCompaction(t *testing.T) {
	prov := &scriptedProvider{} // default allow verdict, also serves the summarizer
	gs := NewSession(prov, tool.NewRegistry(), PolicyPrompt(), "guardian-test", 0, nil, &captureSink{})
	parent := agent.NewSession("sys")

	filler := strings.Repeat("parent transcript filler. ", 160)
	for i := 0; i < compactEvery; i++ {
		parent.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("turn %d: %s", i, filler)})
		if allow, _, err := gs.Review(context.Background(), "write_file", json.RawMessage(`{"file_path":"a.txt"}`), parent); err != nil || !allow {
			t.Fatalf("review %d = allow %v err %v, want allow nil", i+1, allow, err)
		}
	}

	msgs := gs.sess.Snapshot()
	var hasDigest bool
	for _, m := range msgs {
		if agent.IsCompactionSummary(m) {
			hasDigest = true
		}
	}
	if !hasDigest {
		t.Fatal("test setup: guardian compaction did not fold anything, the digest adjacency is not exercised")
	}
	for i := 1; i < len(msgs); i++ {
		if msgs[i].Role == msgs[i-1].Role {
			t.Fatalf("guardian session has consecutive %s messages at indexes %d/%d of %d", msgs[i].Role, i-1, i, len(msgs))
		}
	}
}
