package navigator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// SensorEvent is what a background sensor emits when it detects a change.
// Sensors don't push events into the agent's prompt directly — they hand them
// to the EventCorrelator, which batches and prioritizes before injection.
type SensorEvent struct {
	Source   string // "filesystem" | "process" | "interface"
	Kind     string // "create" | "modify" | "delete" | "appear" | "vanish"
	Subject  string // path / pid / element id
	At       time.Time
	Detail   string
	Severity int // 0=info, 1=warn, 2=critical
}

// Sensor is the common interface for all background sensors. A sensor runs for
// the lifetime of the kernel and reports changes through Snapshot() — the
// kernel polls it around each action rather than the sensor pushing, so the
// agent loop stays synchronous and predictable.
type Sensor interface {
	// Name returns the sensor's identifier.
	Name() string
	// Snapshot captures the current perceived state and returns a short hash
	// (for drift detection) plus a human-readable digest (for logging/diag).
	Snapshot(ctx context.Context) (hash, digest string, err error)
	// Changes returns the sensor events accumulated since the last Snapshot.
	Changes() []SensorEvent
}

// FilesystemSensor monitors a working directory for file changes. It is the
// cheapest defense against environment-update deafness: if a tool writes a
// file, the next snapshot's hash differs and the engine knows the env changed.
type FilesystemSensor struct {
	mu       sync.Mutex
	root     string
	last     map[string]os.FileInfo
	events   []SensorEvent
	maxDepth int
}

func NewFilesystemSensor(root string, maxDepth int) *FilesystemSensor {
	if maxDepth <= 0 {
		maxDepth = 3
	}
	return &FilesystemSensor{root: root, maxDepth: maxDepth}
}

func (s *FilesystemSensor) Name() string { return "filesystem" }

func (s *FilesystemSensor) Snapshot(ctx context.Context) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := make(map[string]os.FileInfo)
	var paths []string
	_ = filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(s.root, path)
			depth := strings.Count(rel, string(filepath.Separator))
			if depth >= s.maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		current[path] = info
		paths = append(paths, path)
		return nil
	})

	// Detect changes vs last snapshot.
	s.events = s.events[:0]
	for p, old := range s.last {
		cur, ok := current[p]
		if !ok {
			s.events = append(s.events, SensorEvent{Source: "filesystem", Kind: "delete", Subject: p, At: time.Now(), Severity: 1})
		} else if cur.ModTime() != old.ModTime() || cur.Size() != old.Size() {
			s.events = append(s.events, SensorEvent{Source: "filesystem", Kind: "modify", Subject: p, At: time.Now(), Severity: 0})
		}
	}
	for p := range current {
		if _, ok := s.last[p]; !ok {
			s.events = append(s.events, SensorEvent{Source: "filesystem", Kind: "create", Subject: p, At: time.Now(), Severity: 0})
		}
	}
	s.last = current

	// Hash: sort paths, hash path+size+mtime.
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		info := current[p]
		fmt.Fprintf(h, "%s|%d|%d\n", p, info.Size(), info.ModTime().UnixNano())
	}
	hash := hex.EncodeToString(h.Sum(nil)[:6])
	digest := fmt.Sprintf("%d files in %s", len(paths), s.root)
	return hash, digest, nil
}

func (s *FilesystemSensor) Changes() []SensorEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SensorEvent, len(s.events))
	copy(out, s.events)
	return out
}

// ProcessSensor monitors running processes matching a pattern. On Windows it
// uses tasklist; on Unix it uses ps. The hash changes when processes appear or
// vanish — the signal that an external process touched the environment.
type ProcessSensor struct {
	mu      sync.Mutex
	pattern string
	last    map[string]bool
	events  []SensorEvent
}

func NewProcessSensor(pattern string) *ProcessSensor {
	return &ProcessSensor{pattern: pattern}
}

func (s *ProcessSensor) Name() string { return "process" }

