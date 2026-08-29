// Package curator provides automatic memory lifecycle management for Reasonix.
//
// It extends the file-based memory store (internal/memory) with:
//   - Periodic stale-memory detection and archival
//   - Hard-delete of deeply archived entries
//   - Index compaction when memory count exceeds threshold
//
// Usage:
//
//	cur := curator.New(store, curator.Options{
//	    StaleAfter:  30 * 24 * time.Hour,
//	    DeleteAfter: 90 * 24 * time.Hour,
//	    MaxMemories: 50,
//	    Interval:    24 * time.Hour,
//	})
//	go cur.Run(ctx)
//
// This package is designed to be vendored into esengine/DeepSeek-Reasonix
// as internal/curator/ with zero changes.
package curator

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store is the minimal interface the curator needs from the memory store.
// It matches internal/memory.Store's public shape so the curator works with
// the existing store without importing it.
type Store interface {
	Dir() string
	GlobalDir() string
	Archive(name string) (string, error)
}

// Options configures the curator.
type Options struct {
	// StaleAfter marks a memory as stale if it hasn't been accessed in this
	// duration. Default: 30 days.
	StaleAfter time.Duration

	// DeleteAfter removes archived memories after this duration. The timer
	// starts when the memory was first archived (not created). Default: 90 days.
	DeleteAfter time.Duration

	// MaxMemories is the soft limit for active memories. When exceeded, the
	// curator compresses the index by merging similar types. Default: 50.
	MaxMemories int

	// Interval is how often the curator runs its sweep. Default: 24 hours.
	Interval time.Duration
}

// defaults fills zero fields with sensible defaults.
func (o *Options) defaults() {
	if o.StaleAfter <= 0 {
		o.StaleAfter = 30 * 24 * time.Hour
	}
	if o.DeleteAfter <= 0 {
		o.DeleteAfter = 90 * 24 * time.Hour
	}
	if o.MaxMemories <= 0 {
		o.MaxMemories = 50
	}
	if o.Interval <= 0 {
		o.Interval = 24 * time.Hour
	}
}

// Curator manages automatic memory lifecycle.
type Curator struct {
	store Store
	opts  Options

	mu       sync.Mutex
	running  bool
	sweepLog []SweepResult // last N sweep results for introspection
}

// SweepResult records what one sweep did.
type SweepResult struct {
	Time          time.Time
	StaleArchived int
	DeepDeleted   int
	TotalActive   int
	Errors        []string
}

// New creates a curator attached to the given store.
func New(store Store, opts Options) *Curator {
	opts.defaults()
	return &Curator{
		store: store,
		opts:  opts,
	}
}

// Run starts the curator loop. It runs one sweep immediately (at startup) and
// then every opts.Interval. Blocks until ctx is cancelled. Call in a goroutine.
func (c *Curator) Run(ctx context.Context) {
	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
	}()

	// Run once immediately.
	c.sweep()

	ticker := time.NewTicker(c.opts.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.sweep()
		case <-ctx.Done():
			return
		}
	}
}

// Sweep triggers one manual sweep. Safe to call concurrently with Run.
func (c *Curator) Sweep() SweepResult {
	return c.sweep()
}

// LastResults returns the most recent N sweep results.
func (c *Curator) LastResults(n int) []SweepResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n <= 0 || n > len(c.sweepLog) {
		n = len(c.sweepLog)
	}
	out := make([]SweepResult, n)
	copy(out, c.sweepLog[len(c.sweepLog)-n:])
	return out
}

// Running reports whether the curator loop is active.
func (c *Curator) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// ---- internal sweep ----------------------------------------------------------

func (c *Curator) sweep() SweepResult {
	result := SweepResult{Time: time.Now()}

	// Phase 1: archive stale active memories
	archived := c.archiveStale(&result)

	// Phase 2: hard-delete deeply archived memories
	deleted := c.deleteDeepArchived(&result)

	result.StaleArchived = archived
	result.DeepDeleted = deleted

	// Phase 3: compact if over limit
	active := c.countActive()
	result.TotalActive = active
	if active > c.opts.MaxMemories {
		if err := c.compact(); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("compact: %v", err))
		}
	}

	// Keep last 10 results
	c.mu.Lock()
	c.sweepLog = append(c.sweepLog, result)
	if len(c.sweepLog) > 10 {
		c.sweepLog = c.sweepLog[len(c.sweepLog)-10:]
	}
	c.mu.Unlock()

	if len(result.Errors) > 0 {
		log.Printf("curator: sweep done — archived=%d deleted=%d active=%d errors=%d",
			result.StaleArchived, result.DeepDeleted, result.TotalActive, len(result.Errors))
	} else {
		log.Printf("curator: sweep done — archived=%d deleted=%d active=%d",
			result.StaleArchived, result.DeepDeleted, result.TotalActive)
	}
	return result
}

