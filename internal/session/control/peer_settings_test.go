package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
)

type toolCalls struct {
	mu    sync.Mutex
	calls [][2]bool
}

func (r *toolCalls) set(enabled, dispatch bool) {
	r.mu.Lock()
	r.calls = append(r.calls, [2]bool{enabled, dispatch})
	r.mu.Unlock()
}

func (r *toolCalls) last() ([2]bool, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return [2]bool{}, 0
	}
	return r.calls[len(r.calls)-1], len(r.calls)
}

type gateRunner struct {
	started chan struct{}
	release chan struct{}
}

func (r *gateRunner) Run(ctx context.Context, _ string) error {
	r.started <- struct{}{}
	select {
	case <-r.release:
	case <-ctx.Done():
	}
	return nil
}

type standbyFixture struct {
	c        *Controller
	runner   *gateRunner
	tools    *toolCalls
	peersDir string
	key      string
}

func newStandbyFixture(t *testing.T) *standbyFixture {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	if err := os.WriteFile(config.UserConfigPath(), []byte("[peers]\nenabled = false\ndispatch = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := testenv.TempDir(t)
	peersDir := filepath.Join(dir, "peers")
	path := filepath.Join(dir, "sessions", "me.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &gateRunner{started: make(chan struct{}, 4), release: make(chan struct{})}
	tools := &toolCalls{}
	c := New(Options{
		Runner: runner, Sink: event.FuncSink(func(event.Event) {}),
		SessionDir: filepath.Dir(path), SessionPath: path,
		Peers: PeersOptions{Dir: peersDir, Standby: true, Tools: tools.set},
	})
	t.Cleanup(func() {
		select {
		case <-runner.release:
		default:
			close(runner.release)
		}
		c.Close()
		c.autosaveWG.Wait()
	})
	return &standbyFixture{c: c, runner: runner, tools: tools, peersDir: peersDir, key: sessionPeerKey(path)}
}

func (f *standbyFixture) liveRecords() int {
	files, _ := filepath.Glob(filepath.Join(f.peersDir, "live", "*"))
	return len(files)
}

func yes() *bool { v := true; return &v }
func no() *bool  { v := false; return &v }

func TestAStandbySessionIsNotPublishedAndHasNoPeerTools(t *testing.T) {
	f := newStandbyFixture(t)
	if f.c.PeersEnabled() || f.liveRecords() != 0 {
		t.Fatal("a session that never turned messages on is reachable")
	}
	s := f.c.PeersSettings()
	if s.Enabled || !s.Available || s.Pending || s.ReleaseProjects == nil {
		t.Fatalf("settings of a fresh session: %+v", s)
	}
	if _, n := f.tools.last(); n != 0 {
		t.Fatal("the tool set moved without a switch")
	}
}

func TestTurningMessagesOnBetweenTurnsPublishesAtOnceAndWritesTheUserFile(t *testing.T) {
	f := newStandbyFixture(t)
	s, err := f.c.SavePeersSettings(PeersPatch{Enabled: yes(), Dispatch: yes()})
	if err != nil {
		t.Fatal(err)
	}
	if !f.c.PeersEnabled() || f.liveRecords() != 1 || s.Pending {
		t.Fatalf("enabled=%v live=%d settings=%+v", f.c.PeersEnabled(), f.liveRecords(), s)
	}
	if last, _ := f.tools.last(); last != [2]bool{true, true} {
		t.Fatalf("tool set after enabling: %v", last)
	}
	if got := config.UserPeers(); !got.Enabled || !got.Dispatch {
		t.Fatalf("user file: %+v", got)
	}
}

func TestTurningMessagesOffWithdrawsTheSessionAndItsTools(t *testing.T) {
	f := newStandbyFixture(t)
	if _, err := f.c.SavePeersSettings(PeersPatch{Enabled: yes(), Dispatch: yes()}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.SavePeersSettings(PeersPatch{Enabled: no()}); err != nil {
		t.Fatal(err)
	}
	if f.c.PeersEnabled() || f.liveRecords() != 0 {
		t.Fatal("a session that turned messages off is still reachable")
	}
	if last, _ := f.tools.last(); last != [2]bool{false, false} {
		t.Fatalf("tool set after disabling: %v", last)
	}
	if len(f.c.HeldPeerMessages()) != 0 || len(f.c.Peers()) != 0 {
		t.Fatal("a disabled session still lists peers or held messages")
	}
	if _, err := f.c.SendPeerMessage("anyone", "hi"); err == nil {
		t.Fatal("a disabled session sent a message")
	}
}

// A save made while a turn is running changes nothing until the next turn
// starts: the tool block never moves inside a request.
func TestASaveDuringATurnWaitsForTheNextTurn(t *testing.T) {
	f := newStandbyFixture(t)
	f.c.Submit("work")
	select {
	case <-f.runner.started:
	case <-time.After(testenv.Budget(t)):
		t.Fatal("the turn never started")
	}
	s, err := f.c.SavePeersSettings(PeersPatch{Enabled: yes()})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Pending || f.c.PeersEnabled() {
		t.Fatalf("applied mid-turn: pending=%v enabled=%v", s.Pending, f.c.PeersEnabled())
	}
	if _, n := f.tools.last(); n != 0 {
		t.Fatal("the tool set moved mid-turn")
	}
	close(f.runner.release)
	waitFor(t, "the first turn ends", func() bool { return !f.c.Running() })
	f.c.Submit("next")
	waitFor(t, "the next turn applies the save", func() bool { _, n := f.tools.last(); return n > 0 })
	if last, _ := f.tools.last(); last != [2]bool{true, false} {
		t.Fatalf("tool set at the next turn: %v", last)
	}
}

func TestReleaseProjectsMustBeAbsolutePathsAndAreReplacedWhole(t *testing.T) {
	f := newStandbyFixture(t)
	bad := []string{"relative/dir"}
	if _, err := f.c.SavePeersSettings(PeersPatch{ReleaseProjects: &bad}); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("a relative project was accepted: %v", err)
	}
	root := testenv.TempDir(t)
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	both := []string{a, b}
	if _, err := f.c.SavePeersSettings(PeersPatch{ReleaseProjects: &both}); err != nil {
		t.Fatal(err)
	}
	one := []string{b}
	s, err := f.c.SavePeersSettings(PeersPatch{ReleaseProjects: &one})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.ReleaseProjects) != 1 || s.ReleaseProjects[0].Path != b || s.ReleaseProjects[0].Name != "b" {
		t.Fatalf("revoking a project: %+v", s.ReleaseProjects)
	}
	if f.c.PeersEnabled() {
		t.Fatal("saving a project list turned messages on")
	}
}

func TestRememberingAnApprovalReleasesThisProjectAndDenyCannotRemember(t *testing.T) {
	f := newPeerFixture(t)
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	f.c.workspaceRoot = testenv.TempDir(t)
	f.send(t, "m1", "first")
	f.send(t, "m2", "second")
	f.c.peerIngest()
	if err := f.c.ResolvePeerMessageRemembering("m1", false, PeerRememberProject); !errors.Is(err, ErrPeerRememberDenied) {
		t.Fatalf("a remembered denial: %v", err)
	}
	if config.LoadForEdit(config.UserConfigPath()).PeersReleasedProject(f.c.WorkspaceRoot()) {
		t.Fatal("a refused remember still wrote the project")
	}
	if err := f.c.ResolvePeerMessageRemembering("m2", true, PeerRememberProject); err != nil {
		t.Fatal(err)
	}
	if !config.LoadForEdit(config.UserConfigPath()).PeersReleasedProject(f.c.WorkspaceRoot()) {
		t.Fatal("the project was not released")
	}
	if len(f.c.HeldPeerMessages()) != 1 {
		t.Fatalf("remembering also settled the other message: %d held", len(f.c.HeldPeerMessages()))
	}
}

func TestRememberingAMessageThatIsNotHeldWritesNothing(t *testing.T) {
	f := newPeerFixture(t)
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	f.c.workspaceRoot = testenv.TempDir(t)
	if err := f.c.ResolvePeerMessageRemembering("nope", true, PeerRememberProject); !errors.Is(err, ErrPeerNotHeld) {
		t.Fatalf("unknown id: %v", err)
	}
	if config.LoadForEdit(config.UserConfigPath()).PeersReleasedProject(f.c.WorkspaceRoot()) {
		t.Fatal("an unknown message released the project")
	}
}

func TestTheDispatchCeilingsAreSettingsTheUserCanRaiseRemoveAndNotMakeNegative(t *testing.T) {
	f := newStandbyFixture(t)
	s := f.c.PeersSettings()
	if s.MaxDispatchDepth != config.DefaultMaxDispatchDepth || s.MaxDispatchedSessions != config.DefaultMaxDispatchedSessions {
		t.Fatalf("defaults = depth %d sessions %d, want %d and %d", s.MaxDispatchDepth, s.MaxDispatchedSessions, config.DefaultMaxDispatchDepth, config.DefaultMaxDispatchedSessions)
	}
	three, none, below := 3, 0, -1
	got, err := f.c.SavePeersSettings(PeersPatch{MaxDispatchDepth: &three, MaxDispatchedSessions: &none})
	if err != nil || got.MaxDispatchDepth != 3 || got.MaxDispatchedSessions != 0 {
		t.Fatalf("saved = %+v, %v; want depth 3 and no session ceiling", got, err)
	}
	if again := f.c.PeersSettings(); again.MaxDispatchDepth != 3 || again.MaxDispatchedSessions != 0 {
		t.Fatalf("read back = %+v; a 0 ceiling must survive a reload instead of snapping back to the default", again)
	}
	if _, err := f.c.SavePeersSettings(PeersPatch{MaxDispatchDepth: &below}); !errors.Is(err, ErrPeerCeilingInvalid) {
		t.Fatalf("a negative ceiling = %v, want ErrPeerCeilingInvalid", err)
	}
	if after := f.c.PeersSettings(); after.MaxDispatchDepth != 3 {
		t.Fatal("a refused ceiling changed the file")
	}
}
