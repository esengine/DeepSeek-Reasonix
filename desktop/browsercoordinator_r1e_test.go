package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/desktop/internal/browseripc"
)

// failingWriteCloser simulates a broken pipe: every write fails.
type failingWriteCloser struct{}

func (failingWriteCloser) Write(p []byte) (int, error) { return 0, errors.New("broken pipe") }
func (failingWriteCloser) Close() error                { return nil }

// TestBrowserCoordinatorWriteFailureFailsClosed: a broken write pipe must fail
// the companion generation synchronously (state crashed, writer cleared,
// pending drained) without waiting for the reader goroutine's EOF.
func TestBrowserCoordinatorWriteFailureFailsClosed(t *testing.T) {
	b := newStateTestCoordinator()
	b.mu.Lock()
	b.state = browserStarting
	b.writer = failingWriteCloser{}
	b.mu.Unlock()

	err := b.callDirect(context.Background(), "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	if err == nil {
		t.Fatal("write failure must surface as an error")
	}
	b.mu.Lock()
	state := b.state
	writer := b.writer
	pending := len(b.pending)
	b.mu.Unlock()
	if state != browserCrashed {
		t.Fatalf("state = %q, want crashed (write failure must fail closed)", state)
	}
	if writer != nil {
		t.Fatal("writer must be cleared by fail-closed")
	}
	if pending != 0 {
		t.Fatalf("pending = %d, want 0", pending)
	}
	// A subsequent restore decision must see the process as gone.
	if !b.processGone() {
		t.Fatal("processGone must be true after fail-closed")
	}
}

// TestBrowserStateRestoreAbortsOnWriteFailure: the restore pass treats a
// write-side failure (broken pipe noticed by the writer, not the reader) as a
// process-level abort: no partial mirror is published or persisted, and the
// intact desired state on disk survives.
func TestBrowserStateRestoreAbortsOnWriteFailure(t *testing.T) {
	statePath := filepath.Join(desktopConfigDir(), browserStateFileName)
	store := &browserStateStore{path: statePath}
	seed := newStateTestCoordinator()
	seed.updateTabMirror("chat-1", "t-old-1", "https://example.com", "Example", false, 1)
	seed.updateTabMirror("chat-1", "t-old-2", "https://other.com", "Other", false, 1)
	seed.setActiveTabMirror("chat-1", "t-old-1")
	store.syncFromCoordinator(seed)
	// Seeded state must not leak into tests that run after this one (they
	// share the global test home).
	t.Cleanup(func() { _ = os.Remove(statePath) })

	c := newStateTestCoordinator()
	c.opts.now = time.Now
	store2 := &browserStateStore{path: filepath.Join(desktopConfigDir(), browserStateFileName)}
	c.tabsChanged = func() { store2.syncFromCoordinator(c) }
	t.Cleanup(c.Close)
	// The writer is broken before the restore starts: equivalent to the
	// companion dying between handshake and first tab.open, with the write
	// path noticing first.
	c.mu.Lock()
	c.state = browserStarting
	c.writer = failingWriteCloser{}
	c.mu.Unlock()

	c.restoreBrowserState()

	c.mu.Lock()
	_, published := c.owners["chat-1"]
	_, barrier := c.restoreBarrier["chat-1"]
	passActive := c.restorePassActive
	state := c.state
	c.mu.Unlock()
	if published {
		t.Fatal("partial mirror was published after the write failure")
	}
	if barrier {
		t.Fatal("restore barrier leaked after the aborted pass")
	}
	if passActive {
		t.Fatal("restore pass flag leaked after the abort")
	}
	if state != browserCrashed {
		t.Fatalf("state = %q, want crashed", state)
	}
	if got := store2.generation; got != 0 {
		t.Fatalf("aborted pass persisted %d times, want 0", got)
	}
	disk := loadBrowserStateFile()
	owner, ok := disk.Owners["chat-1"]
	if !ok || len(owner.Tabs) != 2 {
		t.Fatalf("desired state on disk was replaced: %+v", owner)
	}
}

