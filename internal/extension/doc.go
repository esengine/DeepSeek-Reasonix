// Package extension is the unified extension kernel: every capability the
// runtime exposes — tools, skills, commands, prompts, hooks, MCP servers,
// providers, themes, interceptors, and replacement slots — is modeled as a
// Contribution with explicit provenance, assembled by a deterministic Builder
// into an immutable RuntimeSnapshot.
//
// Spatiotemporal composability (plugin/runtime v2) adds EffectScope-owned live
// resources, a typed DependencyGraph, component epochs, and RuntimePlan-driven
// activate/publish/drain. RuntimeSnapshot stays configuration-only; live
// handles (processes, streams, watchers, goroutines) never enter the snapshot.
//
// The kernel exists so that shadowing, conflicts, and ordering stop being
// emergent side effects of whichever discovery path ran last. Discovery still
// lives in the existing packages (internal/skill, internal/command,
// internal/hook, internal/tool, internal/plugin, internal/provider); the
// adapters in adapters.go only wrap that discovery in the Contributor
// interface. Winner rules are resolved here, once, identically for every
// caller: a higher-tier scope shadows a lower one for the same canonical ID,
// same-tier duplicates from different sources are hard conflicts instead of
// silent overrides, and hooks/interceptors stay additive. Callers assembling
// pre-kernel legacy resources (boot) can opt into ConflictCollect, which
// records such conflicts on the snapshot's Diagnostics and keeps the
// deterministic winner instead of failing the build; native extensions keep
// the default ConflictFail.
//
// A RuntimeSnapshot is immutable after Freeze: every accessor returns
// defensive copies, so a snapshot can be shared across turns, subagents, and
// frontends without locking. CacheHash fingerprints exactly the
// provider-visible prefix state (system prompt + canonical tool schemas), so
// a snapshot rebuild that changes nothing observable reuses the same hash and
// preserves provider-side prompt caching.
package extension
