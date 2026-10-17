// Package persona composes the user's system persona around the assembled
// system prompt. It is pure — no I/O, no config reading — so the composition
// rule is testable in isolation and its only inputs are explicit.
//
// A persona has a prefix (the agent's identity, leading the prompt) and a suffix
// (trailing the first-party guidance), plus two switches: Complete makes the
// prefix the whole prompt, and IncludeRuntimeContext controls whether optional
// runtime context may appear at all. The result is folded into the cache-stable
// prefix once at boot; a saved change rides a new session, never a rebuild of
// one already running.
package persona