// TestBrowserCoordinatorStaleOwnerRequestsRejected: after a deletion, requests
// addressed to the deleted chat are stale and must not reach the companion;
// only tab.open (explicit reopen) and owner.remove (cleanup) are allowed.
func TestBrowserCoordinatorStaleOwnerRequestsRejected(t *testing.T) {
	b := newStateTestCoordinator()
	b.mu.Lock()
	b.tombstonedOwners["chat-1"] = true
	b.mu.Unlock()

	// Stale operations are rejected before any write.
	err := b.callDirect(context.Background(), "chat-1", "owner.activate", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	if err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("owner.activate to deleted chat: err = %v, want deletion rejection", err)
	}
	err = b.callDirect(context.Background(), "chat-1", "tab.navigate", browseripc.TabNavigateParams{
		OwnerID: "chat-1", TabID: "t1", URL: "https://example.com",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("tab.navigate to deleted chat: err = %v, want deletion rejection", err)
	}
	// Cleanup and explicit reopen pass the gate (they fail later on the
	// missing process, but not with the deletion error).
	err = b.callDirect(context.Background(), "chat-1", "owner.remove", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	if err != nil && strings.Contains(err.Error(), "deleted") {
		t.Fatalf("owner.remove must be allowed for tombstoned chats: %v", err)
	}
	err = b.callDirect(context.Background(), "chat-1", "tab.open", browseripc.TabOpenParams{
		OwnerID: "chat-1", URL: "https://fresh.com", Disposition: browseripc.DispositionForeground,
	}, nil)
	if err != nil && strings.Contains(err.Error(), "deleted") {
		t.Fatalf("tab.open must be allowed as explicit reopen: %v", err)
	}

	// After a reopen clears the tombstone, stale rejection is gone.
	b.mu.Lock()
	delete(b.tombstonedOwners, "chat-1")
	b.mu.Unlock()
	err = b.callDirect(context.Background(), "chat-1", "owner.activate", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	if err != nil && strings.Contains(err.Error(), "deleted") {
		t.Fatalf("owner.activate after reopen must not be rejected: %v", err)
	}
}

// TestBrowserCoordinatorFailClosedIdempotentAndGenerationIsolated: the crash
// teardown runs exactly once per generation, a stale generation's death is
// ignored entirely, and the captured process is actually killed.
func TestBrowserCoordinatorFailClosedIdempotentAndGenerationIsolated(t *testing.T) {
	b := newStateTestCoordinator()
	var crashes atomic.Int64
	b.opts.onCrash = func() { crashes.Add(1) }
	// A real helper-process companion ("hang" stays alive until killed).
	cmd := fakeBrowserCompanionCommand("hang")
	cmd.Env = append(cmd.Env, "GO_WANT_BROWSER_FAKE=1", "BROWSER_FAKE_MODE=hang")
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn fake companion: %v", err)
	}
	b.mu.Lock()
	b.procToken = 1
	b.state = browserStarting
	b.writer = failingWriteCloser{}
	b.cmd = cmd
	waitCh := make(chan error, 1)
	b.waitCh = waitCh
	waited := make(chan struct{})
	go func() {
		waitCh <- cmd.Wait()
		close(waited)
	}()
	b.mu.Unlock()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	// First teardown for the current generation: runs fully, kills the
	// process, fires the callback once.
	b.failClosed(1, errors.New("boom"))
	if crashes.Load() != 1 {
		t.Fatalf("onCrash fired %d times, want 1", crashes.Load())
	}
	if b.State() != browserCrashed {
		t.Fatalf("state = %q, want crashed", b.State())
	}
	// The captured process must actually be dead (waited is test-owned;
	// failClosed consumes the shared waitCh itself).
	select {
	case <-waited:
	case <-time.After(3 * time.Second):
		t.Fatal("companion process was not killed by failClosed")
	}
	// Second teardown for the same generation: idempotent no-op.
	b.failClosed(1, errors.New("boom again"))
	if crashes.Load() != 1 {
		t.Fatalf("onCrash fired %d times after second teardown, want still 1", crashes.Load())
	}
	// A stale generation's death must not touch the current state.
	b.failClosed(99, errors.New("stale death"))
	if b.State() != browserCrashed {
		t.Fatalf("stale death changed state to %q", b.State())
	}
	if crashes.Load() != 1 {
		t.Fatalf("stale death fired onCrash (%d), want still 1", crashes.Load())
	}
}

// TestBrowserCoordinatorStaleDeathDoesNotClearNewGeneration: a late EOF from
// an OLD process must not clear a NEWER process's writer or pending calls.
func TestBrowserCoordinatorStaleDeathDoesNotClearNewGeneration(t *testing.T) {
	b := newStateTestCoordinator()
	b.mu.Lock()
	b.procToken = 2 // newer process already spawned
	b.state = browserStarting
	b.writer = failingWriteCloser{}
	b.pending["r-stale-1"] = &pendingBrowserCall{reply: make(chan browseripc.Response, 1)}
	b.mu.Unlock()

	// The old process (token 1) dies late.
	b.failClosed(1, io.EOF)
	b.mu.Lock()
	state := b.state
	writer := b.writer
	pending := len(b.pending)
	b.mu.Unlock()
	if state != browserStarting || writer == nil || pending != 1 {
		t.Fatalf("stale death cleared the new generation: state=%q writer=%v pending=%d", state, writer != nil, pending)
	}
}

// TestBrowserCoordinatorOwnerEpochLinearizesDeletion: a request that passed
// the tombstone gate before a deletion must be dropped at the write gate when
// the deletion lands in between (the gate re-checks the epoch under writeMu,
// and RemoveOwner increments the epoch under the same lock), and a tab.open
// response from before the deletion must not clear the tombstone.
func TestBrowserCoordinatorOwnerEpochLinearizesDeletion(t *testing.T) {
	b := newStateTestCoordinator()
	b.mu.Lock()
	b.procToken = 1
	b.state = browserStarting
	b.mu.Unlock()

	// Write gate: a stale epoch is dropped before any byte reaches the wire.
	counting := &countingWriteCloser{}
	b.mu.Lock()
	b.ownerEpoch["chat-1"] = 1 // one deletion already happened
	b.mu.Unlock()
	staleReq := browseripc.Request{
		ProtocolVersion: browseripc.ProtocolVersion,
		RequestID:       "r-stale",
		OwnerID:         "chat-1",
		Method:          "owner.activate",
		Params:          mustFakeJSON(browseripc.OwnerParams{OwnerID: "chat-1"}),
	}
	err := b.writeRequest(counting, staleReq, 0) // captured before the deletion
	if err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("stale-epoch frame reached the wire gate: %v", err)
	}
	if counting.writes != 0 {
		t.Fatalf("stale frame was written (%d writes)", counting.writes)
	}
	// A current-epoch frame passes.
	currentReq := browseripc.Request{
		ProtocolVersion: browseripc.ProtocolVersion,
		RequestID:       "r-current",
		OwnerID:         "chat-1",
		Method:          "owner.activate",
		Params:          mustFakeJSON(browseripc.OwnerParams{OwnerID: "chat-1"}),
	}
	if err := b.writeRequest(counting, currentReq, 1); err != nil {
		t.Fatalf("current-epoch frame dropped: %v", err)
	}
	if counting.writes != 2 {
		t.Fatalf("current frame not written (%d writes)", counting.writes)
	}

	// Response path: a tab.open response issued BEFORE the deletion (old
	// epoch) must not clear the tombstone; one issued after (current epoch)
	// may, and recreates the owner.
	b.mu.Lock()
	b.tombstonedOwners["chat-1"] = true
	b.mu.Unlock()
	openResp := browseripc.Response{Result: mustFakeJSON(browseripc.TabInfo{
		TabID: "t1", URL: "https://example.com", Title: "X", Generation: 1, Active: true,
	})}
	b.updateMirrorFromResponse(browseripc.Request{Method: "tab.open", OwnerID: "chat-1"}, openResp, 0)
	b.mu.Lock()
	_, tombstoned := b.tombstonedOwners["chat-1"]
	b.mu.Unlock()
	if !tombstoned {
		t.Fatal("in-flight tab.open response cleared the deletion tombstone")
	}
	b.updateMirrorFromResponse(browseripc.Request{Method: "tab.open", OwnerID: "chat-1"}, openResp, 1)
	b.mu.Lock()
	_, tombstoned = b.tombstonedOwners["chat-1"]
	_, exists := b.owners["chat-1"]
	b.mu.Unlock()
	if tombstoned || !exists {
		t.Fatalf("post-deletion reopen did not clear the tombstone: tombstoned=%v exists=%v", tombstoned, exists)
	}
}

// countingWriteCloser counts successful writes.
type countingWriteCloser struct {
	writes int
	mu     sync.Mutex
}

func (c *countingWriteCloser) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.writes++
	c.mu.Unlock()
	return len(p), nil
}