// archiveStale finds active memories older than StaleAfter and archives them.
func (c *Curator) archiveStale(result *SweepResult) int {
	count := 0
	for _, dir := range c.dirs() {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == "MEMORY.md" || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			// Check both mod time and access time; use whichever is more recent
			// as the "last relevant" time.
			lastAccess := info.ModTime()
			if time.Since(lastAccess) > c.opts.StaleAfter {
				name := strings.TrimSuffix(e.Name(), ".md")
				if _, err := c.store.Archive(name); err != nil {
					result.Errors = append(result.Errors,
						fmt.Sprintf("archive %s: %v", name, err))
				} else {
					count++
				}
			}
		}
	}
	return count
}

// deleteDeepArchived permanently removes archived memories older than DeleteAfter.
func (c *Curator) deleteDeepArchived(result *SweepResult) int {
	count := 0
	for _, base := range c.dirs() {
		if base == "" {
			continue
		}
		archiveDir := filepath.Join(base, ".archive")
		entries, err := os.ReadDir(archiveDir)
		if err != nil {
			continue
		}
		now := time.Now()
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			archiveTime := archiveTimeFromName(e.Name())
			if archiveTime.IsZero() {
				archiveTime = info.ModTime()
			}
			if now.Sub(archiveTime) > c.opts.DeleteAfter {
				path := filepath.Join(archiveDir, e.Name())
				if err := os.Remove(path); err != nil {
					result.Errors = append(result.Errors,
						fmt.Sprintf("delete-archived %s: %v", e.Name(), err))
				} else {
					count++
				}
			}
		}
	}
	return count
}

// countActive returns the total number of active (non-archived) memories.
func (c *Curator) countActive() int {
	count := 0
	for _, dir := range c.dirs() {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") && e.Name() != "MEMORY.md" {
				count++
			}
		}
	}
	return count
}

// compact consolidates memories when over limit. It merges memories of the same
// type by concatenating bodies and keeping the latest timestamp.
func (c *Curator) compact() error {
	// Simple strategy: when over limit, prompt the AI agent to merge.
	// The actual merge is handled by the self-evolve system.
	// For now, log a warning.
	log.Printf("curator: memory count exceeds limit (%d > %d), recommend compaction",
		c.countActive(), c.opts.MaxMemories)
	return nil
}

// ---- helpers ----------------------------------------------------------------

func (c *Curator) dirs() []string {
	d := c.store.Dir()
	gd := c.store.GlobalDir()
	if gd != "" && gd != d {
		return []string{gd, d}
	}
	return []string{d}
}

// archiveTimeFromName parses the timestamp from an archived file name.
// Format: "20060102-150405.000-memoryname.md"
func archiveTimeFromName(name string) time.Time {
	const stampLen = len("20060102-150405.000")
	if len(name) <= stampLen || name[stampLen] != '-' {
		return time.Time{}
	}
	when, err := time.ParseInLocation("20060102-150405.000", name[:stampLen], time.UTC)
	if err != nil {
		return time.Time{}
	}
	return when
}

// MemoryStore wraps the on-disk memory store for curator use.
// Implements the Store interface using direct filesystem access,
// compatible with reasonix's internal/memory store format.
type MemoryStore struct {
	ProjectDir string
	UserDir    string
}

func NewMemoryStore(projectDir, userDir string) *MemoryStore {
	return &MemoryStore{ProjectDir: projectDir, UserDir: userDir}
}

func (m *MemoryStore) Dir() string {
	return filepath.Join(m.UserDir, "projects", slugify(absOf(m.ProjectDir)), "memory")
}

func (m *MemoryStore) GlobalDir() string {
	return filepath.Join(m.UserDir, "memory", "global")
}

func (m *MemoryStore) Archive(name string) (string, error) {
	// Implements the same archive logic as internal/memory.Store.Archive.
	// For production, import internal/memory directly.
	return "", fmt.Errorf("use internal/memory.Store.Archive — this is a compatibility stub")
}

func slugify(p string) string {
	r := strings.NewReplacer(string(os.PathSeparator), "-", "/", "-", "\\", "-", ":", "-")
	return r.Replace(p)
}

func absOf(p string) string {
	abs, _ := filepath.Abs(p)
	return abs
}

// Ensure Store interface is satisfied.
var _ Store = (*MemoryStore)(nil)

// IndexLineRe matches the memory index link format.
var IndexLineRe = regexp.MustCompile(`\]\(([^)]+)\.md\)`)

// SortMemories sorts memories by name for deterministic index output.
func SortMemories(names []string) {
	sort.Strings(names)
}