func (s *ProcessSensor) Snapshot(ctx context.Context) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// An empty pattern means "no filter" — on a desktop every tasklist changes
	// constantly, so the hash would never be stable and each snapshot would
	// run an expensive full process listing. Treat it as "not monitoring":
	// report a stable empty state without shelling out.
	if s.pattern == "" {
		return "", "", nil
	}

	current := make(map[string]bool)
	out, err := s.listProcesses()
	if err != nil {
		// Don't fail the whole snapshot on a process-list error; return last hash.
		return "", "", nil
	}
	for _, line := range out {
		if strings.Contains(line, s.pattern) {
			current[line] = true
		}
	}

	s.events = s.events[:0]
	for p := range s.last {
		if !current[p] {
			s.events = append(s.events, SensorEvent{Source: "process", Kind: "vanish", Subject: p, At: time.Now(), Severity: 0})
		}
	}
	for p := range current {
		if !s.last[p] {
			s.events = append(s.events, SensorEvent{Source: "process", Kind: "appear", Subject: p, At: time.Now(), Severity: 0})
		}
	}
	s.last = current

	keys := make([]string, 0, len(current))
	for k := range current {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%s\n", k)
	}
	hash := hex.EncodeToString(h.Sum(nil)[:6])
	digest := fmt.Sprintf("%d processes matching %q", len(current), s.pattern)
	return hash, digest, nil
}

func (s *ProcessSensor) listProcesses() ([]string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("tasklist", "/FO", "CSV", "/NH")
	} else {
		cmd = exec.Command("ps", "aux")
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(out), "\n")
	return lines, nil
}

func (s *ProcessSensor) Changes() []SensorEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SensorEvent, len(s.events))
	copy(out, s.events)
	return out
}

// InterfaceSensor captures UI state. It is intentionally abstract — the real
// implementation depends on the host (Reasonix desktop has screen capture;
// HERMES may have a DOM tree). The default implementation hashes a caller-
// supplied probe string so the sensor is testable without a real display.
type InterfaceSensor struct {
	mu       sync.Mutex
	probeFn  func(ctx context.Context) (string, error)
	lastHash string
	events   []SensorEvent
}

func NewInterfaceSensor(probe func(ctx context.Context) (string, error)) *InterfaceSensor {
	return &InterfaceSensor{probeFn: probe}
}

func (s *InterfaceSensor) Name() string { return "interface" }

func (s *InterfaceSensor) Snapshot(ctx context.Context) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.probeFn == nil {
		return "", "no interface probe", nil
	}
	probe, err := s.probeFn(ctx)
	if err != nil {
		return s.lastHash, "", err
	}
	h := sha256.Sum256([]byte(probe))
	hash := hex.EncodeToString(h[:6])
	s.events = s.events[:0]
	if hash != s.lastHash && s.lastHash != "" {
		s.events = append(s.events, SensorEvent{Source: "interface", Kind: "modify", Subject: "screen", At: time.Now(), Severity: 1})
	}
	s.lastHash = hash
	return hash, fmt.Sprintf("interface hash %s", hash), nil
}

func (s *InterfaceSensor) Changes() []SensorEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SensorEvent, len(s.events))
	copy(out, s.events)
	return out
}

// EventCorrelator batches sensor events across sensors and prioritizes them.
// A single filesystem change might be benign (a log rotated); the same change
// correlated with a process vanishing is likely the task's effect and deserves
// the engine's attention. The correlator groups events by a short time window
// and assigns a composite priority.
type EventCorrelator struct {
	mu       sync.Mutex
	pending  []SensorEvent
	windowMs int
}

func NewEventCorrelator(windowMs int) *EventCorrelator {
	if windowMs <= 0 {
		windowMs = 500
	}
	return &EventCorrelator{windowMs: windowMs}
}

// Ingest collects events from all sensors for correlation.
func (c *EventCorrelator) Ingest(events []SensorEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = append(c.pending, events...)
}

