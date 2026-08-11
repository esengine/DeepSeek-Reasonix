// Browser state persistence: the host-side mirror of the companion's tab
// layout, stored in browser-state-v1.json. Cookie, history, cache, IndexedDB,
// and every other Chromium profile artifact live inside the companion's
// isolated persist:reasonix-browser-v1 Session, never in this file. Deleting a
// chat deletes its tab entries here; shared login state is untouched.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"reasonix/internal/fileutil"
)

const (
	browserStateFileName   = "browser-state-v1.json"
	browserStateFormatV1   = "reasonix.browser.state.v1"
	browserStateVersionV1  = 1
	browserStateTempSuffix = ".tmp"
	browserStateDebounce   = 250 * time.Millisecond
)

// browserStateFile is the on-disk format. v1 is frozen: new fields must use a
// new format/version (and, when incompatible, a new file name). Readers
// tolerate unknown fields inside a v1 document and never overwrite a document
// written by a newer format version.
type browserStateFile struct {
	Format      string                       `json:"format"`
	Version     int                          `json:"version"`
	Generation  uint64                       `json:"generation"`
	Owners      map[string]browserStateOwner `json:"owners,omitempty"`
	ActiveOwner string                       `json:"activeOwner,omitempty"`
	// Future marks a file written by a newer format version. Never
	// serialized: readers set it on format/version mismatch, and writers
	// refuse to overwrite a future file.
	Future bool `json:"-"`
}

type browserStateOwner struct {
	Tabs      []browserStateTab `json:"tabs"`
	ActiveTab string            `json:"activeTab,omitempty"`
}

type browserStateTab struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// browserStateStore serializes writes to browser-state-v1.json with a
// generation guard: every snapshot is stamped with a monotonic generation at
// collection time, and a write is dropped if a newer generation already
// landed. A stale snapshot can therefore never overwrite fresher state.
type browserStateStore struct {
	mu sync.Mutex
	// path is the destination; writes go through path+".tmp" then ReplaceFile.
	path string
	// generation is bumped per collected snapshot; lastWritten is the
	// generation of the newest write that landed.
	generation  uint64
	lastWritten uint64
	// App event callbacks debounce disk writes while still collecting every
	// mirror generation. Explicit sync/delete/shutdown paths flush immediately.
	debounce        time.Duration
	pending         *browserStateFile
	timer           browserStateTimer
	scheduleVersion uint64
}

type browserStateTimer interface {
	Stop() bool
}

func newBrowserStateStore() *browserStateStore {
	return &browserStateStore{
		path:     filepath.Join(desktopConfigDir(), browserStateFileName),
		debounce: browserStateDebounce,
	}
}

// syncFromCoordinator snapshots the coordinator's owner mirror and persists it
// atomically.
func (s *browserStateStore) syncFromCoordinator(b *browserCoordinator) {
	state := s.snapshotFromCoordinator(b)
	s.cancelScheduled()
	s.write(state)
}

// scheduleFromCoordinator coalesces navigation/title bursts into one atomic
// disk write. The in-memory coordinator mirror is already current when this
// is called; only persistence is delayed.
func (s *browserStateStore) scheduleFromCoordinator(b *browserCoordinator) {
	state := s.snapshotFromCoordinator(b)
	s.mu.Lock()
	s.pending = &state
	s.scheduleVersion++
	version := s.scheduleVersion
	if s.timer != nil {
		s.timer.Stop()
	}
	delay := s.debounce
	if delay <= 0 {
		delay = browserStateDebounce
	}
	s.timer = time.AfterFunc(delay, func() { s.flushVersion(version) })
	s.mu.Unlock()
}

// flush persists the latest scheduled snapshot synchronously. App shutdown
// and permanent chat deletion call this before returning.
func (s *browserStateStore) flush() {
	s.mu.Lock()
	s.scheduleVersion++
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	state := s.pending
	s.pending = nil
	s.mu.Unlock()
	if state != nil {
		s.write(*state)
	}
}

