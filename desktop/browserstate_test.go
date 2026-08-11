package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/desktop/internal/browseripc"
)

// newStateTestCoordinator is like newTestCoordinator but without spawning: the
// state store only needs the mirror, which tests populate directly.
func newStateTestCoordinator() *browserCoordinator {
	opts := browserCoordinatorOptions{
		resolveBinary: func() (string, error) { return "/fake", nil },
		spawn: func(ctx context.Context, path string, env []string) (*exec.Cmd, io.WriteCloser, io.Reader, io.Reader, error) {
			return nil, nil, nil, nil, os.ErrNotExist
		},
		now: time.Now,
	}
	return newBrowserCoordinator(opts)
}

// TestBrowserStateRoundTrip: mirror mutations persist and reload with tab order
// and active tab preserved.
func TestBrowserStateRoundTrip(t *testing.T) {
	b := newStateTestCoordinator()
	store := newBrowserStateStore()
	b.tabsChanged = func() { store.syncFromCoordinator(b) }

	b.updateTabMirror("chat-1", "t1", "https://example.com", "Example", true, 1)
	b.updateTabMirror("chat-1", "t2", "https://other.com", "Other", false, 1)
	b.setActiveTabMirror("chat-1", "t1")

	state := loadBrowserStateFile()
	owner, ok := state.Owners["chat-1"]
	if !ok {
		t.Fatalf("owner chat-1 missing from %s", browserStateFileName)
	}
	if len(owner.Tabs) != 2 || owner.Tabs[0].ID != "t1" || owner.Tabs[0].URL != "https://example.com" {
		t.Fatalf("tabs = %+v", owner.Tabs)
	}
	if owner.ActiveTab != "t1" {
		t.Fatalf("activeTab = %q", owner.ActiveTab)
	}
	if state.Format != browserStateFormatV1 || state.Version != browserStateVersionV1 {
		t.Fatalf("format/version: %+v", state)
	}
	if state.Generation == 0 {
		t.Fatal("generation not persisted")
	}
}

// TestBrowserStateRemoveOwner: deleting a chat removes only its tabs; other
// chats' state and the file stay intact.
func TestBrowserStateRemoveOwner(t *testing.T) {
	b := newStateTestCoordinator()
	store := newBrowserStateStore()
	b.tabsChanged = func() { store.syncFromCoordinator(b) }

	b.updateTabMirror("chat-1", "t1", "https://example.com", "Example", true, 1)
	b.updateTabMirror("chat-2", "t2", "https://other.com", "Other", true, 1)

	if err := b.RemoveOwner(context.Background(), "chat-1"); err != nil {
		t.Fatalf("RemoveOwner: %v", err)
	}

	state := loadBrowserStateFile()
	if _, ok := state.Owners["chat-1"]; ok {
		t.Fatal("chat-1 tabs still persisted after removal")
	}
	if _, ok := state.Owners["chat-2"]; !ok {
		t.Fatal("chat-2 tabs lost")
	}
}

// TestBrowserStateGenerationGuard: a stale snapshot (collected before a newer
// write landed) cannot overwrite fresher state. The snapshot/write split
// models an asynchronous save landing out of order.
func TestBrowserStateGenerationGuard(t *testing.T) {
	dir := t.TempDir()
	store := &browserStateStore{path: filepath.Join(dir, browserStateFileName)}

	b1 := newStateTestCoordinator()
	b2 := newStateTestCoordinator()
	b1.updateTabMirror("chat-1", "t1", "https://old.com", "Old", true, 1)
	b2.updateTabMirror("chat-1", "t1", "https://new.com", "New", true, 2)

	// The old snapshot is collected first (gen 1, old URL)...
	stale := store.snapshotFromCoordinator(b1)
	// ...then the fresh snapshot is collected and written (gen 2)...
	fresh := store.snapshotFromCoordinator(b2)
	store.write(fresh)
	// ...and the old snapshot's write lands late: gen 1 < lastWritten 2, so it
	// must be dropped, not applied.
	store.write(stale)

	state := loadBrowserStateFileFrom(filepath.Join(dir, browserStateFileName))
	owner := state.Owners["chat-1"]
	if len(owner.Tabs) != 1 || owner.Tabs[0].URL != "https://new.com" {
		t.Fatalf("stale snapshot overwrote newer state: %+v", owner.Tabs)
	}
}

