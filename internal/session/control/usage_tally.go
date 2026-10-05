package control

import (
	"maps"
	"strings"
	"sync"

	"reasonix/internal/contract/event"
	"reasonix/internal/state/usagereport"
)

// usageTally accumulates the session's billed tokens off the event stream. It
// is a strict leaf: it never calls back into the Controller, and it does not
// own where the record goes — the session's own persistence point supplies the
// path, so the two can never disagree about which session was being written.
type usageTally struct {
	mu     sync.Mutex
	usage  usagereport.Usage
	writes func(string, usagereport.Report) error
}

func newUsageTally() *usageTally {
	return &usageTally{writes: usagereport.Save}
}

// observe folds one usage event in. The caller has already established that
// this is a billable Usage event.
func (t *usageTally) observe(e event.Event) {
	if t == nil || e.Usage == nil {
		return
	}
	u := e.Usage
	source := usageSourceKey(e.UsageSource)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.usage.PromptTokens += u.PromptTokens
	t.usage.CompletionTokens += u.CompletionTokens
	t.usage.ReasoningTokens += u.ReasoningTokens
	t.usage.CacheHitTokens += u.CacheHitTokens
	t.usage.CacheMissTokens += u.CacheMissTokens
	t.usage.RequestCount++
	t.usage.Estimated = t.usage.Estimated || u.Estimated
	if t.usage.Sources == nil {
		t.usage.Sources = map[string]usagereport.Source{}
	}
	entry := t.usage.Sources[source]
	entry.RequestCount++
	t.usage.Sources[source] = entry
	if t.usage.ColdStart == nil && source == event.UsageSourceExecutor {
		t.usage.ColdStart = &usagereport.ColdStart{
			PromptTokens:    u.PromptTokens,
			CacheHitTokens:  u.CacheHitTokens,
			CacheMissTokens: u.CacheMissTokens,
		}
	}
}

// writeTo persists the record beside the session at path. A session with no
// path and one that billed nothing both write nothing rather than a record
// saying zero, which a reader cannot tell from a session that made no calls.
func (t *usageTally) writeTo(path string) error {
	if t == nil || path == "" {
		return nil
	}
	t.mu.Lock()
	if t.usage.RequestCount == 0 {
		t.mu.Unlock()
		return nil
	}
	report := usagereport.Report{Version: usagereport.Version, Usage: t.snapshotLocked()}
	write := t.writes
	t.mu.Unlock()
	return write(path, report)
}

// snapshotLocked copies the accounting so the marshal cannot race the next
// event. Callers hold mu.
func (t *usageTally) snapshotLocked() usagereport.Usage {
	out := t.usage
	out.Sources = maps.Clone(t.usage.Sources)
	if t.usage.ColdStart != nil {
		cold := *t.usage.ColdStart
		out.ColdStart = &cold
	}
	return out
}

// usageSourceKey names the origin a usage event is billed to. An empty source
// means the executor: the field postdates it, and every reader that splits by
// source has always resolved it that way.
func usageSourceKey(source string) string {
	if s := strings.TrimSpace(source); s != "" {
		return s
	}
	return event.UsageSourceExecutor
}
