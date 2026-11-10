package boot

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/session/control"
	"reasonix/internal/state/peermail"
	"reasonix/internal/state/sessioninbox"
)

// What may start a message from another session while the receiver's queue is
// paused: a card the person just approved starts it whatever paused the queue; a
// standing consent (a released project) starts it only over a pause the host set
// itself. Items the person queued earlier never run, and the pause stays.
func TestEffectPeerMessageAgainstAPausedQueue(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes do not make a file unreadable on Windows")
	}
	cases := []struct {
		pause, consent string
		starts         bool
	}{
		{"user", "card", true},
		{"user", "release", false},
		{"host", "card", true},
		{"host", "release", true},
		{"host then user", "release", false},
	}
	for _, tc := range cases {
		t.Run(tc.pause+" pause, "+tc.consent, func(t *testing.T) {
			rec, peersDir, ctrl, held := buildPeerSession(t, true)
			home := filepath.Dir(peersDir)
			t.Setenv("REASONIX_HOME", home)
			config := "[peers]\nenabled = true\ndispatch = false\n"
			if tc.consent == "release" {
				config += fmt.Sprintf("release_projects = [%q]\n", ctrl.WorkspaceRoot())
			}
			writeFile(t, home, "config.toml", config)

			switch tc.pause {
			case "user":
				if err := ctrl.SetInboxPaused(true); err != nil {
					t.Fatal(err)
				}
				if _, err := ctrl.EnqueueInbox(control.InboxRequest{Intent: sessioninbox.IntentFollowup, Submit: "STALE-USER-ITEM"}); err != nil {
					t.Fatal(err)
				}
			case "host", "host then user":
				if err := os.WriteFile(filepath.Join(ctrl.WorkspaceRoot(), "locked.md"), []byte("x"), 0o000); err != nil {
					t.Fatal(err)
				}
				if _, err := ctrl.EnqueueInbox(control.InboxRequest{Intent: sessioninbox.IntentFollowup, Submit: "STALE-USER-ITEM @locked.md"}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.pause == "host then user" {
				if err := ctrl.SetInboxPaused(true); err != nil {
					t.Fatal(err)
				}
			}
			if snap := ctrl.InboxSnapshot(); !snap.Paused {
				t.Fatal("the queue is not paused")
			} else if tc.pause != "host" && snap.PauseCode != sessioninbox.PauseUser {
				t.Fatalf("pause code = %q, want the person's pause", snap.PauseCode)
			}

			d, err := peermail.Open(peersDir)
			if err != nil {
				t.Fatal(err)
			}
			var receiver string
			waitFor(t, "this session to be published", func() bool {
				live, _ := d.ListLive(func(peermail.Live) bool { return true })
				if len(live) == 1 {
					receiver = live[0].SessionID
				}
				return receiver != ""
			})
			const words = "WORDS-AGAINST-A-PAUSED-QUEUE"
			if err := d.Send(peermail.Message{
				ID: "m1", SentAt: time.Now().UTC(), Body: words,
				From: peermail.Party{SessionID: "s-0000000000000001", Title: "other"}, To: peermail.Party{SessionID: receiver},
			}); err != nil {
				t.Fatal(err)
			}
			if tc.consent == "card" {
				waitFor(t, "the message to be held", func() bool { return len(held()) == 1 })
				if err := ctrl.ResolvePeerMessage("m1", true); err != nil {
					t.Fatal(err)
				}
			}

			if tc.starts {
				waitFor(t, "the message to start a turn", func() bool { return rec.anyMentions(words) })
				waitFor(t, "the session to settle", func() bool { return !ctrl.Running() })
			} else {
				waitFor(t, "the message to be queued", func() bool { return peerItemState(ctrl) == sessioninbox.StateQueued })
				time.Sleep(3 * time.Second)
				if rec.anyMentions(words) {
					t.Fatal("a standing consent started a message over a pause the person set")
				}
				if peerItemState(ctrl) != sessioninbox.StateQueued {
					t.Fatalf("the message is %q, want it left queued", peerItemState(ctrl))
				}
			}
			if rec.anyMentions("STALE-USER-ITEM") {
				t.Fatal("an item queued earlier ran")
			}
			if snap := ctrl.InboxSnapshot(); !snap.Paused {
				t.Fatal("the pause was lifted")
			}
		})
	}
}

func peerItemState(ctrl *control.Controller) sessioninbox.InboxState {
	for _, it := range ctrl.InboxSnapshot().Items {
		if strings.HasPrefix(it.Source, "peer:") {
			return it.State
		}
	}
	return ""
}