// TestBrowserStateDebounceCoalescesLatestSnapshot proves the app callback can
// absorb a navigation/title burst without touching disk until an explicit
// flush, and that the flushed state is the newest mirror generation.
func TestBrowserStateDebounceCoalescesLatestSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, browserStateFileName)
	store := &browserStateStore{path: path, debounce: time.Hour}
	b := newStateTestCoordinator()

	b.updateTabMirror("chat-1", "t1", "https://old.example", "Old", true, 1)
	store.scheduleFromCoordinator(b)
	b.updateTabMirror("chat-1", "t1", "https://new.example", "New", true, 2)
	store.scheduleFromCoordinator(b)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("debounced save touched disk before flush: %v", err)
	}
	store.flush()
	state := loadBrowserStateFileFrom(path)
	owner := state.Owners["chat-1"]
	if len(owner.Tabs) != 1 || owner.Tabs[0].URL != "https://new.example" || owner.Tabs[0].Title != "New" {
		t.Fatalf("flushed state = %+v, want newest snapshot", owner)
	}
	if state.Generation != 2 || store.lastWritten != 2 {
		t.Fatalf("generations disk=%d written=%d, want 2", state.Generation, store.lastWritten)
	}
}

// TestBrowserStateSnapshotGenerationLinearizesMirrorAge proves an older
// mirror cannot release the coordinator lock and later receive a generation
// newer than a concurrently collected snapshot.
func TestBrowserStateSnapshotGenerationLinearizesMirrorAge(t *testing.T) {
	b := newStateTestCoordinator()
	store := newBrowserStateStore()
	store.mu.Lock() // Hold generation assignment so snapshot must wait there.

	snapshotDone := make(chan struct{})
	go func() {
		_ = store.snapshotFromCoordinator(b)
		close(snapshotDone)
	}()

	deadline := time.Now().Add(time.Second)
	for b.mu.TryLock() {
		b.mu.Unlock()
		if time.Now().After(deadline) {
			store.mu.Unlock()
			t.Fatal("snapshot never acquired the coordinator lock")
		}
		time.Sleep(time.Millisecond)
	}

	mutationDone := make(chan struct{})
	go func() {
		b.mu.Lock()
		b.owners["newer"] = &browserOwnerState{ownerID: "newer"}
		b.mu.Unlock()
		close(mutationDone)
	}()
	select {
	case <-mutationDone:
		store.mu.Unlock()
		t.Fatal("coordinator mutation overtook snapshot generation assignment")
	case <-time.After(25 * time.Millisecond):
		// Expected: the snapshot holds b.mu until it receives its generation.
	}

	store.mu.Unlock()
	select {
	case <-snapshotDone:
	case <-time.After(time.Second):
		t.Fatal("snapshot did not finish after generation lock was released")
	}
	select {
	case <-mutationDone:
	case <-time.After(time.Second):
		t.Fatal("coordinator mutation did not resume after snapshot")
	}
}

// TestBrowserStateCorruptFileTolerated: a corrupt file loads as empty without
// crashing and the next write repairs it (only well-formed documents from a
// newer format version are protected from overwrite).
func TestBrowserStateCorruptFileTolerated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, browserStateFileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadBrowserStateFileFrom(path); got.Future || len(got.Owners) != 0 {
		t.Fatalf("corrupt file loaded: %+v", got)
	}

	// A later save over the corrupt file must succeed and be readable.
	store := &browserStateStore{path: path}
	b := newStateTestCoordinator()
	b.updateTabMirror("chat-1", "t1", "https://example.com", "Example", true, 1)
	store.syncFromCoordinator(b)
	got := loadBrowserStateFileFrom(path)
	if got.Future || len(got.Owners) != 1 {
		t.Fatalf("save over corrupt file failed: %+v", got)
	}
}