func (s *browserStateStore) flushVersion(version uint64) {
	s.mu.Lock()
	if version != s.scheduleVersion {
		s.mu.Unlock()
		return
	}
	state := s.pending
	s.pending = nil
	s.timer = nil
	s.mu.Unlock()
	if state != nil {
		s.write(*state)
	}
}

func (s *browserStateStore) cancelScheduled() {
	s.mu.Lock()
	s.scheduleVersion++
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.pending = nil
	s.mu.Unlock()
}

// snapshotFromCoordinator collects the mirror under the coordinator lock and
// stamps the snapshot with the next generation. Collection and stamping are
// atomic, so snapshot generations order by mirror age: a later-collected
// snapshot always carries a higher generation.
func (s *browserStateStore) snapshotFromCoordinator(b *browserCoordinator) browserStateFile {
	b.mu.Lock()
	state := browserStateFile{
		Format:  browserStateFormatV1,
		Version: browserStateVersionV1,
		Owners:  make(map[string]browserStateOwner, len(b.owners)),
	}
	for ownerID, owner := range b.owners {
		so := browserStateOwner{ActiveTab: owner.activeTab}
		so.Tabs = make([]browserStateTab, 0, len(owner.tabs))
		for _, t := range owner.tabs {
			so.Tabs = append(so.Tabs, browserStateTab{ID: t.tabID, URL: t.url, Title: t.title})
		}
		state.Owners[ownerID] = so
	}
	// Keep b.mu held while assigning the store generation. That makes mirror
	// collection order and generation order identical: an older callback can
	// never resume late and receive a generation newer than a later mirror.
	s.mu.Lock()
	s.generation++
	state.Generation = s.generation
	s.mu.Unlock()
	b.mu.Unlock()
	return state
}

// write persists a snapshot unless a newer generation already landed. The
// write itself is serialized and atomic (tmp + ReplaceFile).
func (s *browserStateStore) write(state browserStateFile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state.Generation < s.lastWritten {
		// A newer snapshot already landed; this one is stale by generation.
		return
	}
	if state.Generation == s.lastWritten {
		return
	}
	// A document written by a newer format version must never be overwritten
	// by an older reader's save: the older version would drop fields it does
	// not understand.
	if current := loadBrowserStateFileFrom(s.path); current.Future {
		return
	}
	if len(state.Owners) == 0 {
		// An empty mirror is still a legitimate snapshot (all chats closed);
		// persist it so a restart does not resurrect deleted chats.
		state.Owners = map[string]browserStateOwner{}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(s.path+browserStateTempSuffix, data, 0o644); err != nil {
		return
	}
	if err := fileutil.ReplaceFile(s.path+browserStateTempSuffix, s.path); err != nil {
		return
	}
	s.lastWritten = state.Generation
}

func browserStatePath() string {
	return filepath.Join(desktopConfigDir(), browserStateFileName)
}

// loadBrowserStateFile reads the persisted state from the default location.
func loadBrowserStateFile() browserStateFile {
	return loadBrowserStateFileFrom(browserStatePath())
}

// loadBrowserStateFileFrom reads a state file. Missing or corrupt files yield
// an empty state: the companion starts with no restored tabs rather than
// crashing the host. Unknown fields inside a v1 document are tolerated (older
// readers keep working). A document from a newer format version loads as
// Future, which suppresses every write path.
func loadBrowserStateFileFrom(path string) browserStateFile {
	var state browserStateFile
	data, err := os.ReadFile(path)
	if err != nil {
		return state
	}
	if err := json.Unmarshal(data, &state); err != nil {
		// Corrupt (or not JSON at all): treat as absent so the next save
		// repairs the file. Only a well-formed document from a newer format
		// version is protected from overwrite.
		return browserStateFile{}
	}
	if state.Format != browserStateFormatV1 || state.Version != browserStateVersionV1 {
		return browserStateFile{Future: true}
	}
	if state.Owners == nil {
		state.Owners = map[string]browserStateOwner{}
	}
	return state
}
