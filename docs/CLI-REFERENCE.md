# Reasonix CLI Reference

Every shell subcommand, every TUI slash command, every keybinding. The in-app `/help` and `/keys` panels are the live source of truth — this page is the printable companion.

---

## Shell subcommands

Run `reasonix --help` (or any subcommand with `--help`) for the full flag list. Headline subcommands:

| Subcommand | What it does |
|---|---|
| `reasonix code [dir]` | Code-mode TUI — file edits, plan mode, edit-gate, project-scoped sessions |
| `reasonix chat` | Chat-only TUI — no filesystem access, no code mode |
| `reasonix run <task>` | Headless run — read prompt, execute, exit (CI-friendly) |
| `reasonix setup` | Interactive first-run config (API key, language, theme) |
| `reasonix sessions [name]` | List or open a saved session |
| `reasonix prune-sessions` | Drop sessions older than `--days N` |
| `reasonix replay <transcript>` | Re-render a JSONL transcript without calling the model |
| `reasonix diff <a> <b>` | Compare two transcripts (cost / cache / tokens) |
| `reasonix events <name>` | Tail the event log for a session |
| `reasonix stats [transcript]` | One-shot cost / cache breakdown |
| `reasonix doctor` | Health check — API reach, config, hooks, project |
| `reasonix commit` | `git add -A && git commit` with an LLM-written message |
| `reasonix mcp <list\|search\|install\|inspect\|browse>` | MCP server management |
| `reasonix index` | Build the local semantic index (Ollama or OpenAI-compatible embeddings) |
| `reasonix version` / `reasonix update` | Version info + upgrade hint |

### Notable runtime flags (chat / code)

| Flag | Effect |
|---|---|
| `--no-session` | Ephemeral run — nothing is persisted |
| `--session <name>` | Resume / pin to a named session |
| `--continue` | Resume the most recent session for this workspace |
| `--new` | Force a fresh session even if one exists |
| `--budget <usd>` | Per-session USD cap — warns at 80%, refuses next turn at 100% |
| `--preset <auto\|flash\|pro>` | Model bundle (auto-escalation, locked flash, locked pro) |
| `--mcp <spec>` | Attach an MCP server for this run (repeatable) |
| `--no-config` | Ignore `~/.reasonix/config.json` for this run |
| `--no-dashboard` | Don't auto-start the embedded web dashboard |
| `--no-alt-screen` | Render to scrollback instead of the alt-screen buffer (preserves chat in shell history; legacy mode, can ghost on resize) |
| `--no-mouse` | Disable DECSET 1007 (alternate-scroll); wheel reverts to native terminal scroll |

---

## Slash commands

Type `/` mid-chat to open the picker. Aliases shown in parentheses. Code-mode-only commands marked **(code)**.

### Chat ops

| Command | What it does |
|---|---|
| `/help` (`/?`) | Show the full command reference inline |
| `/new` (`/reset`, `/clear`) | Start a fresh conversation (clear context + scrollback) |
| `/retry` | Truncate and resend your last message — fresh sample |
| `/compact` | Fold older turns into a summary (cache-safe). Auto-fires at 50% ctx; this is the manual trigger |
| `/stop` | Abort the current model turn (typed alternative to Esc) |
| `/copy` | Open vim/tmux-style copy mode — `j`/`k` navigate, `v` select, `y` yank to clipboard. The right answer for SSH / mosh / tmux where drag-select can't extend past the viewport |

### Setup

| Command | What it does |
|---|---|
| `/preset <auto\|flash\|pro>` | Switch model bundle. Bare opens picker |
| `/model <id>` | Switch DeepSeek model id. Bare opens picker |
| `/language <EN\|zh-CN>` (`/lang`) | Switch the runtime language |
| `/theme <name>` | Show or persist terminal theme. Bare opens picker |

### Info

| Command | What it does |
|---|---|
| `/status` | Current model, flags, context, session |
| `/cost [text]` | Bare → last turn's spend; with text → estimate cost of sending it next |
| `/context` | Context-window breakdown (system / tools / log / input) |
| `/stats` | Cross-session cost dashboard (today / week / month / all-time) |
| `/doctor` | Health check (api / config / api-reach / index / hooks / project) |
| `/keys` | Keyboard + mouse + copy/paste reference |
| `/feedback` | Open a GitHub issue with diagnostic info copied to clipboard |

### Extend

| Command | What it does |
|---|---|
| `/mcp` | Open the MCP hub (live + marketplace tabs) |
| `/resource [uri]` | Browse / read MCP resources |
| `/prompt [name]` | Browse / fetch MCP prompts |
| `/memory [list\|show\|forget\|clear]` | Manage pinned memory (REASONIX.md + `~/.reasonix/memory`) |
| `/skill [list\|show\|new\|<name>]` | List / run / scaffold user skills |

### Session

| Command | What it does |
|---|---|
| `/sessions` | List saved sessions (current marked with ▸) |

### Code mode

| Command | What it does |
|---|---|
| `/init [force]` | Scan project, synthesize a baseline `REASONIX.md` |
| `/apply [N\|N,M\|N-M]` | Commit pending edit blocks to disk (subset selection supported) |
| `/discard [N\|N,M\|N-M]` | Drop pending edits without writing |
| `/walk` | Step through pending edits one block at a time (git-add-p style) |
| `/undo` | Roll back the last applied edit batch |
| `/history` | List every edit batch this session |
| `/show [id]` | Dump a stored edit diff |
| `/commit "msg"` | `git add -A && git commit -m ...` |
| `/mode <review\|auto\|yolo>` | Edit-gate mode. Shift+Tab cycles |
| `/plan [on\|off]` | Toggle read-only plan mode. Submitted plans initially show a compact summary; press `Ctrl+P` in the plan confirmation modal to expand/collapse full details |
| `/checkpoint [name\|list\|forget]` | Snapshot every file the session has touched |
| `/restore <name\|id>` | Roll back to a named checkpoint |
| `/cwd <path>` (`/sandbox`) | Switch the workspace root mid-session |