// TestBrowserStateRestoreReplaysTabs: after a fresh coordinator start the
// persisted state is replayed to the companion as background tab.open calls.
func TestBrowserStateRestoreReplaysTabs(t *testing.T) {
	dir := t.TempDir()
	store := &browserStateStore{path: filepath.Join(dir, browserStateFileName)}
	b := newStateTestCoordinator()
	b.updateTabMirror("chat-1", "t1", "https://example.com", "Example", true, 1)
	b.updateTabMirror("chat-1", "t2", "https://other.com", "Other", false, 1)
	b.setActiveTabMirror("chat-1", "t1")
	store.syncFromCoordinator(b)

	// Fresh coordinator: no mirror, but the persisted state exists.
	fresh := newStateTestCoordinator()
	// Replace the file's location: restoreBrowserState reads the global dir.
	if err := os.MkdirAll(desktopConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.ReadFile(filepath.Join(dir, browserStateFileName))
	if err := os.WriteFile(filepath.Join(desktopConfigDir(), browserStateFileName), orig, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(filepath.Join(desktopConfigDir(), browserStateFileName)) })

	fresh.opts.spawn = func(ctx context.Context, path string, env []string) (*exec.Cmd, io.WriteCloser, io.Reader, io.Reader, error) {
		// The fake companion records tab.open calls.
		cmd := fakeBrowserCompanionCommand("ok")
		cmd.Env = append(env, "GO_WANT_BROWSER_FAKE=1", "BROWSER_FAKE_MODE=ok")
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
		return cmd, stdin, stdout, stderr, nil
	}
	fresh.opts.resolveBinary = func() (string, error) { return "/fake", nil }
	fresh.opts.now = time.Now

	// Trigger a start; the handshake triggers restoreBrowserState which issues
	// tab.open calls against the fake companion.
	var res browseripc.TabInfo
	if err := fresh.Call(context.Background(), "chat-1", "tab.open", browseripc.TabOpenParams{
		OwnerID: "chat-1", URL: "https://fresh.com", Disposition: browseripc.DispositionForeground,
	}, &res); err != nil {
		t.Fatalf("fresh start: %v", err)
	}
	// The mirror must contain the restored tabs from the persisted state.
	fresh.mu.Lock()
	owner := fresh.owners["chat-1"]
	fresh.mu.Unlock()
	if owner == nil || len(owner.tabs) < 2 {
		t.Fatalf("restored mirror = %+v", owner)
	}
}

