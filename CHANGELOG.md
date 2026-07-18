# Changelog

All notable changes to the Go line (Reasonix 1.0+) are recorded here. The legacy
`0.x` TypeScript history lives on the [`v1`](https://github.com/esengine/DeepSeek-Reasonix/tree/v1)
branch.

## Unreleased

### Added

- Added Claude Code-style searchable CLI pickers for models, providers, and
  sessions, with arrow, Vim, and `Ctrl+P` / `Ctrl+N` navigation.
- Added `-p` / `--print`, `text`, `json`, and `stream-json` output modes for
  one-shot use and automation.
- Added session-scoped `--allowed-tools`, repeatable `--add-dir`, Claude-compatible
  permission modes, flexible `--resume [QUERY]`, and the `--copy` resume escape
  hatch.
- Added `/status` details for the active model, effort, cache, Git state,
  background jobs, work profile, and provider balance where available.

### Changed

- `Shift+Tab` now cycles CLI safe modes from Ask to Auto to Plan, while YOLO
  remains an independent `Ctrl+Y` toggle.
- Model, provider, resume, and approval menus now use consistent row selection;
  slash completion, help, aliases, and dispatch share one command registry.
- The full-screen CLI composer now uses theme-accented borders and a slim bar
  cursor by default, grows within the available terminal height, scrolls long
  drafts independently, and preserves selections across explicit image paste.
- The persistent CLI footer now uses a responsive, theme-aware layout for
  interaction state, model, effort, localized work mode, Git identity, cache,
  context, compaction headroom, jobs, and balance. Narrow terminals move or
  compact complete groups instead of clipping labels.
- CLI clipboard actions now separate terminal-native text paste from explicit
  image paste: `Ctrl+V` on macOS/Linux, `Alt+V` on Windows, or `/paste-image`.
  Local transcript copy verifies the native clipboard write, while SSH uses a
  clearly labelled OSC 52 fallback.
- Runtime rebuilds after model, effort, or work-mode changes now preserve the
  conversation, session permission overrides, additional directories, and
  session lease ownership.
- Agent execution now monitors host-observed Todo progress automatically. A
  stalled current item receives a recovery nudge after 8 tool-call rounds with
  no new completion, unique read, command, or mutation, and pauses with saved
  work after 16. Exact repeats do not renew the progress lease; real work does.
  Two-level task lists keep the single in_progress contract: the active
  sub-step is the only current item while its phase stays pending, and the
  phase becomes in_progress to sign off only after all of its sub-steps are
  completed. A level-1 sub-step with no phase header above it is rejected.
  Executor and planner rounds now use automatic progress management. Retired
  `[agent].max_steps` and `planner_max_steps` keys remain parseable for upgrades,
  but are ignored and removed by a one-time migration so stale hidden limits
  cannot truncate new behavior. One-off CLI and unattended bot limits remain.

### Fixed

- Restored captured-mouse right-click text paste, made composer drag selection
  copy through the verified native clipboard path, and kept non-Git footer
  telemetry left-aligned without reserving an empty data band.
- Restored stateful MCP behavior after the v1.17.13 regression: compatible
  trust receipts migrate instead of prompting again, user-added servers work
  without extra trust settings (including delivery-mode on-demand calls), and
  stdio tools reuse one persistent process so browser sessions survive across
  calls without repeated startup latency. Authorized project servers keep a
  revoke entry in the desktop server details page.
- Localized persistent-footer labels and displayed work-mode values in English,
  Simplified Chinese, and Traditional Chinese, while keeping command arguments
  stable.
- Restored the `0.53` content boundary: model output, tool output, session
  transcripts, recovery branches, and background-job artifacts retain their
  original text instead of being rewritten by heuristic secret redaction.
  Credential masking remains in key-entry summaries and explicit diagnostic or
  session-cleanup paths. Transcript-bearing session/job sidecars are kept
  private (`0600`, with private job directories), and the retired
  `redact_tool_output` setting is removed with a one-time upgrade notice.

## [1.0.0] — 2026-06-03

First stable release — a **ground-up rewrite in Go**. Not an upgrade of the `0.x`
TypeScript line; a new codebase that becomes the default (`main-v2`).

### Highlights

- **Go kernel**: a single static binary (CGO-free), cross-compiled for
  darwin/linux/windows on amd64 + arm64. Distributed via npm (the package wraps
  the native binary), Homebrew (`esengine/reasonix` tap), and release archives;
  no Node runtime needed to run it.
- **Agent core**: the loop, built-in tools (read/write/edit/multi_edit/glob/grep/
  ls/bash/web_fetch/todo_write), permission gate, sandboxed bash, and the
  DeepSeek prefix-cache–oriented design.
- **Subagents**: `task` plus explore/research/review/security_review skill agents.
- **Skills & hooks**: Claude-Code-style skills (`internal/skill`) and hooks
  (`internal/hook`), symlink-aware and slash-integrated.
- **MCP client**: connect external servers over stdio / Streamable HTTP; reads
  `[[plugins]]` and a Claude-Code `.mcp.json`.
- **Code intelligence via CodeGraph**: a tree-sitter symbol/call graph
  (`codegraph_*` tools) replaces embedding semantic search — no embedding service
  or API cost. Fetched into a local cache on first use (or `reasonix codegraph
  install`) and indexed in the background, so installs and startup stay fast.
- **Plan mode** with evidence-backed step sign-off (`complete_step`).
- **Memory**: `REASONIX.md` hierarchy + auto-memory, folded into the cache-stable
  prefix.
- **ACP** (`reasonix acp`) and an HTTP/SSE server frontend; desktop app (Wails).

### Fixed

- **File encoding support restored** — GBK/GB18030 (and other non-UTF-8) files
  can now be read, edited, and grepped correctly. The v2 rewrite had dropped
  v1's encoding detection; files in CJK Windows charsets were silently misread
  or rejected as binary. The read/edit/write round-trip now preserves the
  original file encoding. (#2637)

### Notes

- Versions: the legacy TypeScript line stays in `0.x`; the Go line starts at
  `1.0.0`. See [docs/MIGRATING.md](docs/MIGRATING.md).
- Release archives ship a bare binary; CodeGraph is fetched on first use. Windows
  support for the fetched runtime is unverified — install `codegraph` on PATH if
  the auto-fetch doesn't resolve there.

[1.0.0]: https://github.com/esengine/DeepSeek-Reasonix/releases/tag/v1.0.0