// Flush returns the correlated event batch and clears the buffer. Events within
// the window are grouped; a group with events from 2+ sources is promoted to
// severity 2 (critical) because cross-source correlation signals a real change.
func (c *EventCorrelator) Flush() []SensorEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pending) == 0 {
		return nil
	}
	// Group by time window.
	sort.Slice(c.pending, func(i, j int) bool { return c.pending[i].At.Before(c.pending[j].At) })
	var groups [][]SensorEvent
	var current []SensorEvent
	windowEnd := time.Time{}
	for _, e := range c.pending {
		if e.At.After(windowEnd) && len(current) > 0 {
			groups = append(groups, current)
			current = nil
		}
		if len(current) == 0 {
			windowEnd = e.At.Add(time.Duration(c.windowMs) * time.Millisecond)
		}
		current = append(current, e)
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	c.pending = c.pending[:0]

	// Promote cross-source groups.
	var out []SensorEvent
	for _, g := range groups {
		sources := make(map[string]bool)
		maxSev := 0
		for _, e := range g {
			sources[e.Source] = true
			if e.Severity > maxSev {
				maxSev = e.Severity
			}
		}
		if len(sources) >= 2 && maxSev < 2 {
			maxSev = 2 // cross-source correlation → critical
		}
		merged := SensorEvent{
			Source:   "correlated",
			Kind:     "batch",
			At:       g[0].At,
			Severity: maxSev,
			Detail:   fmt.Sprintf("%d events from %d sources", len(g), len(sources)),
		}
		out = append(out, merged)
	}
	return out
}

// Trim drops the oldest pending events until at most limit remain. It is a
// bound on the buffer for hosts that sample in the background without
// flushing; events are best-effort diagnostics, so dropping old ones is safe.
func (c *EventCorrelator) Trim(limit int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if limit <= 0 || len(c.pending) <= limit {
		return
	}
	c.pending = append([]SensorEvent(nil), c.pending[len(c.pending)-limit:]...)
}

// DynamicEnvSensor orchestrates all attached sensors. The kernel calls
// SnapshotAll before and after each action; the diff between the two calls is
// what feeds the ClosedLoopEngine's comparator.
type DynamicEnvSensor struct {
	sensors    []Sensor
	correlator *EventCorrelator
}

func NewDynamicEnvSensor() *DynamicEnvSensor {
	return &DynamicEnvSensor{correlator: NewEventCorrelator(500)}
}

func (d *DynamicEnvSensor) Add(s Sensor) { d.sensors = append(d.sensors, s) }

// SnapshotAll captures all sensors and returns composite hashes + ingests
// changes into the correlator.
func (d *DynamicEnvSensor) SnapshotAll(ctx context.Context) (ifaceHash, envHash, digest string, err error) {
	var parts []string
	for _, s := range d.sensors {
		hash, _, serr := s.Snapshot(ctx)
		if serr != nil {
			err = serr
			continue
		}
		parts = append(parts, s.Name()+":"+hash)
		d.correlator.Ingest(s.Changes())
		switch s.Name() {
		case "interface":
			ifaceHash = hash
		case "filesystem", "process":
			// Combine env-affecting sensors into envHash.
			if envHash != "" {
				envHash += "|"
			}
			envHash += hash
		}
	}
	digest = strings.Join(parts, " ")
	return
}

// FlushEvents returns the correlated event batch.
func (d *DynamicEnvSensor) FlushEvents() []SensorEvent { return d.correlator.Flush() }

// TrimEvents bounds the pending event buffer, dropping the oldest events when
// the correlator exceeds limit. Used by the background watch so a session with
// churning files cannot grow the buffer without limit while no tool call runs
// to flush it. Events are best-effort diagnostics — dropping the oldest is
// acceptable; the environment hash still carries drift detection.
func (d *DynamicEnvSensor) TrimEvents(limit int) {
	d.correlator.Trim(limit)
}