// TestBrowserStateRestoreStableAcrossRestarts: two consecutive companion
// restarts must produce exactly the same tab count, order, and active tab.
// The companion assigns fresh IDs per process, so the host must map persisted
// IDs to live IDs on every restore; stale IDs must never survive or
// accumulate.
func TestBrowserStateRestoreStableAcrossRestarts(t *testing.T) {
	// Seed the persisted state with an older-generation layout.
	store := &browserStateStore{path: filepath.Join(desktopConfigDir(), browserStateFileName)}
	b := newStateTestCoordinator()
	b.updateTabMirror("chat-1", "t-old-1", "https://example.com", "Example", false, 1)
	b.updateTabMirror("chat-1", "t-old-2", "https://other.com", "Other", false, 1)
	b.setActiveTabMirror("chat-1", "t-old-1")
	store.syncFromCoordinator(b)

	spawnWithFake := func() (*browserCoordinator, *browserStateStore) {
		fresh := newStateTestCoordinator()
		fresh.opts.resolveBinary = func() (string, error) { return "/fake", nil }
		fresh.opts.now = time.Now
		fresh.opts.spawn = func(ctx context.Context, path string, env []string) (*exec.Cmd, io.WriteCloser, io.Reader, io.Reader, error) {
			// events-first reproduces the real companion's frame order:
			// navigation/tab.changed arrive before the tab.open response.
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
			return cmd, stdin, stdout, stderr, nil
		}
		store := &browserStateStore{path: filepath.Join(desktopConfigDir(), browserStateFileName)}
		fresh.tabsChanged = func() { store.syncFromCoordinator(fresh) }
		return fresh, store
	}
	mirrorSnapshot := func(c *browserCoordinator) (urls []string, active, activeURL string, count int) {
		c.mu.Lock()
		defer c.mu.Unlock()
		owner := c.owners["chat-1"]
		if owner == nil {
			return nil, "", "", 0
		}
		count = len(owner.tabs)
		for _, tb := range owner.tabs {
			urls = append(urls, tb.url)
			if tb.tabID == owner.activeTab {
				active, activeURL = tb.tabID, tb.url
			}
		}
		return urls, active, activeURL, count
	}

	// First restart: restored from the seeded file. owner.activate triggers
	// the start without creating or activating any tab itself, so the mirror
	// reflects exactly the restore.
	first, firstStore := spawnWithFake()
	t.Cleanup(first.Close)
	var res json.RawMessage
	if err := first.Call(context.Background(), "chat-1", "owner.activate", browseripc.OwnerParams{OwnerID: "chat-1"}, &res); err != nil {
		t.Fatalf("first start: %v", err)
	}
	firstURLs, firstActive, firstActiveURL, firstCount := mirrorSnapshot(first)
	if firstCount != 2 || firstURLs[0] != "https://example.com" || firstURLs[1] != "https://other.com" {
		t.Fatalf("first restore: %d tabs %v, want 2 [example, other]", firstCount, firstURLs)
	}
	// The persisted active tab (example) must be active under a mapped live
	// ID — never the stale persisted ID.
	if firstActive == "" || firstActive == "t-old-1" || firstActiveURL != "https://example.com" {
		t.Fatalf("first restore active = %q (%s), want a mapped live ID for example.com", firstActive, firstActiveURL)
	}
	// The restore barrier must persist exactly once: buffered events and
	// tab.open responses may not write partial mirrors.
	if got := firstStore.generation; got != 1 {
		t.Fatalf("first restore persisted %d times, want exactly 1 (partial mirrors would corrupt restart state)", got)
	}

	// Shut down and restart from whatever the first run persisted. The second
	// restore must yield exactly the same count, order, and active mapping.
	first.Close()
	second, secondStore := spawnWithFake()
	t.Cleanup(second.Close)
	if err := second.Call(context.Background(), "chat-1", "owner.activate", browseripc.OwnerParams{OwnerID: "chat-1"}, &res); err != nil {
		t.Fatalf("second start: %v", err)
	}
	secondURLs, secondActive, secondActiveURL, secondCount := mirrorSnapshot(second)
	if secondCount != 2 || secondURLs[0] != "https://example.com" || secondURLs[1] != "https://other.com" {
		t.Fatalf("second restore: %d tabs %v, want 2 [example, other]", secondCount, secondURLs)
	}
	if secondActive == "" || secondActiveURL != "https://example.com" {
		t.Fatalf("second restore active = %q (%s), want a mapped live ID for example.com", secondActive, secondActiveURL)
	}
	if got := secondStore.generation; got != 1 {
		t.Fatalf("second restore persisted %d times, want exactly 1", got)
	}
}

// TestBrowserStateFutureFormatNotOverwritten: a document written by a newer
// format version must survive an older version's save untouched (the older
// reader would drop fields it does not understand).
func TestBrowserStateFutureFormatNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, browserStateFileName)
	future := `{"format":"reasonix.browser.state.v2","version":2,"generation":7,"owners":{"chat-9":{"tabs":[{"id":"future-tab","url":"https://future.example","title":"F"}]}},"futureField":"keep-me"}`
	if err := os.WriteFile(path, []byte(future), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadBrowserStateFileFrom(path); !got.Future {
		t.Fatal("future file must load as Future")
	}

	store := &browserStateStore{path: path}
	b := newStateTestCoordinator()
	b.updateTabMirror("chat-1", "t1", "https://example.com", "Example", true, 1)
	store.syncFromCoordinator(b)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != future {
		t.Fatalf("future file was overwritten:\n got: %s\nwant: %s", after, future)
	}
}