func (c *countingWriteCloser) Close() error { return nil }

// TestBrowserStateRestoreActivateWriteFailurePersistsNothing: an EPIPE on
// the final tab.activate must fail the whole pass — no final persist for a
// dead process. The fake companion answers hello and the restore's tab.open
// calls, so the failure lands deterministically on the activate frame: hello
// (2 writes) + two tab.open frames (4 writes) succeed; write 7 — the
// tab.activate header — fails.
func TestBrowserStateRestoreActivateWriteFailurePersistsNothing(t *testing.T) {
	statePath := filepath.Join(desktopConfigDir(), browserStateFileName)
	store := &browserStateStore{path: statePath}
	seed := newStateTestCoordinator()
	seed.updateTabMirror("chat-1", "t-old-1", "https://example.com", "Example", false, 1)
	seed.updateTabMirror("chat-1", "t-old-2", "https://other.com", "Other", false, 1)
	seed.setActiveTabMirror("chat-1", "t-old-1")
	store.syncFromCoordinator(seed)
	t.Cleanup(func() { _ = os.Remove(statePath) })

	c := newStateTestCoordinator()
	c.opts.resolveBinary = func() (string, error) { return "/fake", nil }
	c.opts.now = time.Now
	persistStore := &browserStateStore{path: statePath}
	c.tabsChanged = func() { persistStore.syncFromCoordinator(c) }
	c.opts.spawn = func(ctx context.Context, path string, env []string) (*exec.Cmd, io.WriteCloser, io.Reader, io.Reader, error) {
		cmd := fakeBrowserCompanionCommand("events-first")
		cmd.Env = append(env, "GO_WANT_BROWSER_FAKE=1", "BROWSER_FAKE_MODE=events-first")
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, nil, nil, nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, nil, nil, nil, err
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, nil, nil, nil, err
		}
		return cmd, &countedFailingWriteCloser{failAfter: 6, inner: stdin}, stdout, stderr, nil
	}
	t.Cleanup(c.Close)

	ctx := context.Background()
	var res json.RawMessage
	if err := c.Call(ctx, "chat-1", "owner.activate", browseripc.OwnerParams{OwnerID: "chat-1"}, &res); err == nil {
		t.Fatal("start with an activate-time EPIPE should fail")
	}
	c.mu.Lock()
	passActive := c.restorePassActive
	state := c.state
	c.mu.Unlock()
	if passActive {
		t.Fatal("restore pass flag leaked")
	}
	if state != browserCrashed {
		t.Fatalf("state = %q, want crashed (activate EPIPE)", state)
	}
	// The mirrors were published in memory (that is fine), but the pass must
	// not have persisted them.
	disk := loadBrowserStateFile()
	owner, ok := disk.Owners["chat-1"]
	if !ok || len(owner.Tabs) != 2 {
		t.Fatalf("desired state on disk was replaced: %+v", owner)
	}
	if got := persistStore.generation; got != 0 {
		t.Fatalf("pass persisted %d times, want 0", got)
	}
}

