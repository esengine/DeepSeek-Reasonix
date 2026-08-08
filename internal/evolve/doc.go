// Package evolve implements Reasonix session-learning proposals: validated,
// evidence-backed suggestions derived from recent project history that a human
// confirms before they land in background memory (L0) or standing instructions
// such as AGENTS.md (L1).
//
// Apply writes disk targets only. It never hot-reloads the boot-time
// memory/system-prompt Compose prefix for the live session; callers may queue a
// turn-tail notice so the current turn learns about the change, while the stable
// prefix updates on the next boot/load.
//
// This package is a pure lifecycle layer: no control/cli/desktop imports, so
// unit tests drive Validate/Apply on temporary directories.
package evolve
