// Package browser implements the agent-facing browser tools over one
// host-neutral Executor. The local Electron shell and the remote SSH broker
// each provide an Executor; the tools never learn which one answers.
//
// The tools are registry-only. Boot registers them so use_capability can list
// and call tool:browser_* targets, but they never join the provider-visible
// schema array. That array is part of the cache-stable system-prompt prefix
// and must stay byte-identical whether or not a browser is attached: a desktop
// session with a browser and a CLI session without one share the same prefix,
// which is what keeps DeepSeek's automatic prefix cache warm across both.
// Every tool also implements tool.ContextualTool, so a build without an
// Executor, or an Executor whose grant lapsed, fails closed with a blocked
// card instead of dispatching.
//
// Writes are reserved by the model, not minted here: each write takes an
// operationId that must be unique per attempt and is rejected forever once
// used. Reference-bound writes (click, type, press, scroll, select, upload)
// also carry the documentToken of the snapshot they were planned against. A
// navigation or user take-over invalidates it; the executor then answers
// ErrStaleReference or ErrTakenOver, which the tools translate into
// tool.Blocked results so the model re-reads the page instead of guessing. A
// lost receipt is ErrUnknownOutcome: the action may or may not have run, and
// the error text tells the model it must not be retried.
package browser
