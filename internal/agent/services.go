package agent

import (
	"strings"
	"sync"

	"reasonix/internal/checkpoint"
	"reasonix/internal/diff"
	"reasonix/internal/event"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/jobs"
	"reasonix/internal/memory"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
	"reasonix/internal/workspacelease"
)

// agentServices are the collaborators an Agent talks to, separated from the
// state it remembers. This is not a lifetime: the controller rebinds most of
// these between turns through the Set* seams, and fork wraps prov mid-run. A
// nil field is a capability the host did not wire, which each reader handles.
type agentServices struct {
	prov  provider.Provider
	tools *tool.Registry
	// pricing turns provider usage into money for the task budget.
	pricing      *provider.Pricing
	quoteContext *event.QuoteContext
	// sink receives the turn's typed event stream. Frontends decide how to
	// render it; never nil because New defaults it to event.Discard.
	sink event.Sink
	// warnState rate-limits recovery retries across sessions and processes by an
	// opaque provider-configuration fingerprint (#7059). The legacy type and file
	// names preserve the on-disk v2 contract. nil keeps in-memory gating only.
	warnState *missingReasoningWarnState
	// gate is the per-call permission gate for both standard and Plan
	// workflows. Runtime approval-mode switches replace it while a turn may be
	// executing, so every read/write goes through gateSnapshot/setGate.
	gateMu sync.RWMutex
	gate   Gate
	// extensions is the frozen Extension Protocol v2 dispatcher for this
	// controller generation; nil means every intercept point passes through
	// byte-identically. See extensions.go.
	extensions *dispatch.Dispatcher
	// recoveryGate is the Auto Guard boundary, shared by root and sub-agents for
	// one controller task. nil disables recovery checks.
	recoveryGate RecoveryGate
	// planTrust is retained for legacy controller wiring. The main Plan
	// execution path no longer consults it.
	planTrust PlanModeReadOnlyTrustGate
	// sandboxEscape can ask the user whether one shell command may rerun
	// unconfined after the OS sandbox failed to start.
	sandboxEscape sandbox.EscapeApprover
	// configWrite can ask the user whether a file tool may write a
	// Reasonix-managed config file outside the workspace roots.
	configWrite tool.ConfigWriteApprover
	// writeRoots is the session-scoped writable directory manager.
	writeRoots *sandbox.WritableRootSet
	// writeAccess authorizes extra writable directories. nil skips expansion
	// except for a fail-closed missing-dir check when writeRoots is set.
	writeAccess WriteAccessGate
	// writeAccessExpandable is false for sub-agents: they inherit roots but
	// cannot request new directories.
	writeAccessExpandable bool
	workspaceRoot         string
	homeDir               string
	stateRoot             string
	// hooks fires PreToolUse / PostToolUse shell hooks around each tool call.
	hooks ToolHooks
	// asker lets the `ask` tool put questions to the user; nil in headless runs.
	asker Asker
	// preEdit is the seam the checkpoint store uses to snapshot pre-edit
	// content. Only non-ReadOnly tool.Previewer tools fire it, so bash — whose
	// targets are unknowable — is never tracked. Prefer mutationObserver.
	preEdit func(diff.Change)
	// mutationObserver is the host-side unified file mutation observer: it
	// captures preimages before tools run and fingerprints after, regardless of
	// outcome. Never changes provider-visible schemas or prompts.
	mutationObserver *checkpoint.MutationObserver
	// jobs is the session's background-job manager, stamped onto each tool
	// call's context so the background tools can reach it. nil degrades
	// gracefully.
	jobs *jobs.Manager
	// writeScheduler coordinates parent-agent writes against background
	// subagent write claims. Set on the parent executor only.
	writeScheduler *SubagentScheduler
	// workspaceLease is shared by every writer-capable agent in one Delivery
	// session, acquired lazily on the first mutation and held through the final
	// participating run so verification stays isolated.
	workspaceLease *workspacelease.Owner
	// memQueue lets the remember/forget tools fold a turn-tail note about a
	// just-made memory change into the next turn, so it applies this session
	// without touching the cache-stable prefix.
	memQueue memory.Queue
}

func (s *agentServices) gateSnapshot() Gate {
	s.gateMu.RLock()
	defer s.gateMu.RUnlock()
	return s.gate
}

func (s *agentServices) setGate(g Gate) {
	s.gateMu.Lock()
	s.gate = g
	s.gateMu.Unlock()
}

// newAgentServices binds the collaborators New resolved. It exists so New stays
// under the function-size limit and so adding a collaborator touches one place.
func newAgentServices(
	prov provider.Provider, tools *tool.Registry, sink event.Sink, gate Gate,
	planTrust PlanModeReadOnlyTrustGate, sandboxEscape sandbox.EscapeApprover,
	configWrite tool.ConfigWriteApprover, hooks ToolHooks, opts Options,
) agentServices {
	return agentServices{
		prov:                  prov,
		tools:                 tools,
		pricing:               opts.Pricing,
		quoteContext:          opts.QuoteContext,
		sink:                  sink,
		gate:                  gate,
		extensions:            opts.Extensions,
		recoveryGate:          opts.RecoveryGate,
		planTrust:             planTrust,
		sandboxEscape:         sandboxEscape,
		configWrite:           configWrite,
		hooks:                 hooks,
		jobs:                  opts.Jobs,
		memQueue:              opts.MemoryQueue,
		writeScheduler:        opts.WriteScheduler,
		workspaceLease:        opts.WorkspaceLease,
		warnState:             missingReasoningWarnStateFor(opts.MissingReasoningWarnStateDir),
		mutationObserver:      opts.MutationObserver,
		writeRoots:            opts.WriteRoots,
		writeAccess:           opts.WriteAccessGate,
		writeAccessExpandable: opts.SubagentDepth == 0 && !opts.DisableWriteAccessExpand,
		workspaceRoot:         strings.TrimSpace(opts.WriteWorkspaceRoot),
		homeDir:               strings.TrimSpace(opts.HomeDir),
		stateRoot:             strings.TrimSpace(opts.StateRoot),
	}
}
