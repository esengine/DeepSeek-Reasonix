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
- [Environment variables](#environment-variables)
- [Serve web frontend](#serve-web-frontend)
- [Configuration paths](./CONFIG_PATHS.md)
- [Reasoning language](./REASONING_LANGUAGE.md)
- [Task contracts and pause policy](./TASK_CONTRACT.md)
- [Custom OpenAI-compatible providers](#custom-openai-compatible-providers)
- [Desktop hooks](#desktop-hooks)
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
Fields marked user/global only, including agent step limits, are not overridden
by `./reasonix.toml`.
Provider entries name secrets with `api_key_env`, while the secret values live in
Reasonix's global `<Reasonix home>/.env`, shared by CLI and desktop. Project
`.env`, home `.env`, inherited shell environment variables, legacy credentials,
and the OS keyring are not provider-key runtime fallbacks; legacy credentials are
only migration sources. Project `.env` still feeds workspace-scoped,
non-provider `${VAR}` expansion for MCP/plugin settings without importing
provider keys or Reasonix control variables. See
[Configuration paths](./CONFIG_PATHS.md) for the full `config.toml` and `.env`
structure.

For the desktop and CLI usage of visible reasoning language, see
[Reasoning language](./REASONING_LANGUAGE.md).

```toml
default_model = "deepseek-flash"   # executor; set [agent].planner_model to add a planner
# language    = "zh"               # ui language; empty = auto-detect from $LANG / $REASONIX_LANG

[ui]
# shortcut_layout = "desktop"      # classic|desktop; compatibility setting
# cursor_shape = "underline"       # block|underline|bar; CLI/TUI text cursor

[agent]
max_steps = 0                    # user/global only; executor tool-call rounds; 0 = no limit
planner_max_steps = 0            # user/global only; planner read-only tool-call rounds; 0 = no limit
reasoning_language = "auto"      # visible reasoning text: auto|zh|en
# plan_mode_allowed_tools = ["custom_reader"]   # extra read-only custom tools only;
#                                                # does not unlock blocked tools or unsafe bash
# plan_mode_read_only_commands = ["gh issue view", "gh pr diff"]   # extra read-only shell prefixes for planning
# planner_model = "deepseek-pro"      # optional low-frequency planner
# subagent_model = "deepseek-pro"     # optional default for runAs=subagent skills
# subagent_models = { review = "deepseek-pro", security_review = "deepseek-pro" }
# max_subagent_depth = 2              # nested delegation depth; set 1 for the old single-layer boundary
auto_plan = "off"                  # user-level only; off|on; off keeps plan mode manual
# auto_plan_classifier = "deepseek-flash"   # optional; only borderline tasks call it
tool_result_snip_ratio = 0.6       # shorten stale tool output before summary compaction

[[providers]]
name        = "deepseek-flash"
kind        = "openai"
base_url    = "https://api.deepseek.com"
model       = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
# also preset: deepseek-pro

[tools]
enabled = []   # omit/empty = all built-ins
bash_timeout_seconds = 120   # foreground safety cap; set 0 for no tool-local cap
mcp_call_timeout_seconds = 300   # default MCP call safety cap; per-plugin/tool overrides may raise it

[environment]
enabled = true   # inject a stable startup summary of OS, shell, and common tools
# [environment.tools]
# go = "/opt/homebrew/bin/go"   # optional explicit trusted path; workspace-local paths are not auto-executed

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
# forbid_read    = ["${HOME}/.ssh"]   # dirs the agent must not read or list

[serve]
auth_mode = "none"             # none|token|password; use auth before binding beyond localhost
# token = ""                   # optional fixed token; empty token mode generates one at startup
# password_hash = ""           # bcrypt hash generated with reasonix serve --hash-password --password '...'
# behind_proxy = false         # true only behind a trusted reverse proxy

[[plugins]]
name    = "example"
command = "reasonix-plugin-example"
call_timeout_seconds = 600   # optional per-server MCP call timeout
tool_timeout_seconds = { "generate_video" = 1800 }   # optional raw MCP tool names
```

For the full schema and every field's contract, see [`SPEC.md` §5](./SPEC.md#5-configuration-toml).

`[agent].plan_mode_allowed_tools` is an extra read-only declaration for custom or
external tools Reasonix cannot classify itself. For MCP/plugin tools, a concrete
model-visible name such as `mcp__github__issue_read` also promotes that tool to a
trusted read-only reader for planner and read-only research surfaces. Prefer the
one-time MCP read-only trust prompt, or plugin-level `trusted_read_only_tools`
when you want to pre-seed audited tools; keep `plan_mode_allowed_tools` as the
compatibility escape valve. It never unlocks known blocked plan-mode tools such
as `bash`, `task`, writers, installers, or memory mutation tools, and it never
bypasses bash's plan-mode safety checks.

Use `[agent].plan_mode_read_only_commands` when plan-mode research needs a
specific shell command that Reasonix cannot classify but you know is read-only,
such as `gh issue view` or an internal query CLI. Entries are concrete command
prefixes, not tool names: `["gh issue view"]` permits `gh issue view 4572`, while
`bash`, `sh`, and other shell interpreters are ignored. Shell operators,
redirection, command substitution, background execution, and unsafe built-in
command flags remain blocked while planning. In interactive plan mode, Reasonix
can also ask you to trust a concrete unknown query prefix the first time it is
needed; the persistent choice writes the same
`[agent].plan_mode_read_only_commands` entry. Auto/YOLO approval never answers
this trust prompt.

### Environment variables

Most day-to-day settings belong in `config.toml` or the global Reasonix `.env`
described above. The variables below are process-level advanced switches; set
them before launching Reasonix. Project `.env` files are not a runtime source for
Reasonix control variables.

`REASONIX_MEMORY_COMPILER_LLM_CLASSIFICATION=true` enables the optional LLM
task/chat classifier for Memory v5. By default it is disabled, and Reasonix uses
the local heuristic classifier without extra provider calls. When enabled, cache
misses may send a small classifier request through the configured provider before
deciding whether a user input is task-like or conversational; this can add a
little latency, provider usage, and token cost. The classifier result is cached
per session for a short time. Only the exact trimmed value `true` enables it;
unset, `false`, `1`, and `TRUE` keep the default heuristic path.

```bash
REASONIX_MEMORY_COMPILER_LLM_CLASSIFICATION=true reasonix
```

For development runs, prefix the command that starts the process, for example:

```bash
REASONIX_MEMORY_COMPILER_LLM_CLASSIFICATION=true wails dev -forcebuild
```

Packaged desktop apps launched from the OS app launcher may not inherit variables
from your interactive terminal; start the app from an environment-managed launcher
when you intentionally want this advanced switch enabled.

## Serve web frontend

`reasonix serve` starts the same local engine behind a browser UI. Use it when
you want a desktop-style surface without installing the desktop app, when running
Reasonix on a remote development box through a tunnel, or when you want a
shareable view of a live session.

```bash
cd your-project
reasonix serve
# open http://127.0.0.1:8787
```

By default it listens on `127.0.0.1:8787` with `auth_mode = "none"`. Keep that
default for local-only use. If you bind outside loopback, expose it through a
tunnel, or put it behind a reverse proxy, enable authentication before sharing
the URL:

```bash
reasonix serve --auth token
reasonix serve --addr 0.0.0.0:8787 --auth token
reasonix serve --auth password --password 'temporary-password'
```

Token mode prints a share URL with `?token=...`; pass `--token` or set
`[serve].token` to reuse a stable token. Password mode requires either
`--password` at startup or a stored bcrypt hash:

```bash
reasonix serve --hash-password --password 'strong-password'

# ~/.reasonix/config.toml
[serve]
auth_mode = "password" # none|token|password
password_hash = "$2a$12$..."
behind_proxy = true    # only behind a trusted reverse proxy
```

The web UI exposes chat, tool approvals, session history, rewind/fork/summarize,
model and reasoning-effort controls, Goal, a live todo panel fed by the
`todo_write` tool, and provider balance when configured. Use `--model`,
`--max-steps`, or `--resume` for one-off launches; otherwise `serve` uses the
user-global `default_model`.

## Custom OpenAI-compatible providers

In the desktop app, open **Settings -> Model -> Access -> Add model service ->
Custom provider** for proxies, aggregators, or self-hosted services that speak
the OpenAI-compatible chat API or Anthropic-compatible Messages API.

For common providers, choose **Add model service -> Recommended preset** instead.
Reasonix can prefill editable custom-provider entries for Kimi CN, Kimi Global,
Kimi Coding Plan, MiMo API, MiMo Anthropic, MiMo Token Plan CN/SGP/AMS and their
Anthropic-compatible variants, MiniMax CN/Global API, MiniMax CN/Global
Anthropic, GLM CN, Z.AI Global, GLM/Z.AI Coding Plan OpenAI-compatible and
Anthropic-compatible endpoints, OpenCode Go, OpenCode Go Anthropic, OpenCode Zen
Anthropic, Qwen/DashScope CN/Global, Qwen Coding Plan CN/Global
OpenAI-compatible and Anthropic-compatible endpoints, StepFun OpenAI-compatible
and Anthropic-compatible endpoints, NovitaAI, GMI Cloud, Vercel AI Gateway,
HuggingFace Router, NVIDIA NIM, KiloCode, and Ollama Cloud. Plan names describe
the access/payment route; they include CN/Global only when the provider exposes
distinct regional endpoints. Kimi Coding Plan is therefore a dedicated plan
endpoint, while Kimi direct API is split into CN and Global. The preset path
usually needs only the provider API key: the key value is stored in Reasonix home
`.env`, while `config.toml` stores the endpoint, model list, key
environment-variable name, context window, vision model metadata, proxy bypass
for China-only endpoints, MiniMax `reasoning_split`, GLM/MiniMax thinking
heuristics, Anthropic-compatible Bearer auth where needed, Ollama Cloud
max-effort support, and OpenCode Go per-model reasoning overrides. After adding
a preset, open its provider card if you need to change models, headers,
endpoint, or compatibility settings.

Fill **API address** with the provider endpoint that should receive the standard
chat path. In this mode Reasonix previews and sends chat requests to:

```text
<API address>/chat/completions
```

Enable **Full URL** when the service gives you a complete request URL, for
example `https://gateway.example.com/v1/chat/completions`. Reasonix then sends
chat requests directly to that URL and does not append `/chat/completions`. The
preview under the field shows the exact request URL that will be used.

Model discovery uses the API address to try likely model-list URLs such as
`/models` and `/v1/models`. If the gateway requires a separate model-list
endpoint, open **Compatibility settings** and set `models_url`, for example
`https://gateway.example.com/v1/models`. If discovery is not available, fill the
model list manually.

**Full URL** still uses the OpenAI-compatible chat request body. It does not
switch the request schema to the OpenAI Responses API.

### Compatibility settings

The **Compatibility settings (usually leave unchanged)** section is for gateways
whose authentication, model-list endpoint, or reasoning/thinking request shape
differs from the normal OpenAI-compatible defaults. Leave these fields at their
defaults unless the provider documentation or a proxy error tells you otherwise.
For Anthropic-compatible services, such as some coding-plan endpoints, choose
**Anthropic-compatible** as the connection protocol before saving.

| Field | What it controls | When to change it |
| --- | --- | --- |
| `api_key_env` | The environment-variable name used for this provider's API key. Desktop-saved key values are stored in Reasonix home `.env` under this name; the TOML config stores only the name. | Change it when several providers need distinct keys, or leave it blank for a service that does not require an API key. |
| `models_url` | The URL used only for model discovery. Chat requests still use the API address or Full URL above. | Set it when `/models` or `/v1/models` is not where the gateway exposes its model list. |
| Extra request headers | Static HTTP headers, one `Header: value` per line. | Use for gateways such as OpenRouter that require `HTTP-Referer`, `X-Title`, or similar site headers. Keep bearer/API keys in the key field instead of duplicating them here. |
| Extra request body | A JSON object merged into the top-level chat request body. | Use only for provider-specific flags such as `{"enable_thinking": true}`. Reasonix still owns core fields such as `model`, `messages`, `tools`, `stream`, and `thinking`, and null values are rejected. |
| Authorization: Bearer | For Anthropic-compatible providers, sends the saved API key as `Authorization: Bearer <key>` instead of `x-api-key`. | Enable it only when the gateway documents Bearer auth, such as MiniMax Global or Vercel AI Gateway. |
| Model capability mode | Which reasoning request protocol Reasonix should use for this provider. | Keep **Auto-detect** unless the gateway is misdetected or the model docs require a specific reasoning format. |
| Thinking override | Provider-specific override for `thinking.type`. | Keep **Auto** unless the backend documents `enabled`, `disabled`, or `adaptive`. Unsupported values can make some OpenAI-compatible gateways reject the request. |
| Balance URL | Optional endpoint for wallet/balance lookup. | Set it when the provider exposes a balance endpoint and you want the desktop status bar to show it. |
| Context window | The maximum number of tokens this provider keeps in context. `0` means provider default. | Set it when the model's real context size differs from Reasonix's default or built-in metadata. |

Model capability mode options:

| Option | Effect |
| --- | --- |
| Auto-detect (recommended) | Reasonix chooses the request shape from model capability metadata and endpoint detection. |
| DeepSeek thinking | Uses DeepSeek-style thinking control, including `thinking.type` and DeepSeek-supported reasoning depth. |
| OpenAI reasoning | Uses the standard OpenAI-compatible `reasoning_effort` levels. |
| Plain chat | Sends no reasoning or thinking control fields. Use this for text-only proxies that reject reasoning parameters. |

Thinking override options:

| Option | Effect |
| --- | --- |
| Auto (provider default) | Does not write an explicit provider-level `thinking` override. Reasonix uses the provider/model default behavior. |
| Enabled | Sends `thinking.type = "enabled"` for compatible providers. |
| Disabled | Sends `thinking.type = "disabled"` for compatible providers. On DeepSeek-style providers this also avoids sending a reasoning depth hint. |
| Adaptive (self-adjusting) | Sends or preserves `thinking.type = "adaptive"` only for providers that document adaptive thinking, such as MiniMax-M3-style endpoints. |

Some OpenAI-compatible gateways require non-standard top-level request body
fields. Add them with `extra_body` on the provider entry:

```toml
[[providers]]
name        = "spark"
kind        = "openai"
base_url    = "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2"
models      = ["xopglm52"]
api_key_env = "SPARK_API_KEY"
extra_body  = { enable_thinking = true }
```

`extra_body` is merged into the chat JSON request body. Reasonix keeps core
fields such as `model`, `messages`, `tools`, `stream`, and `thinking` under its
own control.

## Desktop hooks

Desktop hooks run local commands at lifecycle events such as `SessionStart`,
`UserPromptSubmit`, `PreToolUse`, and `PreCompact`. A successful `SessionStart`
hook may write plain text to stdout, or return JSON with
`hookSpecificOutput.additionalContext`; Reasonix injects that text once into the
next real user turn as `<hook-context event="SessionStart">...</hook-context>`.
This is intended for plugin or workflow bootstrap context, including
Superpowers-style startup instructions, without baking that workflow into
Reasonix's system prompt.

Plugin packages can provide this startup context through
`hooks/session-start-codex` or a plugin-root `CLAUDE.md`. Claude-style
`.claude/settings.json` command hooks are also mapped to matching Reasonix hook
events.

The injected hook context is dynamic current-turn context. It does not change
the stable system prompt, memory prefix, or tool schema, though dynamic content
can still reduce cache reuse for that turn. The detailed desktop hook schema and
trust model are documented in [the Chinese desktop hooks guide](./DESKTOP_HOOKS.zh-CN.md).

## Keyboard shortcuts

Shortcuts are documented by client because users usually look for the keys that
work in the surface they are using. The small mode rule is: `Shift+Tab` only
controls Plan, `Ctrl/Cmd+Y` only controls YOLO, and paste stays on the platform
paste key.

`[ui].shortcut_layout` is still accepted for old configs, but the shortcut
behavior below is unified across layouts.

For CLI/TUI text input, `[ui].cursor_shape` accepts `underline`, `block`, or
`bar`. The default is `underline` because terminal block cursors can visually
cover double-width CJK characters in some mixed-language input. Set it to
`block` to keep the old terminal-style cursor, or `bar` for a thin insertion
cursor. This setting does not change desktop or web text fields.

### Desktop GUI

Desktop shortcuts are managed from **Settings → Shortcuts**. Pick a row, press a
new key combination, and Reasonix saves it for the desktop app. Conflicting
bindings are rejected so one shortcut never triggers two actions. Press `?` or
use the help button in the topic bar to open the shortcuts sheet; it is generated
from the same shortcut registry, so it reflects any custom bindings.

Global shortcuts:

| Key or control | What it does | Notes |
| --- | --- | --- |
| `Cmd+K` on macOS, `Ctrl+K` on Windows/Linux | Toggles the command palette | The palette focuses search when it opens; `Esc` closes it. |
| `Cmd+,` on macOS, `Ctrl+,` on Windows/Linux | Opens Settings | Use **Shortcuts** in Settings to customize desktop bindings. |
| `Cmd+W` on macOS, `Ctrl+W` on Windows/Linux | Closes the active top tab | The last tab is kept by the normal close-tab guard. |
| `Cmd+B` / `Ctrl+B` | Shows or hides the left sidebar | Same action as clicking the sidebar toggle. |
| `Cmd+Shift+B` / `Ctrl+Shift+B` | Expands or collapses the most recent shell output | Same action as clicking the collapsed shell-output hint. |
| `Cmd+1`-`Cmd+9` on macOS, `Ctrl+1`-`Ctrl+9` elsewhere | Jumps to the matching visible chat in the sidebar | Hold `Cmd`/`Ctrl` briefly to reveal the numbered badges. Existing custom shortcuts that already use the same key take precedence. |
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
| `Ctrl+L` or `/cls` | Clears only the visible transcript | The LLM context, session file, tools, memory, and plugins stay loaded. Use `/clear` when you want to discard the conversation context. |
| `Esc` | Backs out of the current action | It un-sends a just-submitted turn before any reply, cancels a running turn, or clears non-empty input. |
| Double `Esc` on an empty idle composer | Opens the rewind picker | Same entry point as `/rewind`. |
| Transcript text selection | Copies transcript text | The full-screen TUI enables mouse reporting, so drag in the transcript to select text in-app; releasing the mouse copies it automatically, and `Ctrl+C`/`Super+C`/`Meta+C` or right-clicking the active selection copy it again. |
| `/mouse` | Toggles in-app mouse capture | Off hands the mouse back to your terminal, restoring its native click-drag selection and right-click context menu, at the cost of in-app drag-select, the transcript scrollbar, and wheel-scroll. Set `REASONIX_DISABLE_MOUSE=1` to start every session with it off. |
| `Ctrl+C` | Copies, cancels, clears, or quits | Copies an active transcript selection first. Otherwise it cancels a running turn, clears non-empty input, or quits on a second empty-composer press. |
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
| `Ctrl+B` | Expands or collapses long shell output | Long shell-output hint lines can also be clicked in the transcript; text selection is handled in-app while the full-screen TUI has mouse reporting enabled. |
| Ask / Auto | No keyboard cycle | Ask is the default interactive base. Auto is not entered through `Shift+Tab`; use clients or APIs that expose the tool approval posture directly. |
| `/goal <objective>`, `/goal --research <objective>`, `/goal --simple <objective>`, `/goal status`, `/goal clear` | Starts, checks, or clears Goal | Goal is not in any keyboard cycle; clearly long-horizon goals automatically enable AutoResearch. Ordinary prompts with strong AutoResearch signals are also upgraded into Goal. |
| `/migrate`, `/migrate --from <legacy-dir>` | Retries legacy migration or imports sessions from a chosen v0.x source | Use `--from` for custom Windows v0.52 install/data directories; it imports sessions only. See [Configuration paths](./CONFIG_PATHS.md). |

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
| YOLO | Skips ordinary tool approval prompts; `deny`, user `ask` questions, plan approval prompts, and MCP read-only trust prompts still wait. |
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
out. `forbid_read` optionally hides sensitive directories from the agent's
read/list/search tools; use absolute paths or `${HOME}` / `${VAR}` references,
not `~`, because config expansion is environment-variable based. `bash` is
itself jailed by default when an OS sandbox is available (`[sandbox] bash`,
Seatbelt on macOS, bubblewrap on Linux, and a native helper on Windows):
commands may write only those same roots plus platform-specific command
temp/cache roots, cannot read configured `forbid_read` roots while the OS
sandbox is active, and reach the network only when `[sandbox] network` is set.
The native Windows helper delegates the low-level isolation to
`github.com/SivanCola/windows-sandbox`, which uses AppContainer for read-only
commands and a low-integrity token for writable commands, temporarily grants
access to the workspace, a per-command temp root, and the target executable,
applies deny ACEs for `forbid_read` (files as well as directories), snapshots
touched DACLs before editing them, and restores those snapshots best-effort
after the command exits. Concurrent commands touching the same workspace are
serialized so their ACL edits cannot corrupt each other, and residue from a
force-killed command (a lingering low-integrity label or `forbid_read` deny) is
cleaned up by the next run. Because a writable command runs under a
low-integrity token, it can still write the few locations Windows leaves
writable to any low-integrity process (for example `%USERPROFILE%\AppData\LocalLow`)
in addition to the configured roots; the workspace boundary and `forbid_read`
denials still hold. Read-only AppContainer commands omit network capabilities
when networking is disabled; writable Windows commands fail closed when
`[sandbox] network = false`.
When no OS sandbox backend is available, `bash = "enforce"` refuses bash
execution instead of running unconfined. Install the platform sandbox backend
(bubblewrap/`bwrap` on Linux, `sandbox-exec` on macOS) or set
`[sandbox] bash = "off"` to explicitly restore the pre-1.16 unconfined shell
behavior (see
[`SPEC.md` §9](./SPEC.md#9-roadmap-not-in-current-scope) for the escape-prompt
and optional elevated Windows hardening still to come).

## Plugins (MCP)

Reasonix is an MCP client. A `[[plugins]]` entry's `type` selects the transport:
`stdio` (default) launches a local subprocess (`command`/`args`/`env`); `http`
(Streamable HTTP) connects to a remote `url` with optional static `headers`
(`${VAR}` / `${VAR:-default}` expanded from the environment, so tokens stay out
of the file). Tools surface to the model as `mcp__<server>__<tool>`; a tool
declaring MCP's `readOnlyHint: true` joins parallel dispatch and the permission
reader-default, but planner / read-only research confirms third-party read-only
hints before relying on them. In interactive sessions, approve the first trust
prompt once, or choose the persistent option to remember the raw MCP tool name.
This trust prompt is a user decision, so Auto/YOLO tool approval does not answer
it; allowing for the session or persisting trust prevents repeat prompts for the
same MCP tool.
Advanced users can also pre-seed audited third-party readers on the plugin:

```toml
[[plugins]]
name = "github"
command = "github-mcp"
trusted_read_only_tools = ["issue_read", "pull_request_read"]
```

The desktop MCP panel keeps this as an advanced management surface: expand a
configured server and open its tools list, then use **Pre-trust read-only** or a
per-tool **Pre-trust** button only when you want to approve tools before they are
needed. Use **Untrust** to remove a remembered reader. The desktop writes the raw
MCP tool names to `trusted_read_only_tools` in the owning config source: project
`.mcp.json` servers are updated under
`mcpServers.<server>.trusted_read_only_tools`, while ordinary Reasonix plugins
are updated in the user's Reasonix config. Trust only side-effect-free readers;
create/update/delete tools should remain untrusted.

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
# call_timeout_seconds = 600       # optional per-server MCP call timeout
# tool_timeout_seconds = { "generate_video" = 1800 }   # optional raw MCP tool names

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
`/memory`, `/memory-v5`, `/goal`, `/output-style`, `/sandbox`, `/language`,
`/auto-plan`, `/reasoning-language`, `/help`) run
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
Agent-initiated `remember` and `forget` calls always ask for fresh human approval
and show a compact preview of the saved or archived memory before they run.
Guardian review cannot answer for the user; non-interactive runs refuse these
tools instead of auto-approving them.
Retrieval keeps the top BM25 result while trimming weak common-word matches, and
0-result responses suggest narrower, more distinctive follow-up searches.
Memory v5 is enabled by default across the CLI/TUI, `reasonix serve`, and the
desktop app because they all share the same local controller. It records local,
project-scoped execution traces and compiler state under Reasonix home, then
compiles the next user turn into a compact execution contract only when prior
outcomes produce actionable constraints. Early turns may only write traces and
inject nothing. The default `verbosity = "observe"` keeps this as local learning
and content-free metrics only; it does not send `<memory-compiler-execution>` to
the provider-visible user turn. Opt into `verbosity = "compact"` (or the legacy
`on` command) when you explicitly want compact execution-contract injection,
including selected compact memory references in the provider-visible user turn.
Memory v5 never bypasses memory approvals and never mutates the cache-stable
system prompt, provider prefix, or tool schemas.

Toggle future turns with `/memory-v5 off|observe|compact|on|status` inside an
interactive session, or with `reasonix config memory-v5 off|observe|compact|on|status`
from a shell/script.
Desktop users can also use Settings → General → Memory v5. Settings → Updates →
Share aggregate quality metrics controls the optional aggregate upload. When
enabled, that upload may include only anonymous
count/size buckets such as injection on/off, compiled-token bucket, IR-overhead
bucket, memory-reference count, constraint/risk/step counts, and memory-graph
size buckets. It never includes memory text, prompts, tool outputs, file paths,
IDs, keys, base URLs, or file contents.

CLI/TUI and `reasonix serve` use the same user/global config. Project
`reasonix.toml` files cannot override this user/global setting. The CLI command
updates this underlying config; advanced users may also edit it manually under
Reasonix home:

```toml
[agent]
memory_compiler = { enabled = true, verbosity = "observe" }
```

The CLI can use Memory v5 for local turns, but it does not run the desktop
aggregate metrics upload pipeline. When `reasonix run --metrics <path>` is used,
the JSON also includes content-free `memory_compiler_*` summary fields and a
`memory_compiler_turn_details` array with per-turn injection state, compiled token
and IR-overhead estimates, referenced-memory/constraint/risk/step counts, and
current memory-graph counts.
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

## Goal and AutoResearch

Goal is the unified runtime for long-running objectives. Ordinary `/goal`
objectives stay lightweight: Reasonix keeps working until the goal is complete,
blocked, or cleared. When a goal is clearly long-horizon, Goal automatically
enables the AutoResearch strategy instead of requiring a separate
`/auto-research` skill; `auto-research` is not listed as a standalone built-in
skill in Settings -> Skills or the slash menu. If an ordinary chat prompt has a
very strong long-horizon signal, the host also upgrades it into the equivalent
of `/goal --research <original prompt>`.

For complex work, write the objective as a
[task contract](./TASK_CONTRACT.md): Context, Request, Output format,
Constraints, and Pause policy. Goal mode treats those sections as the boundary
for autonomous work. It keeps going with sensible defaults unless the next step
requires an irreversible or externally visible operation, a scope change, or
information only the user can provide.

AutoResearch is enabled for goals with strong signals such as "keep
researching", "long-running", "thoroughly", "debug until the root cause is
clear", "do not spin", "run experiments", "verify repeatedly", or "turn this
into a complete plan". It can also trigger when the objective combines multiple
phases such as research/diagnosis, implementation/fixing, verification/testing,
optimization/documentation/release, or when the user names an existing
`.reasonix/autoresearch/<task-id>/` directory. Advanced users can force it with
`/goal --research <objective>` or force lightweight Goal with
`/goal --simple <objective>`. Ordinary-chat auto-upgrade is more conservative
than `/goal`'s internal classification: standalone phrases such as "long term",
"optimize", "research this", or "verify this" do not create AutoResearch tasks
by themselves.

Once AutoResearch is active, the agent treats the goal as a stateful research
loop instead of a chat-only continuation. It creates or reuses a project-local
`.reasonix/autoresearch/<task-id>/` directory. For new tasks, the default id
shape is `YYYYMMDD-HHMMSS-slug`, such as `20260618-224530-cache-audit`; Reasonix
checks the project directory first and appends `-2`, `-3`, and so on only if
that id already exists. The task state includes `task_spec.md`, `progress.json`,
`findings.jsonl`, `directions_tried.json`, and `iteration_log.jsonl`, records
each iteration's direction, evidence, verification result, and blocker, and uses
`stale_count` to detect repeated weak progress. Repeated stalls force a
structural pivot, such as changing evidence source, entrypoint, test oracle,
decomposition, benchmark, or worker strategy, rather than retrying the same
tactic.

Workers and subagents may explore independently, but the orchestrator owns the
canonical state files. Completion requires a requirement-by-requirement evidence
audit against `task_spec.md`; a passing narrow check is not treated as proof of a
broad requirement. Dynamic run state stays in `.reasonix/autoresearch/...`, not
in `REASONIX.md`, `AGENTS.md`, project memory, tool schemas, or the cache-stable
system prompt. Public publishing, destructive operations, credentials, payments,
and external notifications still follow the normal approval, privacy, and cache
gates.

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
```

The planner sees loaded `REASONIX.md` / `AGENTS.md` memory and a small read-only
research tool set, so it can inspect relevant files before handing a plan to the
executor. Writer and workflow tools remain executor-only. `max_steps` limits the
executor; `planner_max_steps` limits only the planner, and either can be set to
`0` for no round limit.

Keep step-limit preferences in the user config. Project `./reasonix.toml` files
do not override `max_steps` or `planner_max_steps`.

Subagent skills inherit the executor model by default. Set `subagent_model` to
run them on another configured model, or use `subagent_models` to override only
specific skills such as `review` or `security_review`.

Subagents may delegate one more layer by default: the root session is depth 0,
first-layer subagents are depth 1, and the maximum `max_subagent_depth = 2`
means a depth-1 workflow can dispatch a depth-2 reviewer or implementer. Depth-2
subagents do not receive recursive agent/skill tools. Set
`agent.max_subagent_depth = 1` to restore the old single-layer boundary. This is
intended for workflows such as Superpowers where a workflow skill may dispatch a
reviewer subagent, while still avoiding unbounded recursion and background
fanout.

Use `read_only_task` when planning needs isolated, deeper research without
granting write-capable delegation. Use `read_only_skill` when the same need is
best expressed through an existing skill. Both run ephemeral read-only
subagents with only read-only research tools plus safe foreground bash, return
only the final answer, and do not create resumable subagent transcripts.
Read-only nested delegation may be available until `max_subagent_depth` is
reached, but writer-capable `task` / `run_skill` remain unavailable. In
token economy mode, connect only this narrow surface with
`connect_tool_source(source="read_only_skill")`; the full `skills` source still
enables writer-capable skill tools and remains blocked in plan mode.

For interactive frontends, plan mode is manual by default. Set
`agent.auto_plan = "on"` to make complex-looking tasks enter plan mode
automatically: Reasonix first drafts a read-only plan, then waits for approval
before editing or running side-effecting commands. `auto_plan_classifier` can
name a cheap provider such as `deepseek-flash`; it is only called for borderline
inputs and falls back to the heuristic if classification fails. Use
`/auto-plan off|on` inside `reasonix` to change the user-level setting, or
`reasonix config auto-plan off|on` from a shell/script. Auto-plan is user-level
only; `agent.auto_plan` in a project `reasonix.toml` is ignored. The visible
reasoning language uses a similar shape: `/reasoning-language auto|zh|en` in the
session, or `reasonix config reasoning-language auto|zh|en` in a shell/script.
Memory v5 uses `/memory-v5 off|observe|compact|on|status` or
`reasonix config memory-v5 off|observe|compact|on|status` and is user-level only. Pass `--local`
to the reasoning-language shell command only when you intentionally want a
project-local override.

The why behind separate sessions (keeping each model's prefix cache-stable) is in
[`SPEC.md` §3.5](./SPEC.md#35-two-model-collaboration-coordinator).
