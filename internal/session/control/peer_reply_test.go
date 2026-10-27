package control

import (
	"errors"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/state/peermail"
)

var errAnswer = errors.New("boom")

func doneEvent(cancelled bool, err error) event.Event {
	return event.Event{Kind: event.TurnDone, Cancelled: cancelled, Err: err}
}

var pairSecret = []byte("0123456789abcdef0123456789abcdef")

// openedBy makes f's session one that the fixture's sender opened by a dispatch
// (opener true) or one that the sender was itself handed by this session
// (opener false), publishes the sender so it can be written to, and delivers
// one signed message from it.
func (f *peerFixture) pairedWith(t *testing.T, opener bool) {
	t.Helper()
	if err := f.peers.PublishLive(peermail.Live{SessionID: f.sender, SessionPath: "/tmp/sender.jsonl", PID: 1, StartedAt: time.Now(), BeatAt: time.Now(), Inbound: "hold"}); err != nil {
		t.Fatal(err)
	}
	if opener {
		f.c.MarkOpenedByDispatch(f.sender, pairSecret, 1)
	} else {
		f.c.NoteDispatchedSession(f.sender, pairSecret)
	}
}

func (f *peerFixture) sendSigned(t *testing.T, id, body string, seq uint64) {
	t.Helper()
	m := peermail.Message{
		ID: id, SentAt: time.Now().UTC(), Body: body, Seq: seq,
		From: peermail.Party{SessionID: f.sender, Title: "sender", Project: "alpha"},
		To:   peermail.Party{SessionID: f.key},
	}
	m.Proof = peermail.Proof(pairSecret, m)
	if err := f.peers.Send(m); err != nil {
		t.Fatal(err)
	}
}

func (f *peerFixture) senderInbox() []peermail.Message {
	got, _ := f.peers.Pending(f.sender)
	return got
}

func waitForSenderInbox(t *testing.T, f *peerFixture) []peermail.Message {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := f.senderInbox(); len(got) > 0 {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

func TestASessionAnswersTheSessionThatOpenedItOnItsOwn(t *testing.T) {
	f := newPeerFixture(t)
	setDispatchSwitch(t, true, true)
	f.pairedWith(t, true)
	f.sendSigned(t, "m-task", "please rotate the key", 1)
	f.c.peerIngest()
	waitForInboxDispatch(t, f.runner)
	got := waitForSenderInbox(t, f)
	if len(got) != 1 || got[0].To.SessionID != f.sender || !strings.HasPrefix(got[0].Body, "Reply from the session you handed a task to") {
		t.Fatalf("the sender's inbox = %+v, want one reply from the session it dispatched to", got)
	}
	if got[0].Proof == "" || got[0].Seq == 0 {
		t.Fatalf("the reply is not signed as a pairing message: %+v", got[0])
	}
}

func TestASessionDoesNotAnswerAMessageFromASessionItOpened(t *testing.T) {
	f := newPeerFixture(t)
	setDispatchSwitch(t, true, true)
	f.pairedWith(t, false)
	f.sendSigned(t, "m-back", "done, here is the result", 1)
	f.c.peerIngest()
	waitForInboxDispatch(t, f.runner)
	time.Sleep(300 * time.Millisecond)
	if got := f.senderInbox(); len(got) != 0 {
		t.Fatalf("a reply was answered with another message, which would keep two sessions talking: %+v", got)
	}
}

func TestNoReplyGoesOutWhenTheDispatchSwitchIsOff(t *testing.T) {
	f := newPeerFixture(t)
	f.runner.hold = make(chan struct{})
	setDispatchSwitch(t, true, true)
	f.pairedWith(t, true)
	f.sendSigned(t, "m-task", "please rotate the key", 1)
	f.c.peerIngest()
	waitForInboxDispatch(t, f.runner)
	setDispatchSwitch(t, true, false)
	close(f.runner.hold)
	time.Sleep(300 * time.Millisecond)
	if got := f.senderInbox(); len(got) != 0 {
		t.Fatalf("a reply went out with dispatch off: %+v", got)
	}
}

func TestTheReplyNamesHowTheTurnEndedWithoutReadingItsWords(t *testing.T) {
	f := newPeerFixture(t)
	for outcome, tc := range map[string]struct {
		cancelled bool
		err       error
		want      string
	}{
		"stopped": {cancelled: true, want: "its user stopped it"},
		"failed":  {err: errAnswer, want: "ended with an error"},
		"empty":   {want: "without writing an answer"},
	} {
		done := doneEvent(tc.cancelled, tc.err)
		if got := f.c.replyBody(done); !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q does not say %q", outcome, got, tc.want)
		}
	}
}
