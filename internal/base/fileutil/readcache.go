package fileutil

import (
	"os"
	"sync"
	"time"
)

type fileIdentity struct {
	device, inode uint64
	changed       int64
}
type cachedFile[T any] struct {
	info  os.FileInfo
	read  time.Time
	value T
}

// ReadCache caches immutable decoded files while their identity and change time
// remain stable. Platforms without change-time evidence always reread.
type ReadCache[T any] struct {
	mu      sync.Mutex
	entries map[string]cachedFile[T]
}

// Load returns a shared immutable value. Failed reads and racing writes are
// never cached; callers must not mutate the returned value.
func (c *ReadCache[T]) Load(path string, read func(string) (T, error)) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	before, err := os.Stat(path)
	if err != nil {
		delete(c.entries, path)
		var zero T
		return zero, err
	}
	if old, ok := c.entries[path]; ok && sameFileVersion(old.info, before) && cacheClockStable(old.info, old.read) {
		return old.value, nil
	}
	value, err := read(path)
	after, statErr := os.Stat(path)
	if err != nil || statErr != nil || !sameFileVersion(before, after) {
		delete(c.entries, path)
		return value, err
	}
	if c.entries == nil || len(c.entries) >= 1024 {
		c.entries = map[string]cachedFile[T]{}
	}
	c.entries[path] = cachedFile[T]{info: after, read: time.Now(), value: value}
	return value, nil
}

func sameFileVersion(a, b os.FileInfo) bool {
	x, ok := cacheFileIdentity(a)
	y, otherOK := cacheFileIdentity(b)
	return ok && otherOK && x == y && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && a.Mode() == b.Mode()
}

func cacheClockStable(info os.FileInfo, read time.Time) bool {
	window := 20 * time.Millisecond
	if info.ModTime().Nanosecond() == 0 {
		window = 2 * time.Second
	}
	id, ok := cacheFileIdentity(info)
	return ok && info.ModTime().Before(read.Add(-window)) && time.Unix(0, id.changed).Before(read.Add(-window))
}