// TestBrowserCoordinatorFrameLockDeterministic: two deterministic subtests.
// Without the frame lock, a two-phase barrier writer forces every header onto
// the wire before any payload (guaranteed corruption); with the frame lock
// (callDirect) the same concurrency leaves every frame intact. The pair proves
// the frame lock is what prevents interleaving.
func TestBrowserCoordinatorFrameLockDeterministic(t *testing.T) {
	t.Run("without lock stream corrupts", func(t *testing.T) {
		const writers = 16
		adv := newTwoPhaseBarrierWriter(writers)
		var wg sync.WaitGroup
		for i := range writers {
			wg.Go(func() {
				req := browseripc.Request{
					ProtocolVersion: browseripc.ProtocolVersion,
					RequestID:       fmt.Sprintf("r-%d", i),
					OwnerID:         "chat-1",
					Method:          "tab.list",
					Params:          mustFakeJSON(browseripc.OwnerParams{OwnerID: "chat-1"}),
				}
				_ = browseripc.WriteRequest(adv, req) // deliberately no frame lock
			})
		}
		wg.Wait()
		data := adv.bytes()
		if framesParseCleanly(data, writers) {
			t.Fatal("unlocked concurrent writes produced clean frames: the test cannot detect interleaving")
		}
	})

	t.Run("with lock frames intact", func(t *testing.T) {
		b := newStateTestCoordinator()
		const writers = 16
		writer := &lockProbeWriter{b: b}
		b.mu.Lock()
		b.state = browserReady
		b.writer = writer
		b.opts.responseTimeout = 200 * time.Millisecond
		b.mu.Unlock()

		var wg sync.WaitGroup
		for range writers {
			wg.Go(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				defer cancel()
				_ = b.callDirect(ctx, "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
			})
		}
		wg.Wait()
		if !framesParseCleanly(writer.bytes(), writers) {
			t.Fatal("frame lock failed to keep concurrent frames intact")
		}
		// The frame lock must be HELD for the whole frame write: if the
		// production writeMu is removed, the probe inside Write succeeds and
		// this assertion fails deterministically — no scheduling luck needed.
		if !writer.lockHeld.Load() {
			t.Fatal("writeRequest did not hold writeMu during the frame write")
		}
	})
}

