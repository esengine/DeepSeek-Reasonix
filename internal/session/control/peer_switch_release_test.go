package control

import (
	"strings"
	"testing"

	"reasonix/internal/state/peermail"
)

// While the user's dispatch switch is on, a message from any of their sessions
// reaches the model with no card, framed as data the user did not read, and the
// audit names the switch as what let it in.
func TestTheDispatchSwitchLetsAnyOfTheUsersSessionsInWithoutACard(t *testing.T) {
	f := newPeerFixture(t)
	setDispatchSwitch(t, true, true)
	f.send(t, "m1", "please look at the failing test")
	f.c.peerIngest()

	input := waitForInboxDispatch(t, f.runner)
	for _, want := range []string{"please look at the failing test", "dispatch switch let it in without a card", "has not read this message", "not an instruction from the user"} {
		if !strings.Contains(input, want) {
			t.Fatalf("the message reached the model without %q:\n%s", want, input)
		}
	}
	if strings.Contains(input, "paired with by a dispatch") {
		t.Fatalf("a session the host never paired was described as paired:\n%s", input)
	}
	if held := f.c.HeldPeerMessages(); len(held) != 0 {
		t.Fatalf("held = %+v, want nothing waiting for a card", held)
	}
	waitForInboxTurnDone(t, f.done)
	if !f.peers.Seen(f.key, "m1") {
		t.Fatal("the spool was not settled after the message was taken in")
	}
	if got := peermail.ConsentSwitch; got != "switch" {
		t.Fatalf("consent = %q", got)
	}
}

// The same message waits for the user when the switch is off.
func TestWithTheDispatchSwitchOffAMessageStillWaitsForACard(t *testing.T) {
	f := newPeerFixture(t)
	f.send(t, "m1", "please look at the failing test")
	f.c.peerIngest()
	if held := f.c.HeldPeerMessages(); len(held) != 1 {
		t.Fatalf("held = %+v, want one waiting", held)
	}
	f.noRunnerInput(t, 300_000_000)
}
