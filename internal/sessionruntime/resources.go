// Package sessionruntime owns the session-scoped execution resources that must
// survive a desktop controller rebuild (model switch) while background jobs and
// Delivery writers remain live.
//
// A Controller holds one reference. Replacing the controller retains the shared
// Resources for the replacement, then releases the old controller's reference.
// The last release stops background jobs, waits for them to exit, then closes
// LSP and temporary job artifacts so a late subagent never talks to a dead
// language server.
package sessionruntime

import (
	"strings"
	"sync"

	"reasonix/internal/agent"
	"reasonix/internal/jobs"
	"reasonix/internal/lsp"
	"reasonix/internal/workspacelease"
)

// Resources is the refcounted bag of session-scoped runtime state shared across
// controller rebuilds for one chat session.
type Resources struct {
	Jobs           *jobs.Manager
	Scheduler      *agent.SubagentScheduler
	WorkspaceLease *workspacelease.Owner
	LSP            *lsp.Manager

	// WorkspaceKey is the canonical workspace identity used when this bag was
	// created. Empty when the workspace was unavailable at creation.
	WorkspaceKey string
	// RuntimeKey identifies the runtime profile (token mode / delivery) the bag
	// was built for. Model switches keep the same key; token-mode rebuilds do
	// not reuse a bag with a different key.
	RuntimeKey string
	// ConfigKey fingerprints configuration owned by the shared components
	// (background jobs, scheduler, and LSP). A controller rebuild must not reuse
	// the bag when that effective configuration changed.
	ConfigKey string

	mu      sync.Mutex
	refs    int
	closing bool
	closed  bool
	done    chan struct{}
}

// Config supplies the components owned by a newly created Resources bag.
type Config struct {
	Jobs           *jobs.Manager
	Scheduler      *agent.SubagentScheduler
	WorkspaceLease *workspacelease.Owner
	LSP            *lsp.Manager
	WorkspaceKey   string
	RuntimeKey     string
	ConfigKey      string
}

// New returns a Resources bag with one reference held for the first consumer
// (normally the Controller built by boot).
func New(cfg Config) *Resources {
	return &Resources{
		Jobs:           cfg.Jobs,
		Scheduler:      cfg.Scheduler,
		WorkspaceLease: cfg.WorkspaceLease,
		LSP:            cfg.LSP,
		WorkspaceKey:   strings.TrimSpace(cfg.WorkspaceKey),
		RuntimeKey:     strings.TrimSpace(cfg.RuntimeKey),
		ConfigKey:      strings.TrimSpace(cfg.ConfigKey),
		refs:           1,
		done:           make(chan struct{}),
	}
}

// WrapJobs is the compatibility entry for callers that only inject a
// jobs.Manager (tests and older Options.Jobs wiring). Final release closes the
// manager the same way a full bag would.
func WrapJobs(jm *jobs.Manager) *Resources {
	return New(Config{Jobs: jm})
}

// Retain adds a consumer reference. Returns false when the bag is already
// closing or closed — the caller must not use the components in that case.
func (r *Resources) Retain() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.closing {
		return false
	}
	r.refs++
	return true
}

// Release drops one consumer reference. The last release runs final cleanup
// exactly once: stop jobs, wait for exit, close LSP, then mark done.
func (r *Resources) Release() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	if r.refs > 0 {
		r.refs--
	}
	if r.refs > 0 || r.closing {
		r.mu.Unlock()
		return
	}
	r.closing = true
	r.mu.Unlock()
	r.finalize()
}

// Refs reports the current reference count (tests and diagnostics).
func (r *Resources) Refs() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.refs
}

// Closed reports whether final cleanup has finished.
func (r *Resources) Closed() bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// Done is closed after the last release finishes final cleanup.
func (r *Resources) Done() <-chan struct{} {
	if r == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	return done
}

// CompatibleWith reports whether r can be reused for a controller rebuild that
// targets the same workspace, runtime profile, and resource-owned
// configuration. Empty expected keys are treated as "unspecified"; an explicit
// config key still requires an exact match.
func (r *Resources) CompatibleWith(workspaceKey, runtimeKey, configKey string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.closing {
		return false
	}
	workspaceKey = strings.TrimSpace(workspaceKey)
	runtimeKey = strings.TrimSpace(runtimeKey)
	configKey = strings.TrimSpace(configKey)
	if workspaceKey != "" && r.WorkspaceKey != "" && workspaceKey != r.WorkspaceKey {
		return false
	}
	if runtimeKey != "" && r.RuntimeKey != "" && runtimeKey != r.RuntimeKey {
		return false
	}
	// ConfigKey is intentionally strict when the rebuild supplies an expected
	// key. Unlike legacy workspace/runtime wildcards, an unkeyed bag cannot
	// prove that its LSP/scheduler/job settings match the current configuration.
	if configKey != "" && configKey != r.ConfigKey {
		return false
	}
	return true
}

func (r *Resources) finalize() {
	// Order is intentional: background jobs (and their subagents) may still
	// call into LSP until they exit. Jobs close first and Done waits until
	// every job goroutine and temp-root cleanup has finished. Close itself is
	// grace-bounded; if a non-cooperative job is still unwinding, finish the
	// dependent cleanup asynchronously so Controller.Close keeps that bound.
	if r.Jobs != nil {
		r.Jobs.Close()
		select {
		case <-r.Jobs.Done():
		default:
			go func() {
				<-r.Jobs.Done()
				r.completeFinalize()
			}()
			return
		}
	}
	r.completeFinalize()
}

func (r *Resources) completeFinalize() {
	if r.LSP != nil {
		r.LSP.Close()
	}
	// Workspace lease is released automatically when jobs (RetainUntil) and
	// agent runs unwind. No explicit Close is required on Owner.

	r.mu.Lock()
	r.closed = true
	r.closing = false
	done := r.done
	r.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		default:
			close(done)
		}
	}
}