// TestBrowserStateV1RoundTripKeepsOwnFields: a v1 document round-trips through
// load -> sync without losing any v1 field the writer produced.
func TestBrowserStateV1RoundTripKeepsOwnFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, browserStateFileName)
	store := &browserStateStore{path: path}
	b := newStateTestCoordinator()
	b.updateTabMirror("chat-1", "t1", "https://example.com", "Example", true, 1)
	b.setActiveTabMirror("chat-1", "t1")
	store.syncFromCoordinator(b)

	first := loadBrowserStateFileFrom(path)
	if first.Future || first.Generation == 0 || first.Owners["chat-1"].ActiveTab != "t1" {
		t.Fatalf("first round trip lost v1 fields: %+v", first)
	}
	// A second save over the same v1 file must preserve generation monotonicity
	// and owner data.
	b.updateTabMirror("chat-1", "t1", "https://example.com/updated", "Example", true, 2)
	store.syncFromCoordinator(b)
	second := loadBrowserStateFileFrom(path)
	if second.Generation <= first.Generation {
		t.Fatalf("generation did not advance: %d -> %d", first.Generation, second.Generation)
	}
	if second.Owners["chat-1"].Tabs[0].URL != "https://example.com/updated" {
		t.Fatalf("update lost: %+v", second.Owners["chat-1"].Tabs)
	}
}

// TestBrowserStateRestoreAfterCompanionCrash: when the companion process dies
// while the desktop keeps running, the host must restart it and re-restore
// every owner into the fresh process — the old mirror's tab IDs belong to the
// dead process and must be remapped, never reused.
func TestBrowserStateRestoreAfterCompanionCrash(t *testing.T) {
	store := &browserStateStore{path: filepath.Join(desktopConfigDir(), browserStateFileName)}
	seed := newStateTestCoordinator()
	seed.updateTabMirror("chat-1", "t-old-1", "https://example.com", "Example", false, 1)
	seed.updateTabMirror("chat-1", "t-old-2", "https://other.com", "Other", false, 1)
	seed.setActiveTabMirror("chat-1", "t-old-1")
	store.syncFromCoordinator(seed)

	// One coordinator lives across the crash: mode "crash-after-restore"
	// answers the restore, then dies on the next request.
	c := newStateTestCoordinator()
	c.opts.resolveBinary = func() (string, error) { return "/fake", nil }
	c.opts.now = time.Now
	c.opts.spawn = func(ctx context.Context, path string, env []string) (*exec.Cmd, io.WriteCloser, io.Reader, io.Reader, error) {
		cmd := fakeBrowserCompanionCommand("crash-after-restore")
		cmd.Env = append(env, "GO_WANT_BROWSER_FAKE=1", "BROWSER_FAKE_MODE=crash-after-restore")
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
		return cmd, stdin, stdout, stderr, nil
	}
	store2 := &browserStateStore{path: filepath.Join(desktopConfigDir(), browserStateFileName)}
	c.tabsChanged = func() { store2.syncFromCoordinator(c) }
	t.Cleanup(c.Close)

	ctx := context.Background()
	var res json.RawMessage
	if err := c.Call(ctx, "chat-1", "owner.activate", browseripc.OwnerParams{OwnerID: "chat-1"}, &res); err != nil {
		t.Fatalf("start: %v", err)
	}
	gen1 := c.companionGen
	c.mu.Lock()
	firstOwner := c.owners["chat-1"]
	firstIDs := make([]string, len(firstOwner.tabs))
	for i, tb := range firstOwner.tabs {
		firstIDs[i] = tb.tabID
	}
	firstActive := firstOwner.activeTab
	c.mu.Unlock()
	if gen1 == 0 || len(firstIDs) != 2 || firstActive == "" || firstActive == "t-old-1" {
		t.Fatalf("first restore: gen=%d ids=%v active=%q", gen1, firstIDs, firstActive)
	}
	if got := store2.generation; got != 1 {
		t.Fatalf("first restore persisted %d times, want exactly 1", got)
	}

	// The next request kills the companion; the host must restart it and
	// remap every owner into the fresh process.
	if err := c.Call(ctx, "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, &res); err == nil {
		t.Fatal("companion should have crashed on this call")
	}
	if c.State() != browserCrashed {
		t.Fatalf("state = %q, want crashed", c.State())
	}
	if err := c.Call(ctx, "chat-1", "owner.activate", browseripc.OwnerParams{OwnerID: "chat-1"}, &res); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if c.companionGen <= gen1 {
		t.Fatalf("companionGen did not advance: %d -> %d", gen1, c.companionGen)
	}
	c.mu.Lock()
	secondOwner := c.owners["chat-1"]
	secondIDs := make([]string, len(secondOwner.tabs))
	for i, tb := range secondOwner.tabs {
		secondIDs[i] = tb.tabID
	}
	secondActive := secondOwner.activeTab
	c.mu.Unlock()
	if len(secondIDs) != 2 || secondIDs[0] == firstIDs[0] || secondIDs[1] == firstIDs[1] {
		t.Fatalf("restart did not remap IDs: first=%v second=%v", firstIDs, secondIDs)
	}
	if secondActive == "" || secondActive == firstActive {
		t.Fatalf("restart active = %q (first %q): must map to a fresh live ID", secondActive, firstActive)
	}
	if got := store2.generation; got != 2 {
		t.Fatalf("restart restore persisted %d times total, want exactly 2 (one per process)", got)
	}
}

