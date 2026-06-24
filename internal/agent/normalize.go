package agent

import "reasonix/internal/provider"

// NormalizeSession runs the persisted-history-safe repairs on a loaded
// conversation and is the agent-side entry point for making old, partially
// saved, or interrupted sessions replayable. It is a thin wrapper over
// provider.NormalizeSessionMessages, which shares assistant-turn repairs with
// the provider send path without applying wire-only cleanup such as dropping
// standalone tool messages.
//
// LoadSession calls this right after decoding so a session that was written by
// an older code version, or that was cut short mid-turn, is corrected in memory
// before anything reads it. The corrected messages are persisted lazily: the
// next Session.Save (naturally triggered by the following turn) rewrites the
// whole file with the repairs baked in, so the same stale-data bug is not
// re-repaired on every turn forever. A session that is only ever read (never
// appended to) stays unmodified on disk and is simply re-normalized on the next
// load — cheap, because the fast path returns the input slice unchanged.
//
// Well-formed histories are returned without allocating (see
// provider.NormalizeSessionMessages), so this is a no-op in both time and memory
// for the common case and cannot perturb a provider's prefix-cache key.
func NormalizeSession(msgs []provider.Message) []provider.Message {
	return provider.NormalizeSessionMessages(msgs)
}
