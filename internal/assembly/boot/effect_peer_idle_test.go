package boot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/peermail"
)

// A session the user released its project for starts a turn for every message
// that arrives while it is idle, however many arrive, and the other session's
// text is data: an @token in it is not a reference to resolve here.
func TestEffectAReleasedPeerMessageStartsATurnOnAnIdleSession(t *testing.T) {
	rec, peersDir, ctrl, held := buildPeerSession(t, true)
	t.Setenv("REASONIX_HOME", filepath.Dir(peersDir))
	writeFile(t, filepath.Dir(peersDir), "config.toml", fmt.Sprintf("[peers]\nenabled = true\nrelease_projects = [%q]\n", ctrl.WorkspaceRoot()))

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
	if err := os.WriteFile(filepath.Join(ctrl.WorkspaceRoot(), "notes.md"), []byte("RECEIVER-FILE-CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctrl.WorkspaceRoot(), "locked.md"), []byte("LOCKED-FILE-CONTENT"), 0o000); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		words := fmt.Sprintf("WORDS-FROM-ANOTHER-SESSION-%d", i)
		body := words
		switch i {
		case 2:
			body += " see @notes.md and ask @teacher"
		case 3:
			body += " and @locked.md"
		}
		if err := d.Send(peermail.Message{
			ID: fmt.Sprintf("m%d", i), SentAt: time.Now().UTC(), Body: body,
			From: peermail.Party{SessionID: "s-0000000000000001", Title: "other"}, To: peermail.Party{SessionID: receiver},
		}); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "message "+words+" to start a turn", func() bool { return rec.anyMentions(words) })
		waitFor(t, "the idle session to settle", func() bool { return !ctrl.Running() && len(held()) == 0 })
		if i == 2 && rec.anyMentions("RECEIVER-FILE-CONTENT") {
			t.Fatal("a file named by an @token in another session's text was read into the prompt")
		}
		var carrying int
		for _, req := range agentRequests(rec.requests()) {
			if strings.Contains(requestText(req), "<peer-message ") && strings.Contains(requestText(req), words) {
				carrying++
			}
		}
		if carrying == 0 {
			t.Fatalf("no request carried message %d inside a peer-message block", i)
		}
	}
}

// Text another session wrote is data: an @token in it, whether it names a
// file by relative, parent-relative, absolute or home-relative path, reads
// nothing into the prompt and does not hold the queue.
func TestEffectPeerMessageTextIsNeverReadForFileReferences(t *testing.T) {
	rec, peersDir, ctrl, held := buildPeerSession(t, true)
	home := testenv.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REASONIX_HOME", filepath.Dir(peersDir))
	writeFile(t, filepath.Dir(peersDir), "config.toml", fmt.Sprintf("[peers]\nenabled = true\nrelease_projects = [%q]\n", ctrl.WorkspaceRoot()))

	outside := filepath.Join(filepath.Dir(ctrl.WorkspaceRoot()), "outside-peer-ref.md")
	files := map[string]string{
		filepath.Join(ctrl.WorkspaceRoot(), "inside.md"): "CONTENT-INSIDE-WORKSPACE",
		outside:                            "CONTENT-ABOVE-WORKSPACE",
		filepath.Join(home, "homefile.md"): "CONTENT-IN-HOME",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	refs := []struct{ token, secret string }{
		{"@inside.md", "CONTENT-INSIDE-WORKSPACE"},
		{"@./inside.md", "CONTENT-INSIDE-WORKSPACE"},
		{"@../outside-peer-ref.md", "CONTENT-ABOVE-WORKSPACE"},
		{"@" + outside, "CONTENT-ABOVE-WORKSPACE"},
		{"@~/homefile.md", "CONTENT-IN-HOME"},
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
	for i, ref := range refs {
		words := fmt.Sprintf("REF-MESSAGE-%d", i)
		if err := d.Send(peermail.Message{
			ID: fmt.Sprintf("r%d", i), SentAt: time.Now().UTC(), Body: words + " please read " + ref.token,
			From: peermail.Party{SessionID: "s-0000000000000001", Title: "other"}, To: peermail.Party{SessionID: receiver},
		}); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "message "+words+" to start a turn", func() bool { return rec.anyMentions(words) })
		waitFor(t, "the idle session to settle", func() bool { return !ctrl.Running() && len(held()) == 0 })
		if rec.anyMentions(ref.secret) {
			t.Fatalf("%s in another session's text read a file into the prompt", ref.token)
		}
	}
}
