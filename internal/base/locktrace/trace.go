// Package locktrace records opt-in process-local OS lock intervals for qualification.
package locktrace

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

const recordLimit = 1_000_000

var current atomic.Pointer[Collector]

type Span struct {
	Path        string `json:"path"`
	Shared      bool   `json:"shared"`
	AttemptNS   int64  `json:"attempt_ns"`
	AcquiredNS  int64  `json:"acquired_ns"`
	ReleasingNS int64  `json:"releasing_ns"`
	ReleasedNS  int64  `json:"released_ns"`
}

type Collector struct {
	mu       sync.Mutex
	epoch    time.Time
	spans    []Span
	overflow bool
}

// Start installs one bounded recorder. Stopping admission retains live holds
// in their original recorder until their actual OS release completes.
func Start() (*Collector, func(), error) {
	c := &Collector{epoch: time.Now()}
	if !current.CompareAndSwap(nil, c) {
		return nil, nil, errors.New("OS lock recording is already active")
	}
	return c, func() { current.CompareAndSwap(c, nil) }, nil
}

type Attempt struct {
	collector *Collector
	started   int64
}

func BeginAttempt() Attempt {
	c := current.Load()
	if c == nil {
		return Attempt{}
	}
	return Attempt{collector: c, started: time.Since(c.epoch).Nanoseconds()}
}

type Hold struct {
	collector *Collector
	index     int
}

func (a Attempt) Acquired(path string, shared bool) *Hold {
	c := a.collector
	if c == nil {
		return nil
	}
	at := time.Since(c.epoch).Nanoseconds()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.spans) >= recordLimit {
		c.overflow = true
		return nil
	}
	index := len(c.spans)
	c.spans = append(c.spans, Span{Path: path, Shared: shared, AttemptNS: a.started, AcquiredNS: at})
	return &Hold{collector: c, index: index}
}

func (h *Hold) Releasing() {
	if h == nil {
		return
	}
	c := h.collector
	at := time.Since(c.epoch).Nanoseconds()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.spans[h.index].ReleasingNS == 0 {
		c.spans[h.index].ReleasingNS = at
	}
}

func (h *Hold) Released() {
	if h == nil {
		return
	}
	c := h.collector
	at := time.Since(c.epoch).Nanoseconds()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.spans[h.index].ReleasedNS == 0 {
		c.spans[h.index].ReleasedNS = at
	}
}

func (c *Collector) Snapshot() ([]Span, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.spans), !c.overflow
}

func (c *Collector) Elapsed() int64 { return time.Since(c.epoch).Nanoseconds() }

func (c *Collector) EpochUnixNano() int64 { return c.epoch.UnixNano() }
