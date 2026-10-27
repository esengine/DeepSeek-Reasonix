package control

import (
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/peermail"
	"reasonix/internal/state/sessionstore"
)

type unreadFixture struct {
	c       *Controller
	peers   *peermail.Dir
	key     string
	sender  string
	dir     string
	path    string
	notices chan event.Event
}

// newUnreadFixture is a receiving session whose transcript already has a
// conversation in it, as one a person has opened before.
func newUnreadFixture(t *testing.T) *unreadFixture {
	t.Helper()
	root := testenv.TempDir(t)
	peersDir := filepath.Join(root, "peers")
	sessions := filepath.Join(root, "sessions")
	path := filepath.Join(sessions, "receiver.jsonl")
	s := sessionstore.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	notices := make(chan event.Event, 8)
	c := New(Options{
		Runner: &inboxDispatchRunner{inputs: make(chan string, 8)},
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice && e.Code == event.NoticeCodePeerMessageArrived {
				notices <- e
			}
		}),
		SessionDir: sessions, SessionPath: path, Peers: PeersOptions{Dir: peersDir},
	})
	t.Cleanup(func() {
		c.Close()
		c.autosaveWG.Wait()
	})
	peers, err := peermail.Open(peersDir)
	if err != nil {
		t.Fatal(err)
	}
	return &unreadFixture{c: c, peers: peers, key: sessionPeerKey(path), sender: "s-0000000000000001", dir: sessions, path: path, notices: notices}
}

func (f *unreadFixture) deliver(t *testing.T, id string) {
	t.Helper()
	if err := f.peers.Send(peermail.Message{
		ID: id, SentAt: time.Now().UTC(), Body: "look at the failing test",
		From: peermail.Party{SessionID: f.sender, Title: "other", Project: "alpha"},
		To:   peermail.Party{SessionID: f.key},
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *unreadFixture) unread(t *testing.T) bool {
	t.Helper()
	listed, err := sessionstore.ListSessions(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, si := range listed {
		if si.Path == f.path {
			return si.Unread
		}
	}
	t.Fatalf("%s is not listed", f.path)
	return false
}

func TestAMessageFromAnotherSessionLeavesTheReceiverUnreadAndSaysSo(t *testing.T) {
	f := newUnreadFixture(t)
	if f.unread(t) {
		t.Fatal("the fixture starts unread")
	}
	f.deliver(t, "m1")
	f.c.peerIngest()
	if !f.unread(t) {
		t.Fatal("a message waiting for the person left the session read")
	}
	select {
	case n := <-f.notices:
		if n.Audience != event.NoticeAudienceOperator || n.Level != event.LevelInfo {
			t.Fatalf("notice = %+v, want a quiet operator notice a window can refresh from", n)
		}
	case <-time.After(time.Second):
		t.Fatal("nothing told the window to look again")
	}
}

func TestOnlyAMessageNotSeenBeforeMakesTheSessionUnread(t *testing.T) {
	f := newUnreadFixture(t)
	f.deliver(t, "m1")
	f.c.peerIngest()
	<-f.notices
	if err := sessionstore.MarkSessionViewed(f.path, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.c.peerIngest()
	if f.unread(t) {
		t.Fatal("re-reading the same held message marked the session unread again")
	}
	select {
	case n := <-f.notices:
		t.Fatalf("an unchanged inbox announced itself again: %+v", n)
	case <-time.After(150 * time.Millisecond):
	}
}
