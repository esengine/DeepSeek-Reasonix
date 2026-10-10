package serve

import (
	"encoding/json"
	"os"
	"sync"

	fileencoding "reasonix/internal/base/fileutil/encoding"
)

// stampFile is the shared half of the per-project sidecar caches: a JSON map
// keyed by file name, loaded once, and written at most once per batch of
// changes. The title cache and the 1.x mark cache ride it — what an entry
// holds and what makes one stale differ, while load, dirty-marking and the
// single write per listing do not.
type stampFile[V any] struct {
	mu      sync.Mutex
	path    string
	loaded  bool
	dirty   bool
	entries map[string]V
}

func newStampFile[V any](path string) *stampFile[V] {
	return &stampFile[V]{path: path, entries: map[string]V{}}
}

// entry loads the file once and returns the record stored under name.
func (f *stampFile[V]) entry(name string) (V, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
	e, ok := f.entries[name]
	return e, ok
}

// record stores a fresh answer in memory and marks the file dirty; flush
// writes it out once, however many records a single listing produced.
func (f *stampFile[V]) record(name string, v V) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
	f.entries[name] = v
	f.dirty = true
}

// flush writes the file once when anything was recorded since the last one.
func (f *stampFile[V]) flush() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.dirty {
		return
	}
	f.dirty = false
	if data, err := json.Marshal(f.entries); err == nil {
		_ = os.WriteFile(f.path, data, 0o644)
	}
}

// setPath repoints the file at another project and drops what was loaded from
// the previous one: entries are keyed by file name, which is unique within a
// project and not across them.
func (f *stampFile[V]) setPath(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.path == path {
		return
	}
	f.path = path
	f.loaded = false
	f.dirty = false
	f.entries = map[string]V{}
}

func (f *stampFile[V]) load() {
	if f.loaded {
		return
	}
	f.loaded = true
	if data, err := fileencoding.ReadFileUTF8(f.path); err == nil {
		_ = json.Unmarshal(data, &f.entries)
	}
}