// TestBrowserStateRemoveOwnerDuringRestore: deleting a chat while its restore
// is in flight must not resurrect the deleted chat's tabs — the restore
// commit checks the tombstone and skips publication entirely.
func TestBrowserStateRemoveOwnerDuringRestore(t *testing.T) {
	store := &browserStateStore{path: filepath.Join(desktopConfigDir(), browserStateFileName)}
	seed := newStateTestCoordinator()
	seed.updateTabMirror("chat-1", "t-old-1", "https://example.com", "Example", false, 1)
	seed.updateTabMirror("chat-1", "t-old-2", "https://other.com", "Other", false, 1)
	seed.setActiveTabMirror("chat-1", "t-old-1")
	store.syncFromCoordinator(seed)

	c := newStateTestCoordinator()
	c.opts.resolveBinary = func() (string, error) { return "/fake", nil }
	c.opts.now = time.Now
	c.opts.spawn = func(ctx context.Context, path string, env []string) (*exec.Cmd, io.WriteCloser, io.Reader, io.Reader, error) {
		// slow: tab.open responses (and events) are delayed, leaving the
		// restore in flight while the test deletes the chat.
		cmd := fakeBrowserCompanionCommand("slow")
		cmd.Env = append(env, "GO_WANT_BROWSER_FAKE=1", "BROWSER_FAKE_MODE=slow")
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
		return cmd, stdin, stdout, stderr, nil
	}
	store2 := &browserStateStore{path: filepath.Join(desktopConfigDir(), browserStateFileName)}
	c.tabsChanged = func() { store2.syncFromCoordinator(c) }
	t.Cleanup(c.Close)

	ctx := context.Background()
	var res json.RawMessage
	started := make(chan error, 1)
	go func() {
		started <- c.Call(ctx, "chat-1", "owner.activate", browseripc.OwnerParams{OwnerID: "chat-1"}, &res)
	}()
	// Wait until the restore is inside the barrier (its tab.open calls are in
	// flight), then delete the chat.
	deadline := time.Now().Add(3 * time.Second)
	for {
		c.mu.Lock()
		_, barriered := c.restoreBarrier["chat-1"]
		state := c.state
		c.mu.Unlock()
		if barriered && state == browserStarting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("restore barrier never raised")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := c.RemoveOwner(ctx, "chat-1"); err != nil {
		t.Fatalf("RemoveOwner: %v", err)
	}
	// The trigger request that was queued before the deletion must NOT reach
	// the companion: it is a stale operation for a tombstoned owner and is
	// rejected at the gate.
	if err := <-started; err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("restore call: %v, want the stale trigger rejected as deleted", err)
	}

	// The deleted chat must not be resurrected in the mirror or on disk.
	c.mu.Lock()
	_, exists := c.owners["chat-1"]
	c.mu.Unlock()
	if exists {
		t.Fatal("deleted chat was resurrected in the mirror")
	}
	state := loadBrowserStateFile()
	if _, ok := state.Owners["chat-1"]; ok {
		t.Fatal("deleted chat was resurrected on disk")
	}

	// A later normal tab activity recreates the owner and clears the
	// tombstone.
	if err := c.Call(ctx, "chat-1", "tab.open", browseripc.TabOpenParams{
		OwnerID: "chat-1", URL: "https://fresh.com", Disposition: browseripc.DispositionForeground,
	}, &res); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	c.mu.Lock()
	_, tombstoned := c.tombstonedOwners["chat-1"]
	c.mu.Unlock()
	if tombstoned {
		t.Fatal("tombstone not cleared on normal recreation")
	}
}

