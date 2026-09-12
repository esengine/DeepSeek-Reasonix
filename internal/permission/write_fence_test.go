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

// "Don't ask again" answers a question about one subject. Granting the bare
// tool name would answer every other subject it will ever have — approving one
// file would open every file, including ones no prompt ever named.
func TestSessionGrantForTheFenceNamesThePath(t *testing.T) {
	granted := "/ws/internal/foo/bar.go"
	rule := SessionGrantRuleForScope(ExtendWritePaths, granted)

	if !RuleMatchesString(rule, ExtendWritePaths, granted) {
		t.Errorf("rule %q does not cover the path it was granted for", rule)
	}
	for _, other := range []string{
		"/home/user/.ssh/authorized_keys",
		"/ws/.git/hooks/pre-commit",
		"/ws/secrets/key.pem",
	} {
		if RuleMatchesString(rule, ExtendWritePaths, other) {
			t.Errorf("rule %q also covers %s; one approval opened the whole fence", rule, other)
		}
	}
}

// The same defect on the tiering that was already there: a session grant taken
// on a low-risk install plan must not answer for a high-risk one, or the grade
// that exists to return high-risk plans to a person is bypassed by approving a
// harmless one first.
func TestSessionGrantForAnInstallPlanNamesTheTicket(t *testing.T) {
	rule := SessionGrantRuleForScope("install_source", "low:sha256:abc")
	if !RuleMatchesString(rule, "install_source", "low:sha256:abc") {
		t.Errorf("rule %q does not cover the plan it was granted for", rule)
	}
	if RuleMatchesString(rule, "install_source", "high:sha256:def") {
		t.Errorf("rule %q covers a high-risk ticket; approving a low-risk plan disarmed the grade", rule)
	}
}

// The guard over the two lists above: anything whose authorization can turn on
// its subject has to be granted by subject, or a bare-tool grant silently
// answers a decision nobody made. A tool added to one and not the other is the
// way this regresses.
func TestEverySubjectSensitiveToolIsSubjectScoped(t *testing.T) {
	// Subjects chosen to make each tool's human-required branch fire.
	sensitive := map[string]string{
		ExtendWritePaths:  "/ws/any/path",
		installSourceTool: selfExtendHumanRisk + "sha256:abc",
	}
	for tool, subject := range sensitive {
		if !subjectRequiresHuman(tool, subject) {
			t.Errorf("%s no longer requires a human for %q; this table is stale", tool, subject)
		}
		if !subjectScopedGrant(tool) {
			t.Errorf("%s decides on its subject but grants by bare tool name; one approval would cover every subject", tool)
		}
	}
}

// A grant recorded before grants carried subjects names only the tool, and it
// has been standing for an answer nobody gave: the user approved one plan, and
// the rule kept the tool. It stops covering anything for these tools, which
// asks once more instead of carrying that forward.
func TestABareSessionGrantNoLongerAnswersForEverySubject(t *testing.T) {
	for _, tool := range []string{ExtendWritePaths, installSourceTool} {
		stale := tool // what the old code recorded
		for _, subject := range []string{"low:sha256:abc", "high:sha256:def", "/ws/secrets/key.pem"} {
			if SessionGrantMatches(stale, tool, subject) {
				t.Errorf("stale grant %q still covers %s %q", stale, tool, subject)
			}
		}
	}
}

// The grants this records now keep working, for exactly what they name.
func TestASubjectScopedGrantStillCoversItsOwnSubject(t *testing.T) {
	rule := SessionGrantRuleForScope(installSourceTool, "low:sha256:abc")
	if !SessionGrantMatches(rule, installSourceTool, "low:sha256:abc") {
		t.Errorf("grant %q does not cover the ticket it was given for", rule)
	}
	if SessionGrantMatches(rule, installSourceTool, "high:sha256:def") {
		t.Errorf("grant %q covers a different ticket", rule)
	}
}

// Everything else is unchanged: a bare grant for a tool whose authorization
// does not read its subject is still exactly what the user agreed to.
func TestABareGrantStillWorksForOrdinaryTools(t *testing.T) {
	if !SessionGrantMatches("read_file", "read_file", "/ws/anything.go") {
		t.Error("a bare grant stopped covering an ordinary tool; that answer was never ambiguous")
	}
}
