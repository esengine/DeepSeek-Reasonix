package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	readFileLineStartCacheEntries = 32
	readFileLineStartCacheMax     = 200_000
)

var readFileLineStarts = newReadFileLineStartCache(readFileLineStartCacheEntries)

type readFileLineStartCache struct {
	mu         sync.Mutex
	maxEntries int
	order      []string
	entries    map[string]*readFileLineStartEntry
}

type readFileLineStartEntry struct {
	starts []int64
}

type readFileLineStart struct {
	line       int
	byteOffset int64
}

func newReadFileLineStartCache(maxEntries int) *readFileLineStartCache {
	if maxEntries <= 0 {
		maxEntries = 1
	}
	return &readFileLineStartCache{
		maxEntries: maxEntries,
		entries:    make(map[string]*readFileLineStartEntry),
	}
}

func readFileLineStartKey(path string, info os.FileInfo) string {
	return fmt.Sprintf("%s\x00%d\x00%d", filepath.Clean(path), info.Size(), info.ModTime().UnixNano())
}

func (c *readFileLineStartCache) nearest(path string, info os.FileInfo, target int) (int, int64, bool) {
	if c == nil || info == nil || info.Size() <= 0 || target < 0 {
		return 0, 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := c.entries[readFileLineStartKey(path, info)]
	if entry == nil || len(entry.starts) == 0 {
		return 0, 0, false
	}
	if target >= len(entry.starts) {
		target = len(entry.starts) - 1
	}
	return target, entry.starts[target], true
}

func (c *readFileLineStartCache) record(path string, info os.FileInfo, starts []readFileLineStart) {
	if c == nil || info == nil || info.Size() <= 0 || len(starts) == 0 {
		return
	}
	key := readFileLineStartKey(path, info)

	c.mu.Lock()
	defer c.mu.Unlock()

	entry := c.entries[key]
	if entry == nil {
		entry = &readFileLineStartEntry{starts: []int64{0}}
		c.entries[key] = entry
		c.order = append(c.order, key)
		c.evictOldest()
	}

	for _, start := range starts {
		if start.line < 0 || start.line > readFileLineStartCacheMax || start.byteOffset < 0 || start.byteOffset >= info.Size() {
			continue
		}
		if start.line < len(entry.starts) {
			if entry.starts[start.line] == start.byteOffset {
				continue
			}
			entry.starts = entry.starts[:start.line]
		}
		if start.line == len(entry.starts) {
			entry.starts = append(entry.starts, start.byteOffset)
		}
	}
}

func (c *readFileLineStartCache) evictOldest() {
	for len(c.entries) > c.maxEntries && len(c.order) > 0 {
		key := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, key)
	}
}