// TestBrowserStateRestoreCrashMidwayDoesNotOverwrite: a companion dying in
// the middle of the restore pass must not publish or persist a partial/empty
// mirror — the intact desired state on disk survives, and a retry restores it
// fully.
func TestBrowserStateRestoreCrashMidwayDoesNotOverwrite(t *testing.T) {
	store := &browserStateStore{path: filepath.Join(desktopConfigDir(), browserStateFileName)}
	seed := newStateTestCoordinator()
	seed.updateTabMirror("chat-1", "t-old-1", "https://example.com", "Example", false, 1)
	seed.updateTabMirror("chat-1", "t-old-2", "https://other.com", "Other", false, 1)
	seed.setActiveTabMirror("chat-1", "t-old-1")
	store.syncFromCoordinator(seed)

	healthySpawn := func(mode string) func(ctx context.Context, path string, env []string) (*exec.Cmd, io.WriteCloser, io.Reader, io.Reader, error) {
		return func(ctx context.Context, path string, env []string) (*exec.Cmd, io.WriteCloser, io.Reader, io.Reader, error) {
			cmd := fakeBrowserCompanionCommand(mode)
			cmd.Env = append(env, "GO_WANT_BROWSER_FAKE=1", "BROWSER_FAKE_MODE="+mode)
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
			return cmd, stdin, stdout, stderr, nil
		}
	}

	c := newStateTestCoordinator()
	c.opts.resolveBinary = func() (string, error) { return "/fake", nil }
	c.opts.now = time.Now
	store2 := &browserStateStore{path: filepath.Join(desktopConfigDir(), browserStateFileName)}
	c.tabsChanged = func() { store2.syncFromCoordinator(c) }
	t.Cleanup(c.Close)

	ctx := context.Background()
	var res json.RawMessage
	// First attempt: the companion dies on the restore's first tab.open.
	c.opts.spawn = healthySpawn("crash-during-restore")
	if err := c.Call(ctx, "chat-1", "owner.activate", browseripc.OwnerParams{OwnerID: "chat-1"}, &res); err == nil {
		t.Fatal("start with a dying companion should fail")
	}
	// The disk must still hold the full desired state — never a partial or
	// empty mirror.
	state := loadBrowserStateFile()
	owner, ok := state.Owners["chat-1"]
	if !ok || len(owner.Tabs) != 2 {
		t.Fatalf("crash mid-restore replaced desired state: %+v", owner)
	}
	c.mu.Lock()
	_, published := c.owners["chat-1"]
	_, barrier := c.restoreBarrier["chat-1"]
	c.mu.Unlock()
	if published {
		t.Fatal("partial mirror was published after the crash")
	}
	if barrier {
		t.Fatal("restore barrier leaked after the aborted pass")
	}
	if got := store2.generation; got != 0 {
		t.Fatalf("aborted restore persisted %d times, want 0", got)
	}

	// Retry with a healthy companion (past the backoff window): the intact
	// desired state restores fully.
	time.Sleep(300 * time.Millisecond)
	c.opts.spawn = healthySpawn("events-first")
	if err := c.Call(ctx, "chat-1", "owner.activate", browseripc.OwnerParams{OwnerID: "chat-1"}, &res); err != nil {
		t.Fatalf("retry start: %v", err)
	}
	c.mu.Lock()
	restored := c.owners["chat-1"]
	restoredCount := 0
	if restored != nil {
		restoredCount = len(restored.tabs)
	}
	c.mu.Unlock()
	if restoredCount != 2 {
		t.Fatalf("retry restored %d tabs, want 2", restoredCount)
	}
	if got := store2.generation; got != 1 {
		t.Fatalf("retry restore persisted %d times, want exactly 1", got)
	}
}

