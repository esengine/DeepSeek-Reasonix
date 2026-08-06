package agent

import (
	"context"
	"os"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/navigator"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// collectingSink records events for assertions.
type collectingSink struct{ events []event.Event }

func (s *collectingSink) Emit(e event.Event) { s.events = append(s.events, e) }

// mockNavigator implements NavigatorKernel with programmable corrections.
type mockNavigator struct {
	begins  int
	ends    int
	digest  string
	briefs  []CorrectionBrief
	endErrs []error
}

func (m *mockNavigator) ImplicitStateDigest() string { return m.digest }

func (m *mockNavigator) BeginAction(ctx context.Context, verb, args string) error {
	m.begins++
	return nil
}

func (m *mockNavigator) EndAction(ctx context.Context, verb, args, output string, toolErr error) (CorrectionBrief, error) {
	m.ends++
	var b CorrectionBrief
	if len(m.briefs) > 0 {
		b = m.briefs[0]
		m.briefs = m.briefs[1:]
	}
	var e error
	if len(m.endErrs) > 0 {
		e = m.endErrs[0]
		m.endErrs = m.endErrs[1:]
	}
	return b, e
}

func newAgentForNavigatorTest(sink event.Sink) *Agent {
	sess := NewSession("sys")
	return New(&fakeProvider{reply: "ok"}, tool.NewRegistry(), sess, Options{RecentKeep: 2}, sink)
}

// TestApplyNavigatorCorrectionReinjectsFacts verifies the amnesia fix: a
// reinject_facts correction appends the recovered facts as a user message so
// the model can re-read them without a compaction round.
func TestApplyNavigatorCorrectionReinjectsFacts(t *testing.T) {
	sink := &collectingSink{}
	a := newAgentForNavigatorTest(sink)
	before := len(a.session.Messages)

	a.applyNavigatorCorrection(context.Background(), nil, CorrectionBrief{
		Strategy: "reinject_facts",
		Reason:   "facts dropped across fold",
		Reinject: []string{"path: /tmp/recovered.log", "id: 42"},
	})

	if got := len(a.session.Messages); got != before+1 {
		t.Fatalf("expected 1 injected message, got %d", got-before)
	}
	last := a.session.Messages[len(a.session.Messages)-1]
	if last.Role != provider.RoleUser {
		t.Errorf("expected user-role injection, got %v", last.Role)
	}
	if !strings.Contains(last.Content, "/tmp/recovered.log") || !strings.Contains(last.Content, "id: 42") {
		t.Errorf("injected message missing recovered facts: %q", last.Content)
	}
	if len(sink.events) == 0 {
		t.Error("expected an info event for the re-injection")
	}
}

// TestApplyNavigatorCorrectionWarnings verifies rollback/ask_host surface as
// warn events (the "dead light" defense: the agent knows the world drifted).
func TestApplyNavigatorCorrectionWarnings(t *testing.T) {
	for _, tc := range []struct {
		strategy string
		wantText string
	}{
		{"rollback", "rewound"},
		{"ask_host", "needs host/user"},
		{"retry", "retrying"},
	} {
		t.Run(tc.strategy, func(t *testing.T) {
			sink := &collectingSink{}
			a := newAgentForNavigatorTest(sink)
			a.applyNavigatorCorrection(context.Background(), nil, CorrectionBrief{Strategy: tc.strategy, Reason: "why"})
			found := false
			for _, ev := range sink.events {
				if strings.Contains(ev.Text, tc.wantText) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("no warn event containing %q for strategy %s; events=%d", tc.wantText, tc.strategy, len(sink.events))
			}
		})
	}
}

// TestApplyNavigatorCorrectionContinueIsNoop: continue must not touch the
// session or emit anything.
func TestApplyNavigatorCorrectionContinueIsNoop(t *testing.T) {
	sink := &collectingSink{}
	a := newAgentForNavigatorTest(sink)
	before := len(a.session.Messages)
	a.applyNavigatorCorrection(context.Background(), nil, CorrectionBrief{Strategy: "continue"})
	if len(a.session.Messages) != before {
		t.Error("continue must not append messages")
	}
	if len(sink.events) != 0 {
		t.Errorf("continue must not emit events, got %d", len(sink.events))
	}
}

// TestNavigatorBridgeEndToEnd drives a real navigator kernel through the
// agent-side bridge: Begin/End map to the kernel's observer mode, facts
// recovered from a result reach the digest, and a filesystem drift surfaces
// as a non-continue correction.
func TestNavigatorBridgeEndToEnd(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/baseline.txt", []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	adapter := navigator.NewReasonixAdapter(tool.NewRegistry(), event.Discard, navigator.ReasonixAdapterOptions{})
	kernel := navigator.New(adapter, navigator.Options{HistoryWindow: 20})
	kernel.AddSensor(navigator.NewFilesystemSensor(dir, 3))
	bridge := NewNavigatorBridge(kernel)
	if bridge == nil {
		t.Fatal("bridge must not be nil")
	}

	if err := bridge.BeginAction(ctx, "read", `{"file":"baseline.txt"}`); err != nil {
		t.Fatalf("BeginAction: %v", err)
	}
	if err := os.WriteFile(dir+"/drift.txt", []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	brief, err := bridge.EndAction(ctx, "read", `{"file":"baseline.txt"}`, "Result: /var/log/app.log updated", nil)
	if err != nil {
		t.Fatalf("EndAction: %v", err)
	}
	if brief.Strategy == "continue" {
		t.Error("expected a non-continue correction after filesystem drift")
	}
	// Facts recovered from the result must reach the compaction digest.
	if d := bridge.ImplicitStateDigest(); !strings.Contains(d, "/var/log/app.log") {
		t.Errorf("digest missing recovered fact: %s", d)
	}
}

// TestNavigatorBridgeNilIsSafe: a nil kernel produces a nil bridge and the
// run loop's nil checks keep working.
func TestNavigatorBridgeNilIsSafe(t *testing.T) {
	if b := NewNavigatorBridge(nil); b != nil {
		t.Error("expected nil bridge for nil kernel")
	}
}
