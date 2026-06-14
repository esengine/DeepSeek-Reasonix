# Reasonix AgentOS File Watching Decision

This note records the Phase 8 file-watching decision for the personal AgentOS
roadmap. File watching is part of the daemon runtime surface: it observes local
workspace changes, writes deterministic runtime/timeline state, and wakes a
session with a bounded summary. It must not add large file contents or unstable
state to the provider prompt prefix.

## Decision

Use the existing portable polling watcher as the MVP implementation.

- The daemon polls configured paths on a fixed lightweight interval.
- Per-session debounce merges rapid changes before a wakeup.
- Ignore rules exclude dependency folders, VCS metadata, build output, common
  secret material, and user-provided patterns.
- Wakeup context contains only path summaries, capped by
  `maxFileWatchSummaryFiles`; raw file contents are not injected.
- Matching explicit file waits clears the wait and unregisters the one-shot
  watcher; non-matching waits are logged in the runtime timeline.

This keeps the first local-file automation path dependency-light and consistent
across macOS, Linux, and Windows without adding platform-specific watcher
failure modes to the daemon.

## Alternatives Considered

### fsnotify / platform-native events

`fsnotify` would reduce idle directory walking and provide lower latency on
large watched trees. It also introduces behavior that differs by backend:
recursive watching, rename coalescing, editor atomic-save patterns, queue
overflow, network filesystems, and symlink behavior are not uniform across
platforms. Those edge cases matter because Reasonix uses file changes to resume
long-lived work; a missed or duplicated event can turn into duplicate model
work or a stuck wait.

Native events are a good later optimization, but they should sit behind the same
semantic contract as the polling watcher:

- same ignore rules;
- same debounce and batching behavior;
- same bounded prompt context;
- same wait matching and timeline events;
- same budget and in-flight run guards;
- same deterministic fallback to polling when native watches fail.

### Hybrid native watcher with polling fallback

A hybrid design is the likely upgrade path after the MVP. Native events can
feed the same debounce queue, while periodic polling can reconcile missed events
or unsupported paths. This should be introduced only after the daemon exposes
enough diagnostics to explain which backend is active for a session and why a
fallback happened.

## Current Evidence

The current implementation is in `internal/daemon/filewatcher.go`. Existing
tests cover:

- explicit file wait match clears the wait and wakes once;
- non-matching file waits do not wake;
- event/time waits are not woken by file changes;
- rapid changes are merged into one wakeup;
- ignored paths are skipped;
- active runs are not woken concurrently;
- daily automatic wakeup budget blocks file-watch wakeups.

## Switch Criteria

Revisit native file events when one of these becomes true:

- polling causes measurable CPU or I/O pressure on real project workspaces;
- users need sub-second file change latency;
- large monorepos need broad recursive watches that polling cannot handle
  cheaply;
- diagnostics can show backend choice, dropped events, fallback reason, and last
  scan duration;
- regression tests cover atomic saves, renames, deletes, symlinks, ignored
  directories, and fallback to polling.

Until then, portable polling is the selected implementation for the personal
AgentOS MVP.
