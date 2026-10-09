package plugin

import (
	"context"
	"errors"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
)

type capturedSink struct {
	mu   sync.Mutex
	kind []event.Kind
	text []string
}

func (c *capturedSink) Emit(e event.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.kind = append(c.kind, e.Kind)
	c.text = append(c.text, e.Text)
}

// A status view refreshes on an event, so a state change with no event is a
// view that stays wrong. The case that shipped: a tools-only server (no prompts,
// no resources) connects lazily on its first call, and the only announcements
// were made from the prompt and resource paths — which such a server never
// takes. It showed its boot-time failure while the agent used it fine.
func TestAnnounceReportsWithoutPromptsOrResources(t *testing.T) {
	h := NewHost()
	sink := &capturedSink{}
	h.SetStatusSink(sink)

	h.announce("%s: connected", "windows-mcp")

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.kind) != 1 || sink.kind[0] != event.MCPSurfaceReady {
		t.Fatalf("kinds = %v, want one MCPSurfaceReady", sink.kind)
	}
	if sink.text[0] != "windows-mcp: connected" {
		t.Errorf("text = %q", sink.text[0])
	}
}

// Assembly may hand over a nil sink (headless paths do), and a status change
// must not take the connection down with it.
func TestAnnounceToleratesNoSink(t *testing.T) {
	NewHost().announce("x: connected")
}

func TestConnectionStateChangesAnnounceStatus(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHost()
	defer h.Close()
	sink := &capturedSink{}
	h.SetStatusSink(sink)

	srv := mcpHTTPServer(t, false)
	defer srv.Close()
	spec := Spec{Name: "tools", Type: "http", URL: srv.URL,
		Headers: map[string]string{"Authorization": "Bearer secret"}}
	if _, err := h.EnsureConnected(context.Background(), spec); err != nil {
		t.Fatalf("connect: %v", err)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.text) != 2 || sink.text[0] != "tools: connecting" || sink.text[1] != "tools: connected" {
		t.Fatalf("status announcements = %v", sink.text)
	}
}

// A failed attempt is announced as one: the row it refreshes says "failed", and
// an announcement that never fired would leave a status view showing whatever
// it showed before the attempt.
func TestFailureAnnouncesConnectionFailed(t *testing.T) {
	h := NewHost()
	sink := &capturedSink{}
	h.SetStatusSink(sink)

	h.RecordFailure(Spec{Name: "tools", Type: "http"}, errors.New("connection refused"))

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.text) != 1 || sink.text[0] != "tools: connection failed" {
		t.Fatalf("announcements = %v, want the failure", sink.text)
	}
}

// A project MCP blocked pending authorization was never attempted, so the
// announcement must not claim a connection failed: the row it refreshes says
// "awaiting approval" and a status view told otherwise sends the user looking
// for a fault that does not exist.
func TestLaunchApprovalAnnouncesAwaitingApproval(t *testing.T) {
	h := NewHost()
	sink := &capturedSink{}
	h.SetStatusSink(sink)

	h.RecordLaunchApprovalRequired(Spec{Name: "project-tools", Type: "stdio"})

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.text) != 1 || sink.text[0] != "project-tools: awaiting approval" {
		t.Fatalf("announcements = %v, want the approval state", sink.text)
	}
	failures := h.Failures()
	if len(failures) != 1 || !failures[0].RequiresLaunchApproval {
		t.Fatalf("failures = %+v, want one approval-pending record", failures)
	}
}
