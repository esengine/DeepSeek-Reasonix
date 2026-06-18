# Reasonix Guide

<a href="../README.md">README</a>
&nbsp;·&nbsp;
<a href="./GUIDE.zh-CN.md">简体中文</a>
&nbsp;·&nbsp;
<a href="./SPEC.md">Spec</a>

> Day-to-day configuration and usage. For the engineering contract and internals
> (data types, registries, package layout, roadmap), see the **[Spec](./SPEC.md)**.

## Contents

- [Configuration](#configuration)
- [Configuration paths](./CONFIG_PATHS.md)
- [Reasoning language](./REASONING_LANGUAGE.md)
- [Keyboard shortcuts](#keyboard-shortcuts)
- [Permissions & sandbox](#permissions--sandbox)
- [Plugins (MCP)](#plugins-mcp)
- [Slash commands](#slash-commands)
- [@ references](#-references)
- [Two-model collaboration](#two-model-collaboration)

## Configuration

Resolution order: **flag > `./reasonix.toml` > the user config file >
built-in defaults**. Starting with **Reasonix v1.8.1**, the user config lives at
`~/.reasonix/config.toml` on macOS/Linux and
`%AppData%\reasonix\config.toml` on Windows; see
[Configuration paths](./CONFIG_PATHS.md) for migration and related data paths.
Secrets come from the environment via `api_key_env` and are never stored in
config files. Credentials default to `credentials_store = "auto"`, which prefers
the OS credential store and falls back to the file under Reasonix home. New keys
saved by Reasonix are not written to a project `.env`; project `.env` files are
only read for compatibility and explicit per-project overrides.

For the desktop and CLI usage of visible reasoning language, see
[Reasoning language](./REASONING_LANGUAGE.md).

```toml
default_model = "deepseek-flash"   # executor; set [agent].planner_model to add a planner
# language    = "zh"               # ui language; empty = auto-detect from $LANG / $REASONIX_LANG

[ui]
# shortcut_layout = "desktop"      # classic|desktop; compatibility setting

[agent]
max_steps = 0                    # executor tool-call rounds; 0 = no limit
planner_max_steps = 12           # planner read-only tool-call rounds; 0 = no limit
reasoning_language = "auto"      # visible reasoning text: auto|zh|en
# planner_model = "mimo-pro"          # optional low-frequency planner
# subagent_model = "deepseek-pro"     # optional default for runAs=subagent skills
# subagent_models = { review = "deepseek-pro", security_review = "deepseek-pro" }
auto_plan = "off"                  # off|on; off keeps plan mode manual
# auto_plan_classifier = "deepseek-flash"   # optional; only borderline tasks call it

[[providers]]
name        = "deepseek-flash"
kind        = "openai"
base_url    = "https://api.deepseek.com"
model       = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
# also preset: deepseek-pro, mimo-pro (mimo-v2.5-pro), mimo-flash (mimo-v2.5) @ token-plan-cn.xiaomimimo.com/v1

[tools]
enabled = []   # omit/empty = all built-ins
bash_timeout_seconds = 120   # foreground safety cap; set 0 for no tool-local cap

[skills]
# paths = ["~/my-skills", "../shared/skills"]   # extra custom skill roots
# excluded_paths = ["~/.agents/skills"]         # hide convention roots without deleting folders
# disabled_skills = ["review"]                  # hide skills until /skill enable <name>

[permissions]
mode  = "ask"                                # writer fallback when no rule matches: ask|allow|deny
deny  = ["Bash(rm -rf*)", "Bash(git push*)"] # hard-blocked in every mode
allow = ["Bash(go test:*)"]                  # never prompted

[sandbox]
# workspace_root = ""          # file-writers confined here; empty = current dir
# allow_write    = ["/tmp"]    # extra dirs write_file/edit_file/multi_edit/move_file may touch

[[plugins]]
name    = "example"
command = "reasonix-plugin-example"
```

For the full schema and every field's contract, see [`SPEC.md` §5](./SPEC.md#5-configuration-toml).

## Keyboard shortcuts

Shortcuts are documented by client because users usually look for the keys that
work in the surface they are using. The small mode rule is: `Shift+Tab` only
controls Plan, `Ctrl/Cmd+Y` only controls YOLO, and paste stays on the platform
paste key.

`[ui].shortcut_layout` is still accepted for old configs, but the shortcut
behavior below is unified across layouts.

### Desktop GUI

Desktop shortcuts are managed from **Settings → Shortcuts**. Pick a row, press a
new key combination, and Reasonix saves it for the desktop app. Conflicting
bindings are rejected so one shortcut never triggers two actions. Press `?` or
use the help button in the topic bar to open the shortcuts sheet; it is generated
from the same shortcut registry, so it reflects any custom bindings.

Global shortcuts:

| Key or control | What it does | Notes |
| --- | --- | --- |
| `Cmd+K` on macOS, `Ctrl+K` on Windows/Linux | Opens the command palette | `Esc` closes the palette. |
| `Cmd+,` on macOS, `Ctrl+,` on Windows/Linux | Opens Settings | Use **Shortcuts** in Settings to customize desktop bindings. |
| `Cmd+W` on macOS, `Ctrl+W` on Windows/Linux | Closes the active top tab | The last tab is kept by the normal close-tab guard. |
| `Cmd+B` / `Ctrl+B` | Expands or collapses the most recent shell output | Same action as clicking the collapsed shell-output hint. |
| `Cmd++`, `Cmd+-`, `Cmd+0` on macOS; `Ctrl++`, `Ctrl+-`, `Ctrl+0` elsewhere | Increases, decreases, or resets text size | `=` is accepted for the plus key on keyboards that report it that way. |
| `?` | Opens the keyboard shortcuts sheet | The sheet shows the current effective desktop bindings. |

Composer shortcuts:

| Key or control | What it does | Notes |
| --- | --- | --- |
| `Enter` | Sends the current message | IME composition confirmation is left alone. |
| `Shift+Enter` | Inserts a newline | The composer keeps focus. |
| `Shift+Tab` | Toggles Plan on/off | Plan is read-only planning and does not cycle Ask/Auto/YOLO. |
| `Cmd+Y` / `Ctrl+Y` | Toggles YOLO on/off | Turning YOLO off restores the previous Ask/Auto base when known. |
| `Cmd+V` on macOS, `Ctrl+V` on Windows/Linux | Pastes clipboard content | Clipboard images are attached; images can also be dropped into the composer. |
| Plain `Up` / `Down` at the prompt boundary | Recalls older or newer submitted prompts | Modified arrows and native text navigation stay with the textarea. |
| `Esc` while a turn is running | Cancels the running turn | If the turn has not produced a response yet, the draft is restored. |

Menus and controls:

| Key or control | What it does | Notes |
| --- | --- | --- |
| `Up` / `Down` in slash, `@`, or past-chat menus | Moves the highlighted item | Past-chat search uses the same navigation keys. |
| `Enter` / `Tab` in those menus | Accepts the highlighted item | Directory-like entries can keep the menu open for the next level. |
| `Esc` in those menus | Closes the current menu or returns from past-chat search | Regular typing continues after the menu closes. |
| Ask / Auto / YOLO approval controls | Picks the tool approval posture directly | Clicking these controls is unchanged by keyboard shortcuts. |
| Tool approval card | `Left` / `Right`, `Enter`, `1`-`4`, `Esc` | Move the highlighted action, confirm it, pick a numbered action, or deny. The default highlighted action is Allow once. |
| Plan approval card | `Left` / `Right`, `Enter`, `1`-`3`, `Esc` | Move between Revise plan, Start execution, and Exit plan. The default highlighted action is Start execution. |
| Plan control | Toggles Plan on/off | Same mode as `Shift+Tab`. |
| Goal item in the collaboration menu | Starts, views, or clears Goal | Goal is not in any keyboard cycle. |

### CLI / TUI

Chat and transcript shortcuts:

| Key or command | What it does | Notes |
| --- | --- | --- |
| `Enter` | Sends the current message | While a turn is running, non-empty input is queued as follow-up feedback. |
| `Shift+Enter`, `Alt+Enter`, or `Ctrl+J` | Inserts a newline | Plain `Enter` is reserved for send/confirm. |
| Plain `Up` / `Down` while idle | Recalls older or newer submitted prompts | In a running turn, the same keys navigate queued follow-up feedback. |
| `PageUp` / `PageDown` | Scrolls the transcript | Works regardless of the current chat state. |
| `Ctrl+Home` / `Ctrl+End` | Jumps to the top or bottom of the transcript | Useful after long tool output. |
| `Esc` | Backs out of the current action | It un-sends a just-submitted turn before any reply, cancels a running turn, or clears non-empty input. |
| Double `Esc` on an empty idle composer | Opens the rewind picker | Same entry point as `/rewind`. |
| `Ctrl+C` / `Meta+C` / `Super+C` | Copies an active transcript selection | Without a selection it cancels a running turn, clears non-empty input, or quits on a second empty-composer press. |
| `Ctrl+D` | Quits the TUI | Immediate quit. |
| `Ctrl+V`, `Ctrl+Shift+V`, `Meta+V`, or `Super+V` | Pastes clipboard content | The CLI tries an image first, then falls back to text or file references. |
| `/paste-image` | Pastes a clipboard image | Use it when you want image-only paste or the terminal handles text paste itself. |
| A line starting with `!` | Runs a shell command directly | The command runs locally without asking the model. |

Mode and display shortcuts:

| Key or command | What it does | Notes |
| --- | --- | --- |
| `Shift+Tab` | Toggles Plan on/off | Plan is read-only planning and does not cycle Ask/Auto/YOLO. |
| `Ctrl+Y` | Toggles YOLO on/off | Turning YOLO off restores the previous Ask/Auto base when known. Terminals that forward Command/Super may also send `Cmd+Y`, but `Ctrl+Y` is the reliable terminal shortcut. |
| `--yolo`, `--dangerously-skip-permissions` | Starts chat in YOLO | Same runtime mode as `Ctrl+Y`. |
| `Ctrl+O` | Toggles verbose reasoning display | Also available through `/verbose`. |
| `Ctrl+B` | Expands or collapses long shell output | Same action as clicking the collapsed shell-output hint. |
| Ask / Auto | No keyboard cycle | Ask is the default interactive base. Auto is not entered through `Shift+Tab`; use clients or APIs that expose the tool approval posture directly. |
| `/goal <objective>`, `/goal status`, `/goal clear` | Starts, checks, or clears Goal | Goal is not in any keyboard cycle. |

Picker and approval shortcuts:

| Context | Keys | What they do |
| --- | --- | --- |
| Slash or `@` completion | `Up` / `Down`, `Tab` / `Enter`, `Esc` | Move, accept, or close the completion menu. |
| Tool approval prompt | `y`/`1`, `a`/`2`, `p`/`3`, `n`/`4`, `Enter`, `Esc`, `Ctrl+C` | Allow once, allow for session, persist allow, deny, accept default allow once, deny, or cancel the turn. |
| Ask question card | `Up`/`Down` or `j`/`k`, `Left`/`Right` or `h`/`l`, `Space`, `Enter`, `1`-`9`, `Esc`, `Ctrl+C` | Navigate answers/tabs, toggle multi-select answers, submit/activate, pick numbered options, dismiss, or cancel the turn. |
| Rewind picker | `Up`/`Down` or `j`/`k`, `Enter`, `b`, `c`, `d`, `f`, `s`, `u`, `Esc` | Choose a turn, apply both/conversation/code/fork/summarize actions, or go back/close. |
| Resume picker | `Up`/`Down` or `j`/`k`, `Enter`, `Esc` | Choose a saved session or close the picker. |
| MCP import picker | `Up`/`Down` or `j`/`k`, `Space`, `Enter`, `Esc` / `Ctrl+C` | Move, select servers, import selected servers, or cancel. |
| MCP manager | `Up`/`Down` or `j`/`k`, `Enter`, `Left`/`Right` or `h`/`l`, `r`, number keys, `q` / `Ctrl+C` | Navigate server lists/details, refresh, choose actions, or close. |
| `/clear` confirmation | Arrow keys or `j`/`k` / `Tab`, `Enter`, `y`, `n`, `Esc` / `Ctrl+C` | Toggle Clear/Cancel, confirm clear, or cancel. |

Mode meanings:

| Mode | Meaning |
| --- | --- |
| Ask | Prompts for fallback writer approvals. |
| Auto | Auto-allows fallback approvals; explicit `ask` / `deny` rules still apply. |
| YOLO | Skips ordinary tool approval prompts; `deny`, user `ask` questions, and plan approval prompts still wait. |
| Plan | Keeps the next work read-only until a plan is approved or Plan is turned off. |
| Goal | Pursues a saved objective until complete, blocked, or cleared. |

## Permissions & sandbox

Permissions gate each tool call: `deny` > `ask` > `allow` > fallback. Bash and
file mutation tools require approval by default; read-only tools generally do
not. Approvals are stored and matched as permission rules, not button labels:
for example `Bash(npm run build)`, `Bash(npm run test:*)`, and `Edit(docs/**)`.
`reasonix` can grant Bash as an exact command or as a conservative command
prefix (for example `Bash(go test:*)`), while file-editing tools share session
edit grants and persist path-scoped rules such as `Edit(src/app.go)`.
`reasonix run` stays autonomous but still honours `deny`.

Permissions are *policy* (which calls to allow / prompt). The **sandbox** is
*enforcement*: the file-writers (`write_file` / `edit_file` / `multi_edit` / `move_file`)
refuse any path outside `[sandbox] workspace_root` (default: the current dir, so
edits stay in the project), resolving symlinks and `..` so a link can't tunnel
out. Reads are unrestricted. `bash` is itself jailed on macOS by default
(`[sandbox] bash`, Seatbelt): commands may write only those same roots (plus
temp and toolchain caches) and reach the network only when `[sandbox] network`
is set. Other platforms fall back to running unconfined for now (see
[`SPEC.md` §9](./SPEC.md#9-roadmap-not-in-current-scope) for the escape-prompt and
Linux support still to come).

## Plugins (MCP)

Reasonix is an MCP client. A `[[plugins]]` entry's `type` selects the transport:
`stdio` (default) launches a local subprocess (`command`/`args`/`env`); `http`
(Streamable HTTP) connects to a remote `url` with optional static `headers`
(`${VAR}` / `${VAR:-default}` expanded from the environment, so tokens stay out
of the file). Tools surface to the model as `mcp__<server>__<tool>`; a tool
declaring MCP's `readOnlyHint: true` joins parallel dispatch and the permission
reader-default.

A server's **prompts** surface as `/mcp__<server>__<prompt>` slash commands
(positional args after the command); its **resources** are pulled in by writing
`@<server>:<uri>` in a message; `/mcp` lists connected servers and what each
exposes. `make build` also produces `bin/reasonix-plugin-example` — a runnable
reference stdio server (`echo`, `wordcount`, a `review` prompt, a style-guide
resource) you can copy.

```toml
[[plugins]]                       # local stdio server
name    = "example"
command = "reasonix-plugin-example"

[[plugins]]                       # remote server over Streamable HTTP
name    = "stripe"
type    = "http"
url     = "https://mcp.stripe.com"
headers = { Authorization = "Bearer ${STRIPE_KEY}" }
```

Enabled MCP servers start connecting automatically in the background after a
session begins, so chat stays usable while tools come online. Use `/mcp` or the
desktop MCP panel to refresh status, reconnect a server, inspect failures, or
disable a server for the current session.

**Already have an `.mcp.json`?** Drop it in the project root and Reasonix
reads it as-is — the `mcpServers` spec (`command`/`args`/`env`, `type`/`url`/
`headers`, `${VAR}` expansion) maps field-for-field onto `[[plugins]]`. Both
sources are merged; on a name collision `reasonix.toml` wins.

```json
{
  "mcpServers": {
    "filesystem": { "command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path"] },
    "stripe": { "type": "http", "url": "https://mcp.stripe.com", "headers": { "Authorization": "Bearer ${STRIPE_KEY}" } }
  }
}
```

**Upgrading from `0.x`?** Your old `~/.reasonix/config.json` is still read for its
`mcpServers` (honouring `mcpDisabled`) as a lowest-priority source, so MCP servers
keep working — move them into `reasonix.toml`'s `[[plugins]]` or a `.mcp.json` when
convenient.

## Slash commands

In an interactive `reasonix` session, built-in commands (`/compact`, `/new`, `/clear`, `/rewind`,
`/tree`, `/branch`, `/switch`, `/todo`, `/model`, `/mcp`, `/skills`, `/hooks`,
`/memory`, `/output-style`, `/sandbox`, `/language`, `/auto-plan`,
`/reasoning-language`, `/help`) run
locally — `/help` lists them all. `/new` starts a new session while saving the
previous transcript for history/resume; `/clear` asks for confirmation, then
discards the current context without saving it. `/tree` shows saved conversation
branches, `/branch [name]` forks the current conversation tip, `/branch <turn>
[name]` forks from an earlier checkpointed turn, and `/switch <id|name>` loads
another branch. **Custom commands** are Markdown files under `.reasonix/commands/`
(project) or `~/.reasonix/commands/` (user) — `review.md` becomes
`/review`, a subdirectory namespaces it (`git/commit.md` → `/git:commit`). The
body is a prompt template; invoking the command sends it as a turn.

`/memory` lists both memory documents (`REASONIX.md` / `AGENTS.md`) and saved
auto-memory facts. During agent turns, the read-only `history` and `memory`
tools let the model retrieve prior session decisions, compacted-history
archives, and saved facts on demand instead of injecting that dynamic state into
the stable system prompt. `/forget <name>` archives a saved fact rather than
deleting it permanently; the CLI/TUI and desktop memory panel can show those
archived files for traceability, but they are not searched as active memory.
Agent-initiated `remember` and `forget` calls always ask for fresh approval and
show a compact preview of the saved or archived memory before they run.
Retrieval keeps the top BM25 result while trimming weak common-word matches, and
0-result responses suggest narrower, more distinctive follow-up searches.
For implementation details, see
[`SESSION_MEMORY_RETRIEVAL.md`](SESSION_MEMORY_RETRIEVAL.md).

```markdown
---
description: Review the staged diff
argument-hint: [focus-area]
---
Review the staged diff. Focus on $ARGUMENTS, list bugs with file:line.
```

`$ARGUMENTS` expands to all space-separated args, `$1`…`$N` to positional ones.
MCP prompts also appear here as `/mcp__<server>__<prompt>`.

## @ references

Embed `@` references in a message and Reasonix resolves them before sending, as
tagged context blocks: `@path/to/file` (or `@dir`) injects a local file's
contents (or a directory listing), and `@<server>:<uri>` injects an MCP
resource. A local path is only treated as a reference when it actually exists,
so ordinary `@mentions` stay literal. Typing `/` or `@` opens an autocomplete
menu — slash commands, or hierarchical file navigation (one directory level at a
time, descend into folders) plus MCP resources.

## Two-model collaboration

`reasonix setup` keeps first-run minimal: pick provider → keys (every SKU of a
chosen provider is enabled). Running two models together (executor + planner,
separate cache-stable sessions) is a one-line edit afterwards — set
`planner_model` to any other enabled provider:

```toml
[agent]
planner_model = "deepseek-pro"   # used as the low-frequency planner
planner_max_steps = 12           # read-only tool-call rounds before pausing
```

The planner sees loaded `REASONIX.md` / `AGENTS.md` memory and a small read-only
research tool set, so it can inspect relevant files before handing a plan to the
executor. Writer and workflow tools remain executor-only. `max_steps` limits the
executor; `planner_max_steps` limits only the planner, and either can be set to
`0` for no round limit.

Keep personal step-limit preferences in the user config. Add them to a project's
`./reasonix.toml` only when that repository needs a shared override, such as a
larger planner limit for a very large codebase.

Subagent skills inherit the executor model by default. Set `subagent_model` to
run them on another configured model, or use `subagent_models` to override only
specific skills such as `review` or `security_review`.

For interactive frontends, plan mode is manual by default. Set
`agent.auto_plan = "on"` to make complex-looking tasks enter plan mode
automatically: Reasonix first drafts a read-only plan, then waits for approval
before editing or running side-effecting commands. `auto_plan_classifier` can
name a cheap provider such as `deepseek-flash`; it is only called for borderline
inputs and falls back to the heuristic if classification fails. Use
`/auto-plan off|on` inside `reasonix` to change the user-level setting, or
`reasonix config auto-plan off|on` from a shell/script. The visible reasoning
language uses the same shape: `/reasoning-language auto|zh|en` in the session, or
`reasonix config reasoning-language auto|zh|en` in a shell/script. Pass
`--local` to the shell command only when you intentionally want a project-local
override.

The why behind separate sessions (keeping each model's prefix cache-stable) is in
[`SPEC.md` §3.5](./SPEC.md#35-two-model-collaboration-coordinator).