// framesParseCleanly validates that every frame in data is a complete
// 4-byte-prefixed JSON payload with a unique requestId.
func framesParseCleanly(data []byte, want int) bool {
	seen := map[string]bool{}
	offset := 0
	for offset < len(data) {
		if offset+4 > len(data) {
			return false
		}
		length := int(data[offset])<<24 | int(data[offset+1])<<16 | int(data[offset+2])<<8 | int(data[offset+3])
		offset += 4
		if offset+length > len(data) {
			return false
		}
		payload := data[offset : offset+length]
		offset += length
		var req struct {
			RequestID string `json:"requestId"`
		}
		if err := json.Unmarshal(payload, &req); err != nil {
			return false
		}
		if req.RequestID == "" || seen[req.RequestID] {
			return false
		}
		seen[req.RequestID] = true
	}
	return len(seen) == want
}

// twoPhaseBarrierWriter is deterministic WITHOUT a frame lock: phase 1 parks
// every header write until ALL writers have a header pending, then writes all
// headers; phase 2 parks every payload write until ALL headers are on the
// wire. The stream is therefore headerA..headerN payloadA..payloadN — the
// parser reads frame B's length bytes as frame A's payload. It must never be
// used under a serializing lock (writers would never all reach phase 1).
type twoPhaseBarrierWriter struct {
	mu             sync.Mutex
	buf            bytes.Buffer
	writers        int
	atHeader       int
	headerRelease  chan struct{}
	headersWritten int
	payloadRelease chan struct{}
}

func newTwoPhaseBarrierWriter(writers int) *twoPhaseBarrierWriter {
	return &twoPhaseBarrierWriter{
		writers:        writers,
		headerRelease:  make(chan struct{}),
		payloadRelease: make(chan struct{}),
	}
}

