package main

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/base/locktrace"
	"reasonix/internal/state/mutation"
)

type phaseSpan struct {
	Phase   string `json:"phase"`
	StartNS int64  `json:"start_ns"`
	EndNS   int64  `json:"end_ns"`
}

type workerTrace struct {
	Worker      int              `json:"worker"`
	EpochUnixNS int64            `json:"epoch_unix_ns"`
	Locks       []locktrace.Span `json:"locks"`
	Phases      []phaseSpan      `json:"phases"`
}

type workerProbe struct {
	collector   *locktrace.Collector
	stop        func()
	epochUnixNS int64
	phases      []phaseSpan
	waits       map[string]int64
}

func startWorkerProbe(enabled bool) (*workerProbe, error) {
	if !enabled {
		return nil, nil
	}
	c, stop, err := locktrace.Start()
	if err != nil {
		return nil, err
	}
	return &workerProbe{collector: c, stop: stop, epochUnixNS: c.EpochUnixNano(), waits: map[string]int64{}}, nil
}

func (p *workerProbe) close() {
	if p != nil {
		p.stop()
	}
}

func (p *workerProbe) phase(name string) func() {
	if p == nil {
		return func() {}
	}
	started := p.collector.Elapsed()
	return func() {
		p.phases = append(p.phases, phaseSpan{Phase: name, StartNS: started, EndNS: p.collector.Elapsed()})
	}
}

type interval struct{ start, end int64 }

func (p *workerProbe) finish(worker int, state string, m *measurement) error {
	if p == nil {
		return nil
	}
	spans, complete := p.collector.Snapshot()
	if !complete {
		return errors.New("OS lock recording exceeded its bound")
	}
	if len(p.waits) != 0 {
		return errors.New("resource wait event remained open")
	}
	var lower, upper, registry, preparation, inference []interval
	for _, span := range p.phases {
		if span.Phase == "inference" {
			inference = append(inference, interval{span.StartNS, span.EndNS})
		}
		if span.Phase == "resource_wait" {
			m.ResourceWaitNS += span.EndNS - span.StartNS
		}
	}
	for n := range spans {
		s := &spans[n]
		if s.ReleasedNS == 0 || s.ReleasingNS < s.AcquiredNS || s.ReleasedNS < s.ReleasingNS || s.AcquiredNS < s.AttemptNS {
			return errors.New("OS lock remains active or has invalid timing")
		}
		rel, err := filepath.Rel(state, s.Path)
		if err != nil || !filepath.IsLocal(rel) {
			return errors.New("OS lock outside benchmark state")
		}
		s.Path = filepath.ToSlash(rel)
		span := interval{s.AcquiredNS, s.ReleasingNS}
		switch {
		case strings.HasSuffix(s.Path, ".guard") || filepath.Base(s.Path) == "registry.lock":
			registry = append(registry, span)
		case strings.HasSuffix(s.Path, ".preparing"):
			preparation = append(preparation, span)
		case strings.Contains(s.Path, ".claims/") || (!s.Shared && !strings.Contains(s.Path, "/") && strings.HasSuffix(s.Path, ".lock")):
			lower = append(lower, span)
			upper = append(upper, interval{s.AttemptNS, s.ReleasedNS})
		}
	}
	lower, upper = unionIntervals(lower), unionIntervals(upper)
	m.ResourceHeldLowerNS, m.ResourceHeldUpperNS = sumIntervals(lower), sumIntervals(upper)
	m.RegistryHeldNS, m.PreparationNS = sumIntervals(unionIntervals(registry)), sumIntervals(unionIntervals(preparation))
	for _, held := range upper {
		for _, thinking := range inference {
			m.InferenceWithResourceNS += max(int64(0), min(held.end, thinking.end)-max(held.start, thinking.start))
		}
	}
	m.Traces = []workerTrace{{Worker: worker, EpochUnixNS: p.epochUnixNS, Locks: spans, Phases: slices.Clone(p.phases)}}
	return nil
}

func (p *workerProbe) notice(n mutation.Notice) {
	if p == nil {
		return
	}
	if n.State == "waiting" {
		p.waits[n.TransactionID] = p.collector.Elapsed()
		return
	}
	if n.State == "acquired" || n.State == "abandoned" {
		if start, ok := p.waits[n.TransactionID]; ok {
			p.phases = append(p.phases, phaseSpan{Phase: "resource_wait", StartNS: start, EndNS: p.collector.Elapsed()})
			delete(p.waits, n.TransactionID)
		}
	}
}

func unionIntervals(spans []interval) []interval {
	slices.SortFunc(spans, func(a, b interval) int {
		if a.start < b.start {
			return -1
		}
		if a.start > b.start {
			return 1
		}
		return 0
	})
	var out []interval
	for _, span := range spans {
		if len(out) == 0 || span.start > out[len(out)-1].end {
			out = append(out, span)
		} else {
			out[len(out)-1].end = max(out[len(out)-1].end, span.end)
		}
	}
	return out
}

func sumIntervals(spans []interval) int64 {
	var total int64
	for _, span := range spans {
		total += span.end - span.start
	}
	return total
}
