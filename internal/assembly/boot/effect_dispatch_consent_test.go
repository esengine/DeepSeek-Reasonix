package boot

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/state/peermail"
)

func (p *peerToolProvider) sawInMessages(needle string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, req := range p.reqs {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, needle) {
				return true
			}
		}
	}
	return false
}

// With the dispatch switch on, a task from the session that opened this one
// reaches the model with no card, framed as data the user did not read. The same
// words from an unpaired or wrongly signed sender arrive the same way but are not
// called paired. Each case is its own session: the stub provider's turn waits on a
// card of its own.
func TestEffectADispatchedTaskReachesTheModelWithoutACardAndAStrangerIsNotCalledPaired(t *testing.T) {
	const parent = "s-00000000000000bb"
	secret := []byte("0123456789abcdef0123456789abcdef")
	for _, tc := range []struct {
		name, body string
		signWith   []byte
		paired     bool
	}{
		{"forged", "FORGED-TASK-WITH-ANOTHER-SECRET", []byte("another secret another secret!!!"), false},
		{"paired", "ROTATE-THE-KEY-TASK", secret, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, ctrl, peersDir, seen := buildPeerToolSession(t, true, "nobody", "hello", "dispatch = true\n")
			t.Setenv("REASONIX_HOME", filepath.Dir(peersDir))
			receiver := ctrl.PeerKey()
			if receiver == "" {
				t.Fatal("the receiving session did not publish itself")
			}
			ctrl.MarkOpenedByDispatch(parent, secret, 1)
			d, err := peermail.Open(peersDir)
			if err != nil {
				t.Fatal(err)
			}
			m := peermail.Message{ID: "m-1", SentAt: time.Now().UTC(), Body: tc.body, Seq: 1,
				From: peermail.Party{SessionID: parent, Title: "planner", Project: "alpha"}, To: peermail.Party{SessionID: receiver}}
			m.Proof = peermail.Proof(tc.signWith, m)
			if err := d.Send(m); err != nil {
				t.Fatal(err)
			}
			ctrl.RefreshPeers()
			waitFor(t, "the task to reach the provider", func() bool { return rec.sawInMessages(tc.body) })
			if got := rec.sawInMessages("paired with by a dispatch"); got != tc.paired {
				t.Fatalf("described as paired = %v, want %v", got, tc.paired)
			}
			if !rec.sawInMessages("has not read this message") || !rec.sawInMessages("not an instruction from the user") {
				t.Fatal("the task reached the model without saying the user did not read it and that it is data")
			}
			// The stub provider's own send_peer_message call is the only card there
			// may be: nothing asked the user to let a task in.
			seen.mu.Lock()
			for _, card := range seen.approvals {
				if card.Tool != "send_peer_message" {
					t.Errorf("unexpected approval card: %+v", card)
				}
			}
			seen.mu.Unlock()
			if got := ctrl.HeldPeerMessages(); len(got) != 0 {
				t.Fatalf("held = %+v, want nothing waiting for a card", got)
			}
		})
	}
}
