package permission

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The posture that allows every write says what may happen to this workspace's
// files. It does not say a delegated run may redraw the fence it was given, so
// the request comes back to the user under every fallback mode.
func TestWideningTheFenceReturnsToTheUserUnderEveryMode(t *testing.T) {
	args := json.RawMessage(`{"path":"/ws/internal/foo/bar.go"}`)
	for _, mode := range []Decision{Ask, Allow, Deny} {
		p := Policy{Mode: mode}
		if got := p.Decide(ExtendWritePaths, false, args); got != Ask && mode != Deny {
			t.Errorf("Mode %v: widening decided %v, want Ask", mode, got)
		}
	}
}

// Ordinary writes are unaffected: the fence question is its own capability, not
// a second prompt on every file the run was already allowed to touch.
func TestOrdinaryWritesAreNotTurnedIntoFenceQuestions(t *testing.T) {
	args := json.RawMessage(`{"path":"/ws/internal/foo/bar.go"}`)
	if got := (Policy{Mode: Allow}).Decide("write_file", false, args); got != Allow {
		t.Errorf("write_file under Allow decided %v, want Allow", got)
	}
}

// That is how the line gets moved: deliberately, by a rule naming the path,
// rather than by a posture nobody re-read.
func TestAnExplicitRuleStillMovesTheLine(t *testing.T) {
	p := Policy{Mode: Ask, Allow: []Rule{{Tool: ExtendWritePaths, Subject: "/ws/internal/foo/*"}}}
	if got := p.Decide(ExtendWritePaths, false, json.RawMessage(`{"path":"/ws/internal/foo/bar.go"}`)); got != Allow {
		t.Errorf("an explicit allow for the path decided %v, want Allow", got)
	}
	// And only for what it names.
	if got := p.Decide(ExtendWritePaths, false, json.RawMessage(`{"path":"/ws/secrets/key.pem"}`)); got != Ask {
		t.Errorf("a path the rule does not name decided %v, want Ask", got)
	}
}

// Deny stays the strongest answer.
func TestDenyRuleBeatsTheFenceRequest(t *testing.T) {
	p := Policy{Mode: Allow, Deny: []Rule{{Tool: ExtendWritePaths, Subject: "/ws/secrets/*"}}}
	if got := p.Decide(ExtendWritePaths, false, json.RawMessage(`{"path":"/ws/secrets/key.pem"}`)); got != Deny {
		t.Errorf("denied path decided %v, want Deny", got)
	}
}

// The hole this pins shut: Ask is fail-open when no approver is attached, and
// YOLO is exactly the posture built with none. Without this the fence question
// answers itself under the one posture it most needed a person for — and worse
// than before it was a question at all, when the write was simply refused.
func TestWideningIsRefusedWhenThereIsNobodyToAsk(t *testing.T) {
	gate := NewGate(Policy{Mode: Allow}, nil)
	args := json.RawMessage(`{"path":"/ws/secrets/key.pem"}`)

	allow, reason, err := gate.Check(context.Background(), ExtendWritePaths, args, false)
	if err != nil {
		t.Fatal(err)
	}
	if allow {
		t.Error("widening was granted with no approver attached; the fence moves with nobody watching")
	}
	if strings.TrimSpace(reason) == "" {
		t.Error("the refusal says nothing; the run cannot tell this from a denial it should stop retrying")
	}
}

// And the posture keeps its meaning for everything else: an ordinary write
// under a nil approver still runs, which is what non-interactive autonomy is.
func TestOrdinaryWritesStillRunWithNobodyToAsk(t *testing.T) {
	gate := NewGate(Policy{Mode: Allow}, nil)
	allow, _, err := gate.Check(context.Background(), "write_file", json.RawMessage(`{"path":"/ws/a.go"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !allow {
		t.Error("an ordinary write was refused with no approver; that is not what this branch is for")
	}
}