func (w *twoPhaseBarrierWriter) Write(p []byte) (int, error) {
	if len(p) == 4 {
		w.mu.Lock()
		w.atHeader++
		if w.atHeader == w.writers {
			close(w.headerRelease)
		}
		headerCh := w.headerRelease
		w.mu.Unlock()
		<-headerCh
		w.mu.Lock()
		_, _ = w.buf.Write(p)
		w.headersWritten++
		if w.headersWritten == w.writers {
			close(w.payloadRelease)
		}
		payloadCh := w.payloadRelease
		w.mu.Unlock()
		<-payloadCh
		return len(p), nil
	}
	w.mu.Lock()
	payloadCh := w.payloadRelease
	w.mu.Unlock()
	<-payloadCh
	w.mu.Lock()
	_, _ = w.buf.Write(p)
	w.mu.Unlock()
	return len(p), nil
}

func (w *twoPhaseBarrierWriter) Close() error { return nil }

func (w *twoPhaseBarrierWriter) bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...)
}

// countedFailingWriteCloser fails after N successful writes, wrapping an
// inner writer (the fake companion's stdin).
type countedFailingWriteCloser struct {
	failAfter int
	count     int
	inner     io.WriteCloser
	mu        sync.Mutex
}

func (c *countedFailingWriteCloser) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.count++
	fail := c.count > c.failAfter
	c.mu.Unlock()
	if fail {
		return 0, errors.New("broken pipe")
	}
	if c.inner == nil {
		return len(p), nil
	}
	return c.inner.Write(p)
}

func (c *countedFailingWriteCloser) Close() error {
	if c.inner != nil {
		return c.inner.Close()
	}
	return nil
}

// framesParseCleanly validates that every frame in data is a complete

