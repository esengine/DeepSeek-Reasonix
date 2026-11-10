package control

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/peermail"
)

func newPeerSession(t *testing.T, peersDir, name string) *Controller {
	t.Helper()
	setDispatchSwitch(t, true, false)
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "sessions", name+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	c := New(Options{
		Runner:     &inboxDispatchRunner{inputs: make(chan string, 8)},
		SessionDir: filepath.Dir(path), SessionPath: path, Peers: PeersOptions{Dir: peersDir},
	})
	t.Cleanup(func() {
		c.Close()
		c.autosaveWG.Wait()
	})
	c.peerIngest()
	return c
}

func TestASentMessageLandsHeldAtTheReceiverAndNeverReachesItsModel(t *testing.T) {
	peersDir := filepath.Join(testenv.TempDir(t), "peers")
	a := newPeerSession(t, peersDir, "alpha")
	b := newPeerSession(t, peersDir, "beta")
	a.peerIngest()
	peers := a.Peers()
	if len(peers) != 1 || peers[0].Key != b.peers.key {
		t.Fatalf("Peers() = %+v, want only the other session", peers)
	}
	rec, err := a.SendPeerMessage(peers[0].Key, "please look at the build")
	if err != nil {
		t.Fatal(err)
	}
	b.peerIngest()
	held := b.HeldPeerMessages()
	if len(held) != 1 || held[0].ID != rec.ID {
		t.Fatalf("receiver holds %+v, want the sent message", held)
	}
	if got := a.HeldPeerMessages(); len(got) != 0 {
		t.Fatalf("the sender holds %+v", got)
	}
}

func TestANameThatFitsTwoSessionsIsRefusedNotGuessed(t *testing.T) {
	live := []peermail.Live{
		{SessionID: "s-1", Title: "work", Project: "p", Inbound: "hold"},
		{SessionID: "s-2", Title: "Work", Project: "q", Inbound: "hold"},
	}
	if _, err := resolvePeer(live, "work"); !errors.Is(err, ErrPeerAmbiguous) {
		t.Fatalf("got %v, want ErrPeerAmbiguous", err)
	}
	if got, err := resolvePeer(live, "q/work"); err != nil || got.SessionID != "s-2" {
		t.Fatalf("project/title = %v %v", got, err)
	}
	if got, err := resolvePeer(live, "s-1"); err != nil || got.SessionID != "s-1" {
		t.Fatalf("exact key = %v %v", got, err)
	}
	if _, err := resolvePeer(live, "nobody"); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("got %v, want ErrPeerNotFound", err)
	}
}

func TestASessionCannotMessageItselfOrSendNothing(t *testing.T) {
	a := newPeerSession(t, filepath.Join(testenv.TempDir(t), "peers"), "alpha")
	if _, err := a.SendPeerMessage(a.peers.key, "hi"); !errors.Is(err, ErrPeerSelf) {
		t.Fatalf("self = %v", err)
	}
	if _, err := a.SendPeerMessage(a.peers.key, "  "); !errors.Is(err, ErrPeerEmpty) {
		t.Fatalf("empty = %v", err)
	}
}

func TestSendingIsRateLimitedAndARefusedSendSpendsNothing(t *testing.T) {
	peersDir := filepath.Join(testenv.TempDir(t), "peers")
	a := newPeerSession(t, peersDir, "alpha")
	b := newPeerSession(t, peersDir, "beta")
	a.peerIngest()
	for range 3 {
		if _, err := a.SendPeerMessage("nobody", "x"); !errors.Is(err, ErrPeerNotFound) {
			t.Fatal(err)
		}
	}
	for i := range peerSendBurst {
		if _, err := a.SendPeerMessage(b.peers.key, "m"); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if _, err := a.SendPeerMessage(b.peers.key, "m"); !errors.Is(err, ErrPeerRateLimited) {
		t.Fatalf("over the burst = %v", err)
	}
}

func TestAStaleHeartbeatIsNotListed(t *testing.T) {
	peersDir := filepath.Join(testenv.TempDir(t), "peers")
	a := newPeerSession(t, peersDir, "alpha")
	b := newPeerSession(t, peersDir, "beta")
	d, err := peermail.Open(peersDir)
	if err != nil {
		t.Fatal(err)
	}
	live, _ := d.ListLive(func(peermail.Live) bool { return true })
	for _, l := range live {
		if l.SessionID == b.peers.key {
			l.BeatAt = time.Now().Add(-2 * peerBeatTTL)
			if err := d.PublishLive(l); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := a.Peers(); len(got) != 0 {
		t.Fatalf("Peers() = %+v, want none", got)
	}
}

func TestTwoApprovalsOfOneMessageWriteOneAudit(t *testing.T) {
	f := newPeerFixture(t)
	f.send(t, "m1", "hello")
	f.c.peerIngest()
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for range 2 {
		wg.Go(func() {
			if f.c.ResolvePeerMessage("m1", true) == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("%d approvals succeeded, want 1", ok)
	}
	if n := countAudit(t, f.peersDir, "m1", peermail.DecisionApproved); n != 1 {
		t.Fatalf("approved audit rows = %d, want 1", n)
	}
}

func countAudit(t *testing.T, peersDir, id string, d peermail.Decision) int {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(peersDir, "audit", "*.jsonl"))
	n := 0
	for _, f := range files {
		data, _ := os.ReadFile(f)
		for line := range strings.SplitSeq(string(data), "\n") {
			if strings.Contains(line, `"id":"`+id+`"`) && strings.Contains(line, `"decision":"`+string(d)+`"`) {
				n++
			}
		}
	}
	return n
}

func TestPeerCommandSendsAndTheReceiverApprovesByIDFromItsOwnCommand(t *testing.T) {
	peersDir := filepath.Join(testenv.TempDir(t), "peers")
	a := newPeerSession(t, peersDir, "alpha")
	b := newPeerSession(t, peersDir, "beta")
	a.peerIngest()
	a.peerCommand("send " + b.peers.key + " look at the build")
	b.peerIngest()
	held := b.HeldPeerMessages()
	if len(held) != 1 || held[0].Body != "look at the build" {
		t.Fatalf("held = %+v", held)
	}
	b.peerCommand("deny " + held[0].ID)
	if got := b.HeldPeerMessages(); len(got) != 0 {
		t.Fatalf("still held: %+v", got)
	}
	if name, text, ok := splitPeerName(`"my title" go`); !ok || name != "my title" || text != "go" {
		t.Fatalf("quoted = %q %q %v", name, text, ok)
	}
	if _, _, ok := splitPeerName("alone"); ok {
		t.Fatal("a name without text must not parse")
	}
}

func TestASentMessageCarriesNoProcessWriterIdentity(t *testing.T) {
	peersDir := filepath.Join(testenv.TempDir(t), "peers")
	a := newPeerSession(t, peersDir, "alpha")
	b := newPeerSession(t, peersDir, "beta")
	a.peerIngest()
	if _, err := a.SendPeerMessage(b.peers.key, "hi"); err != nil {
		t.Fatal(err)
	}
	pending, _ := a.peers.dir.Load().Pending(b.peers.key)
	if len(pending) != 1 || pending[0].From.WriterID != "" {
		t.Fatalf("pending = %+v: a host-name-derived writer id can exceed the id limit and make every send invalid", pending)
	}
}