// TestBrowserStateLateEventsDoNotResurrectDeletedOwner: navigation/tab.changed
// events that arrive AFTER the restore commit (real companion keeps emitting
// them) must not recreate an owner deleted during the restore, and must never
// clear its tombstone — only an explicit host-side reopen does.
func TestBrowserStateLateEventsDoNotResurrectDeletedOwner(t *testing.T) {
	store := &browserStateStore{path: filepath.Join(desktopConfigDir(), browserStateFileName)}
	seed := newStateTestCoordinator()
	seed.updateTabMirror("chat-1", "t-old-1", "https://example.com", "Example", false, 1)
	seed.updateTabMirror("chat-1", "t-old-2", "https://other.com", "Other", false, 1)
	seed.setActiveTabMirror("chat-1", "t-old-1")
	store.syncFromCoordinator(seed)

	c := newStateTestCoordinator()
	c.opts.resolveBinary = func() (string, error) { return "/fake", nil }
	c.opts.now = time.Now
	c.opts.spawn = func(ctx context.Context, path string, env []string) (*exec.Cmd, io.WriteCloser, io.Reader, io.Reader, error) {
		// slow-late: slow restore responses (to interleave the deletion) plus
		// navigation/tab.changed events emitted after the restore completes.
		cmd := fakeBrowserCompanionCommand("slow-late")
		cmd.Env = append(env, "GO_WANT_BROWSER_FAKE=1", "BROWSER_FAKE_MODE=slow-late")
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
		return cmd, stdin, stdout, stderr, nil
	}
	store2 := &browserStateStore{path: filepath.Join(desktopConfigDir(), browserStateFileName)}
	c.tabsChanged = func() { store2.syncFromCoordinator(c) }
	t.Cleanup(c.Close)

	ctx := context.Background()
	var res json.RawMessage
	started := make(chan error, 1)
	go func() {
		started <- c.Call(ctx, "chat-1", "owner.activate", browseripc.OwnerParams{OwnerID: "chat-1"}, &res)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c.mu.Lock()
		_, barriered := c.restoreBarrier["chat-1"]
		c.mu.Unlock()
		if barriered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("restore barrier never raised")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := c.RemoveOwner(ctx, "chat-1"); err != nil {
		t.Fatalf("RemoveOwner: %v", err)
	}
	// The trigger request that was queued before the deletion must NOT reach
	// the companion: it is a stale operation for a tombstoned owner and is
	// rejected at the gate.
	if err := <-started; err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("restore call: %v, want the stale trigger rejected as deleted", err)
	}

	// The late events were emitted before the trigger response, so they have
	// arrived by now. The deleted owner must still be absent, tombstoned.
	c.mu.Lock()
	_, exists := c.owners["chat-1"]
	_, tombstoned := c.tombstonedOwners["chat-1"]
	c.mu.Unlock()
	if exists {
		t.Fatal("late events resurrected the deleted owner")
	}
	if !tombstoned {
		t.Fatal("late events cleared the deletion tombstone")
	}
	if got := store2.generation; got != 1 {
		t.Fatalf("persisted %d times, want exactly 1 (the RemoveOwner sync)", got)
	}

	// An explicit host-side reopen clears the tombstone and recreates the
	// owner.
	if err := c.Call(ctx, "chat-1", "tab.open", browseripc.TabOpenParams{
		OwnerID: "chat-1", URL: "https://fresh.com", Disposition: browseripc.DispositionForeground,
	}, &res); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	c.mu.Lock()
	_, tombstoned = c.tombstonedOwners["chat-1"]
	_, exists = c.owners["chat-1"]
	c.mu.Unlock()
	if tombstoned {
		t.Fatal("tombstone not cleared by explicit reopen")
	}
	if !exists {
		t.Fatal("owner not recreated by explicit reopen")
	}
}

// TestBrowserStateEmptyMirrorPersistsEmpty: closing the last chat clears the
// file so a restart does not resurrect deleted tabs.
func TestBrowserStateEmptyMirrorPersistsEmpty(t *testing.T) {
	b := newStateTestCoordinator()
	store := newBrowserStateStore()
	b.tabsChanged = func() { store.syncFromCoordinator(b) }
	b.updateTabMirror("chat-1", "t1", "https://example.com", "Example", true, 1)
	if err := b.RemoveOwner(context.Background(), "chat-1"); err != nil {
		t.Fatal(err)
	}
	state := loadBrowserStateFile()
	if len(state.Owners) != 0 {
		t.Fatalf("owners after full removal: %+v", state.Owners)
	}
}