// TestBrowserCoordinatorOldWriterEPIPEDoesNotKillNewToken: a late EPIPE from
// an OLD writer must fail the OLD generation — never the newer process that
// replaced it. The request goes through the real callDirect capture chain:
// the writer swaps in a new process generation between the epoch capture and
// the write, so only the captured-token path can survive.
func TestBrowserCoordinatorOldWriterEPIPEDoesNotKillNewToken(t *testing.T) {
	b := newStateTestCoordinator()
	var crashes atomic.Int64
	b.opts.onCrash = func() { crashes.Add(1) }
	b.mu.Lock()
	b.procToken = 1
	b.state = browserStarting
	swapper := &tokenSwapEPIPEWriter{b: b}
	b.writer = swapper
	b.mu.Unlock()

	err := b.callDirect(context.Background(), "chat-1", "owner.activate", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	if err == nil {
		t.Fatal("write failure must surface as an error")
	}
	b.mu.Lock()
	state := b.state
	writer := b.writer
	pending := len(b.pending)
	procToken := b.procToken
	b.mu.Unlock()
	if procToken != 2 {
		t.Fatalf("procToken = %d, want 2 (new generation installed)", procToken)
	}
	if state != browserStarting || writer == nil || pending != 0 {
		t.Fatalf("old-writer EPIPE tore down the new generation: state=%q writer=%v pending=%d", state, writer != nil, pending)
	}
	if crashes.Load() != 0 {
		t.Fatalf("old-writer EPIPE fired onCrash (%d), want 0", crashes.Load())
	}
}

// tokenSwapEPIPEWriter simulates the old process dying inside a write: on the
// first Write it installs a newer generation (procToken 2, fresh writer), then
// returns EPIPE. Only failClosed with the CAPTURED token (1) can survive this.
type tokenSwapEPIPEWriter struct {
	b     *browserCoordinator
	first atomic.Bool
}

func (w *tokenSwapEPIPEWriter) Write(p []byte) (int, error) {
	if w.first.CompareAndSwap(false, true) {
		w.b.mu.Lock()
		w.b.procToken = 2
		w.b.state = browserStarting
		w.b.writer = w // the new generation's writer
		w.b.mu.Unlock()
	}
	return 0, errors.New("broken pipe")
}

func (w *tokenSwapEPIPEWriter) Close() error { return nil }

// TestBrowserCoordinatorEpochRejectionDoesNotKillProcess: a stale-owner
// rejection at the write gate is a normal refusal — the healthy companion
// must survive it (no failClosed, no crash callback), and no stale byte
// reaches the wire.
func TestBrowserCoordinatorEpochRejectionDoesNotKillProcess(t *testing.T) {
	b := newStateTestCoordinator()
	var crashes atomic.Int64
	b.opts.onCrash = func() { crashes.Add(1) }
	b.mu.Lock()
	b.procToken = 1
	b.state = browserStarting
	b.ownerEpoch["chat-1"] = 1 // deletion already happened
	b.mu.Unlock()

	counting := &countingWriteCloser{}
	b.mu.Lock()
	b.writer = counting
	b.mu.Unlock()
	staleReq := browseripc.Request{
		ProtocolVersion: browseripc.ProtocolVersion,
		RequestID:       "r-stale",
		OwnerID:         "chat-1",
		Method:          "owner.activate",
		Params:          mustFakeJSON(browseripc.OwnerParams{OwnerID: "chat-1"}),
	}
	err := b.writeRequest(counting, staleReq, 0)
	if err == nil || !errors.Is(err, errBrowserStaleOwner) {
		t.Fatalf("write gate err = %v, want errBrowserStaleOwner", err)
	}
	b.mu.Lock()
	b.tombstonedOwners["chat-1"] = true
	b.mu.Unlock()
	err = b.callDirect(context.Background(), "chat-1", "owner.activate", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	if err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("callDirect err = %v, want deletion rejection", err)
	}
	b.mu.Lock()
	state := b.state
	writer := b.writer
	b.mu.Unlock()
	if state != browserStarting || writer == nil {
		t.Fatalf("stale rejection killed the healthy process: state=%q writer=%v", state, writer != nil)
	}
	if crashes.Load() != 0 {
		t.Fatalf("stale rejection fired onCrash (%d), want 0", crashes.Load())
	}
	if counting.writes != 0 {
		t.Fatalf("stale frame reached the wire (%d writes)", counting.writes)
	}
}

// TestBrowserCoordinatorOldListResponseDoesNotResurrectOwner: the unified
// epoch gate rejects ALL stale responses — a tab.list from before the
// deletion must not recreate the owner through replaceOwnerMirror.
func TestBrowserCoordinatorOldListResponseDoesNotResurrectOwner(t *testing.T) {
	b := newStateTestCoordinator()
	b.mu.Lock()
	b.tombstonedOwners["chat-1"] = true
	b.ownerEpoch["chat-1"] = 1
	b.mu.Unlock()

	listResp := browseripc.Response{Result: mustFakeJSON(browseripc.TabListResult{
		Tabs: []browseripc.TabInfo{{TabID: "t1", URL: "https://example.com", Title: "X", Generation: 1, Active: true}},
	})}
	b.updateMirrorFromResponse(browseripc.Request{Method: "tab.list", OwnerID: "chat-1"}, listResp, 0)
	b.mu.Lock()
	_, exists := b.owners["chat-1"]
	_, tombstoned := b.tombstonedOwners["chat-1"]
	b.mu.Unlock()
	if exists {
		t.Fatal("stale tab.list response resurrected the deleted owner")
	}
	if !tombstoned {
		t.Fatal("stale tab.list response cleared the tombstone")
	}
}

// TestBrowserCoordinatorStaleNonTabEventsDoNotReachSink: the unified token
// gate covers EVERY event — permission.request, downloads, takeover notices
// and renderer crashes from a stale generation must not reach the sink.
func TestBrowserCoordinatorStaleNonTabEventsDoNotReachSink(t *testing.T) {
	b := newStateTestCoordinator()
	events := make(chan browseripc.Event, 8)
	b.opts.events = func(ev browseripc.Event) { events <- ev }
	b.mu.Lock()
	b.procToken = 2
	b.mu.Unlock()

	for _, name := range []string{"permission.request", "download", "agent.takeover", "renderer.crash", "cdp.detach"} {
		b.handleEvent(1, browseripc.Event{
			ProtocolVersion: browseripc.ProtocolVersion,
			Event:           browseripc.EventBody{Name: name, OwnerID: "chat-1", Data: mustFakeJSON(map[string]string{"k": "v"})},
		})
	}
	select {
	case ev := <-events:
		t.Fatalf("stale %s event reached the sink", ev.Event.Name)
	case <-time.After(100 * time.Millisecond):
	}
	// The current generation's events still flow.
	b.handleEvent(2, browseripc.Event{
		ProtocolVersion: browseripc.ProtocolVersion,
		Event:           browseripc.EventBody{Name: "permission.request", OwnerID: "chat-1", Data: mustFakeJSON(map[string]string{"k": "v"})},
	})
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("current-generation event did not reach the sink")
	}
}