### Jobs (code mode)

| Command | What it does |
|---|---|
| `/jobs` | List background jobs |
| `/kill <id>` | Stop a background job (SIGTERM → SIGKILL) |
| `/logs <id> [lines]` | Tail a job's output (default 80 lines) |

### Advanced

| Command | What it does |
|---|---|
| `/pro [off]` | Arm v4-pro for the NEXT turn only |
| `/budget [usd\|off]` | Session USD cap |
| `/search-engine <mojeek\|searxng>` (`/se`) | Switch web search backend |
| `/hooks [reload]` | List / reload hooks |
| `/permissions [list\|add\|remove\|clear]` | Edit shell allowlist |
| `/dashboard [stop]` | Launch / stop the embedded web dashboard |
| `/loop <interval> <prompt>` | Auto-resubmit a prompt every interval |
| `/plans` | List active + archived plans |
| `/replay [N]` | Load an archived plan as a read-only Time Travel snapshot |
| `/update` | Show current vs latest version |
| `/exit` (`/quit`, `/q`) | Quit the TUI |

---

## Keyboard

| Key | What it does |
|---|---|
| `Enter` | Submit the prompt |
| `Shift+Enter` | Insert a newline in the prompt |
| `↑` / `↓` | Scroll chat history (mouse wheel routes here too) |
| `Ctrl+P` / `Ctrl+N` | Previous / next prompt history · cursor up / down in a multi-line draft. In a submitted-plan confirmation modal, `Ctrl+P` expands/collapses full plan details |
| `Ctrl+A` / `Ctrl+E` | Jump to start / end of the current line |
| `Ctrl+W` | Delete the word before the cursor |
| `Ctrl+U` | Clear the entire prompt buffer |
| `Tab` | Complete @-mention · drill folder · accept slash command |
| `Shift+Tab` | Edit-gate: toggle review ↔ AUTO mode |
| `Esc` | Dismiss picker · abort the running model turn |
| `Ctrl+C` | Abort the running model turn (NOT copy — see clipboard) |
| `PgUp` / `PgDn` | Scroll chat history a page at a time. While plan details are expanded, scroll the bounded detail window |
| `End` | Jump chat to the most recent line |

### Edit-gate (code mode)

| Key | What it does |
|---|---|
| `y` / `n` | Accept / drop pending edits in the review modal |
| `Shift+Tab` | Toggle review ↔ AUTO (persisted across sessions) |
| `u` | Undo the last auto-applied batch (within the 5s banner) |

---

## Mouse

| Action | What it does |
|---|---|
| Wheel | Scrolls chat history (works on web / cloud / SSH terminals too) |
| Drag | Selects text natively — no modifier needed |
| Right-click | Terminal-native (e.g. paste menu on Windows Terminal) |

Reasonix sets DECSET 1007 (alternate-scroll) only — wheel events translate to ↑/↓ keypresses for the app, but native click/drag selection is left untouched. Pass `--no-mouse` to opt out entirely.

---

## Copy / paste

The default path is **terminal-native**. Drag to select, then use your terminal's normal copy keys:

| Action | How |
|---|---|
| Select text | Drag — terminal-native (no modifier) |
| Copy | `Ctrl+Shift+C` (Win / Linux) · `Cmd+C` (macOS) — or auto-copy-on-select if your terminal does it |
| Paste | `Ctrl+V` or `Ctrl+Shift+V` (Win / Linux) · `Cmd+V` (macOS) |
| Multi-line paste | Bracketed paste — pastes stay one block, no auto-submit on intermediate newlines |

### When drag-select doesn't work

In SSH / mosh / tmux, the alt-screen buffer prevents the terminal from extending the selection past the visible viewport — there is no scrollback above the alt-screen to drag into. Two fixes:

1. **`/copy`** — open vim/tmux-style copy mode in-app. Snapshots the current chat to a navigable buffer; `y` yanks to clipboard via OSC 52 (with a temp-file fallback for terminals that don't support it).
2. **`--no-alt-screen`** — render to shell scrollback instead. Drag-select then works terminal-natively (the chat content is real lines in the scrollback above your cursor). Trade-off: redraw can ghost on resize.

### `/copy` — copy mode keys

| Key | What it does |
|---|---|
| `j` / `↓` | Cursor down one line |
| `k` / `↑` | Cursor up one line |
| `PgUp` / `PgDn` | Page up / down |
| `g` / `G` | Jump to top / bottom |
| `v` | Start (or cancel) selection at the cursor |
| `y` / `Enter` | Yank selection to clipboard, exit |
| `q` / `Esc` | Quit without yanking |

`y` with no active selection yanks just the current line. The yank goes through OSC 52 first (works through SSH, mosh, tmux with `set -g set-clipboard on`); content larger than 75 KB falls back to a temp file whose path is printed on exit.

---

## Where this lives

In-app, `/keys` and `/help` print the same content the model knows about. This page mirrors them so the reference is greppable from the repo / website.