// TestBrowserCoordinatorReadLoopDropsFramesAfterTokenSwitch: a frame that
// arrives AFTER a newer process was installed while this reader was blocked in
// ReadFrame must be dropped entirely — it must not reach the sink or the
// mirror (the post-read token re-check).
func TestBrowserCoordinatorReadLoopDropsFramesAfterTokenSwitch(t *testing.T) {
	b := newStateTestCoordinator()
	events := make(chan browseripc.Event, 4)
	b.opts.events = func(ev browseripc.Event) { events <- ev }
	b.mu.Lock()
	b.procToken = 1
	b.state = browserStarting
	b.mu.Unlock()

	pr, pw := io.Pipe()
	entered := make(chan struct{})
	reader := &enterNotifyReader{r: pr, entered: entered}
	go b.readLoop(1, bufio.NewReader(reader))

	// Wait until ReadFrame is blocked, then install a newer generation.
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("reader never entered ReadFrame")
	}
	b.mu.Lock()
	b.procToken = 2
	b.mu.Unlock()

	// The old process's buffered frame arrives after the switch.
	ev := browseripc.Event{
		ProtocolVersion: browseripc.ProtocolVersion,
		Event:           browseripc.EventBody{Name: "tab.changed", OwnerID: "chat-1", Data: mustFakeJSON(browseripc.TabChangedEventData{OwnerID: "chat-1", TabID: "t1", URL: "https://example.com", Title: "X", Active: true, Generation: 1})},
	}
	payload, _ := json.Marshal(ev)
	_ = browseripc.WriteFrame(pw, payload)
	_ = pw.Close()

	select {
	case got := <-events:
		t.Fatalf("stale frame reached the sink: %+v", got)
	case <-time.After(300 * time.Millisecond):
	}
	b.mu.Lock()
	_, exists := b.owners["chat-1"]
	b.mu.Unlock()
	if exists {
		t.Fatal("stale frame polluted the new generation's mirror")
	}
}

// enterNotifyReader closes entered on the first Read call.
type enterNotifyReader struct {
	r       io.Reader
	entered chan struct{}
	once    sync.Once
}

func (n *enterNotifyReader) Read(p []byte) (int, error) {
	n.once.Do(func() { close(n.entered) })
	return n.r.Read(p)
}

// lockProbeWriter deterministically binds the production frame lock: inside
// every Write it probes writeMu with TryLock. If writeRequest holds the lock
// for the whole frame (header + payload), the probe fails and lockHeld stays
// true; if the production lock is removed, the probe succeeds and lockHeld
// becomes false — a deterministic regression signal independent of scheduling.
type lockProbeWriter struct {
	b        *browserCoordinator
	lockHeld atomic.Bool
	probed   atomic.Bool
	mu       sync.Mutex
	buf      bytes.Buffer
}

func (w *lockProbeWriter) Write(p []byte) (int, error) {
	if w.probed.CompareAndSwap(false, true) {
		if w.b.writeMu.TryLock() {
			// The frame lock is NOT held during a frame write: the production
			// implementation is broken.
			w.b.writeMu.Unlock()
		} else {
			w.lockHeld.Store(true)
		}
	}
	w.mu.Lock()
	n, _ := w.buf.Write(p)
	w.mu.Unlock()
	return n, nil
}

func (w *lockProbeWriter) Close() error { return nil }

func (w *lockProbeWriter) bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...)
}
