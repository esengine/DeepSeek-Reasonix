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
- [CLI reference](./CLI.md)
- [Environment variables](#environment-variables)
- [Serve web frontend](#serve-web-frontend)
- [Configuration paths](./CONFIG_PATHS.md)
- [Reasoning language](./REASONING_LANGUAGE.md)
- [Task contracts and pause policy](./TASK_CONTRACT.md)
- [Custom OpenAI-compatible providers](#custom-openai-compatible-providers)
- [Desktop hooks](#desktop-hooks)
- [Keyboard shortcuts](#keyboard-shortcuts)
- [Permissions & sandbox](#permissions--sandbox)
- [Capability diagnostics](#capability-diagnostics)
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
Fields marked user/global only are not overridden by `./reasonix.toml`.
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
# cursor_shape = "bar"             # block|underline|bar; CLI/TUI text cursor

[agent]
reasoning_language = "auto"      # visible reasoning text: auto|zh|en
# plan_mode_allowed_tools = ["mcp__legacy__reader"]   # legacy MCP read-only alias; does not change Plan availability
# plan_mode_read_only_commands = ["gh issue view"]   # legacy compatibility only; Plan bash now uses Permissions
# planner_model = "deepseek-pro"      # optional low-frequency planner
# subagent_model = "deepseek-pro"     # optional default for runAs=subagent skills
# subagent_models = { review = "deepseek-pro", security_review = "deepseek-pro" }
# max_subagent_depth = 2              # nested delegation depth; set 1 for the old single-layer boundary
# max_subagent_concurrency = 6        # session-wide sub-agent concurrency (task/fleet/skills)
# max_parallel_writers = 3            # concurrent writers with non-overlapping write_paths
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
# forbid_read    = ["${HOME}/.ssh"]   # paths the agent must not read or list

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

The legacy `[agent].plan_mode_allowed_tools` field is still decoded and rendered
for old configs. Concrete `mcp__<server>__<tool>` entries continue to act as a
local read-only trust alias, but prefer each server's `trusted_read_only_tools`
with raw MCP names. The field never grants or revokes calls in the main Plan
workflow.

`[agent].plan_mode_read_only_commands` is also retained for config round trips,
but the main Plan workflow no longer has a separate bash allowlist or trust
prompt. Bash classification and approval use the same Permissions rules in Plan
and Standard mode; the Sandbox remains the filesystem, process, and network
boundary. Dedicated planner and read-only subagent runners keep their own strict
read-only tool registry and foreground-command classifier.

### Environment variables

Most day-to-day settings belong in `config.toml` or the global Reasonix `.env`
described above. The variables below are process-level advanced switches; set
them before launching Reasonix. Project `.env` files are not a runtime source for
Reasonix control variables.

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

# <Reasonix home>/config.toml
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

## Editor integrations over ACP

`reasonix acp` exposes Reasonix as an ACP v1 stdio agent for editors and other
host clients. The dedicated **[ACP editor integration](./ACP.md)** guide covers
startup, capability negotiation, session lifecycle, independent model/work/
collaboration/approval controls, client filesystem and terminal capabilities,
MCP servers, permission requests, and the Reasonix mid-turn steering extension.

## Remote SSH

The remote module runs Reasonix on a remote host and reaches it over your own
SSH connection — VS Code Remote-SSH style. It bootstraps a persistent headless
`reasonix serve` on the remote host, forwards a local loopback port to it, and
opens the existing serve web client through that tunnel. The agent, its tools,
and its files all live on the remote host at full fidelity; nothing runs through
a lossy file proxy. V1 supports Linux and macOS remote hosts.

Hosts live in a user-global `[remote]` section of `config.toml`. Like
`[secrets]`, a project `reasonix.toml` cannot inject or override remote hosts —
a cloned repo can never steer where Reasonix opens SSH connections. Credentials
follow the provider idiom: the host names an env var (`passphrase_env`,
`password_env`) whose value lives in Reasonix's global `.env`; key material
itself is never stored — `identity_file` is a path.

```toml
[remote]
[[remote.hosts]]
name          = "gpu-box"
host          = "203.0.113.7"
user          = "dev"
identity_file = "~/.ssh/id_ed25519"
workspace     = "~/projects/app"
serve_install = "auto"            # auto | npm | upload | never

[[remote.hosts.forwards]]
type   = "local"                  # local (-L) | remote (-R)
bind   = "127.0.0.1:5432"
target = "127.0.0.1:5432"
```

CLI:

```bash
reasonix remote add gpu-box dev@203.0.113.7 --workspace '~/projects/app'
reasonix remote import --all              # import aliases; ssh -G resolves Include/Match rules when connecting
reasonix remote test gpu-box              # dial + auth + host-key confirmation
reasonix remote connect gpu-box --open    # bootstrap serve, tunnel, open the URL
reasonix remote serve status gpu-box
reasonix remote fs ls gpu-box:'~/projects/app'
```

Hosts with `use_ssh_config` enabled resolve the final effective configuration
through the local OpenSSH `ssh -G`, including `Include`, wildcard `Host`,
`Match` (including `Match exec`), repeated `IdentityFile`, `ProxyJump`, and
`IdentitiesOnly`. Import stores the original alias instead of a stale snapshot.

`connect` is a foreground supervisor (like `ssh -N` plus the serve bootstrap):
it keeps the tunnel and configured forwards alive, auto-reconnects with
exponential backoff if the link drops, and re-attaches forwards on reconnect.
Ctrl-C disconnects the local side only — the remote serve keeps running, so the
next `connect` reuses it. There is no background daemon in V1.

Host keys are verified against your OpenSSH `~/.ssh/known_hosts` (read-only)
plus a Reasonix-managed `~/.reasonix/remote/known_hosts`. A first-seen key
prompts for trust-on-first-use and is recorded in the managed file; a key that
contradicts a recorded one is a hard error that names the offending line and is
never auto-accepted.

Remote-side state lives under the remote host's `~/.reasonix/remote/`:
`serve-<workspace-slug>.json` (pid, bound loopback address, workspace),
`serve-<slug>.token` (0600; the auth token, passed to serve via `--token-file`
so it never appears in `ps`), and `serve-<slug>.log`.

In the desktop app, manage hosts under **Settings -> Remote SSH**, then use the
status-bar chip or the host row's **Remote explorer** button to browse and edit
files over SFTP, manage port forwards, and start/open the remote workspace.
Opening a workspace creates a separate native Reasonix window, similar to a
VS Code Remote SSH window. The primary window owns the SSH tunnel; the remote
window is an isolated, lightweight shell and does not restore or acquire local
conversation sessions.

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
| Context window | The provider-wide token budget Reasonix uses for automatic context cleanup. `0` disables automatic compaction. | Set it to the provider's model context limit; use a per-model override below when selected models differ. |

Each selected model also has an optional **Context window** input. Leave it blank
to inherit the provider-wide value, or enter a positive token count to override
that value for this model. This avoids premature compaction for long-context
models and provider errors for shorter-context models sharing the same endpoint.
Use the context-window limit from the model documentation, not the maximum output
tokens. For example, 128K commonly means `128000`; if the provider documents
`131072`, use that exact value. Values below 16384 show a non-blocking warning
because they can trigger frequent compaction and reduce cache hit rates.

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
work in the surface they are using. Desktop keeps its Plan toggle, while the CLI
cycles Ask, Auto, and Plan with `Shift+Tab`. `Ctrl/Cmd+Y` controls YOLO, and
desktop paste stays on the platform paste key. In the CLI, terminal-native text
paste and application-owned image paste use separate shortcuts.

`[ui].shortcut_layout` is still accepted for old configs, but the shortcut
behavior below is unified across layouts.

For CLI/TUI text input, `[ui].cursor_shape` accepts `underline`, `block`, or
`bar`. The default is `bar`: it remains easy to locate without covering
double-width CJK characters in mixed-language input. Set it to `block` for a
traditional terminal cursor or `underline` for a lower-profile cursor. This
setting does not change desktop or web text fields.

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
| `Shift+Tab` | Toggles Plan on/off | Plan changes the workflow instruction; built-in writers keep the active Ask/Auto/YOLO and Sandbox boundary, while MCP writer/destructive targets stay hard-blocked for the whole planning phase. |
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

The composer uses theme-coloured top and bottom borders and a slim bar cursor by
default. Long drafts grow to the available maximum height; once they overflow,
wheel events inside the composer scroll the draft without moving the insertion
cursor, while wheel events in the transcript keep scrolling the conversation.
Use `/theme auto|light|dark` to select the background mode, or `/theme <style>`
to select one of the named accent palettes shown by bare `/theme`.

The responsive footer keeps the active Ask/Auto/Plan or YOLO posture and current
interaction state on the left. On wider terminals, model, effort, and work mode
stay together on the right; a second row shows available Git identity, cache hit
rate, context use, compaction headroom, jobs, and balance. `ready` is the idle
composer state, not a model-health check. Pickers, approvals, image paste, shell
mode, and other active interactions replace it. Narrow terminals move, wrap, or
compact whole groups; labels and displayed work-mode values follow `/language`,
while `/work-mode` command arguments remain the stable English identifiers.

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
| Transcript text selection | Copies transcript text | Releasing an in-app drag writes through the verified native clipboard path in a local session (`pbcopy` on macOS, the available Wayland/X11 tool on Linux, or the Windows clipboard). SSH falls back to OSC 52 and labels the fallback instead of claiming native success. `Ctrl+C`/`Super+C`/`Meta+C` or right-clicking the active selection copies it again. |
| Composer text selection | Selects, copies, or replaces draft text | Releasing an in-app drag copies the selection through the same verified clipboard path as transcript text. Typing or pasting replaces the selection; arrow keys collapse it. |
| Right-click with no active selection | Pastes clipboard text locally | In a local session with in-app mouse capture on, Reasonix reads text only and routes it through the normal bracketed-paste handling. Over SSH, use the terminal paste shortcut because the remote process cannot read the local clipboard; `/mouse` restores the terminal's native right-click menu. Right-click with an active selection still copies that selection. |
| `/mouse` | Toggles in-app mouse capture | Off hands the mouse back to your terminal, restoring its native click-drag selection and right-click context menu, at the cost of in-app drag-select, the transcript scrollbar, and wheel-scroll. Set `REASONIX_DISABLE_MOUSE=1` to start every session with it off. |
| `Ctrl+C` | Copies, cancels, clears, or quits | Copies an active transcript or composer selection first. Otherwise it cancels a running turn, clears non-empty input, or quits on a second empty-composer press. |
| `Ctrl+D` | Quits the TUI | Immediate quit. |
| Your terminal's text-paste shortcut | Pastes text | Text stays on the terminal's bracketed-paste path (`Cmd+V` on macOS, commonly `Ctrl+Shift+V` on Linux, and the terminal's configured shortcut elsewhere). Reasonix consumes the resulting paste event and never probes for an image first. |
| `Ctrl+V` on macOS/Linux; `Alt+V` on Windows | Pastes a clipboard image | Image paste is a separate application action. The footer shows `Pasting image…` while the clipboard is read, then inserts an editable `[image #N]` token at the cursor. |
| `/paste-image` | Pastes a clipboard image | Command form of the same image-only action. |
| A line starting with `!` | Runs a shell command directly | The command runs locally without asking the model. |

Mode and display shortcuts:

| Key or command | What it does | Notes |
| --- | --- | --- |
| `Shift+Tab` | Cycles Ask → Auto → Plan → Ask | YOLO remains outside this composer-mode cycle; the footer shows the active mode. |
| `Ctrl+Y` | Toggles YOLO on/off | Turning YOLO off restores the previous Ask/Auto base when known. Terminals that forward Command/Super may also send `Cmd+Y`, but `Ctrl+Y` is the reliable terminal shortcut. |
| `--yolo`, `--dangerously-skip-permissions` | Starts chat in YOLO | Same runtime mode as `Ctrl+Y`. |
| `/work-mode [economy|balanced|delivery]` | Shows or switches the current session's work mode | `/profile` is a compatibility alias. Switching rebuilds the runtime atomically, preserves the conversation and approval posture, and is blocked while work is active. |
| `/theme [auto|light|dark|style]` | Shows or switches the CLI theme | Bare `/theme` lists background modes and named accent palettes. The choice is saved to the user config; `REASONIX_THEME` and `REASONIX_THEME_STYLE` can override it for one run. |
| `Ctrl+O` | Toggles verbose reasoning display | Also available through `/verbose`. |
| `Ctrl+B` | Expands or collapses long shell output | Long shell-output hint lines can also be clicked in the transcript; text selection is handled in-app while the full-screen TUI has mouse reporting enabled. |
| `/goal <objective>`, `/goal --research <objective>`, `/goal --simple <objective>`, `/goal status`, `/goal clear` | Starts, checks, or clears Goal | Goal is not in any keyboard cycle; clearly long-horizon goals automatically enable AutoResearch. Ordinary prompts with strong AutoResearch signals are also upgraded into Goal. |
| `/migrate`, `/migrate --from <legacy-dir>` | Retries legacy migration or imports sessions from a chosen v0.x source | Use `--from` for custom Windows v0.52 install/data directories; it imports sessions only. See [Configuration paths](./CONFIG_PATHS.md). |

Picker and approval shortcuts:

| Context | Keys | What they do |
| --- | --- | --- |
| Slash or `@` completion | `Up` / `Down`, `Ctrl+P` / `Ctrl+N`, `Tab` / `Enter`, `Esc` | Move, accept, or close the completion menu. |
| Tool approval prompt | `y`/`1`, `a`/`2`, `p`/`3`, `n`/`4`, `Enter`, `Esc`, `Ctrl+C` | Allow once, allow for session, persist allow, deny, accept default allow once, deny, or cancel the turn. |
| Ask question card | `Up`/`Down` or `j`/`k`, `Left`/`Right` or `h`/`l`, `Space`, `Enter`, `1`-`9`, `Esc`, `Ctrl+C` | Navigate answers/tabs, toggle multi-select answers, submit/activate, pick numbered options, dismiss, or cancel the turn. |
| Rewind picker | `Up`/`Down` or `j`/`k`, `Enter`, `b`, `c`, `d`, `f`, `s`, `u`, `Esc` | Choose a turn, apply both/conversation/code/fork/summarize actions, or go back/close. |
| Model, provider, or resume picker | `Up`/`Down` or `Ctrl+P`/`Ctrl+N`; `j`/`k` while search is empty; type to filter; `Enter`; `Esc` | Search, select an item, or close the picker. Once search input starts, `j`/`k` become query text. `/provider` opens that provider's model list. |
| MCP import picker | `Up`/`Down` or `j`/`k`, `Space`, `Enter`, `Esc` / `Ctrl+C` | Move, select servers, import selected servers, or cancel. |
| MCP manager | `Up`/`Down` or `j`/`k`, `Enter`, `Left`/`Right` or `h`/`l`, `r`, number keys, `q` / `Ctrl+C` | Navigate server lists/details, refresh, choose actions, or close. |
| `/clear` confirmation | Arrow keys or `j`/`k` / `Tab`, `Enter`, `y`, `n`, `Esc` / `Ctrl+C` | Toggle Clear/Cancel, confirm clear, or cancel. |

Mode meanings:

| Mode | Meaning |
| --- | --- |
| Ask | Prompts for fallback writer approvals. |
| Auto | Auto-allows fallback approvals; explicit `ask` / `deny` rules still apply. |
| YOLO | Skips ordinary tool approval prompts; `deny`, user `ask` questions, and plan approval prompts still wait. |
| Plan | Directs the model to plan first — a plan-first workflow, not an all-tools read-only mode. Built-in writers still follow the active Ask/Auto/YOLO rules and Sandbox; installed MCP writers, destructive targets, and untrusted readers are hard-blocked for the whole planning phase (approval cannot release them; they return once Plan exits), and explicit phase-only tools such as `complete_step` wait until approval. |
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

Ask is not read-only: after approval, a writer can still run. Permissions decide
whether to allow or prompt; the Sandbox is the enforced capability boundary.

Permissions are *policy* (which calls to allow / prompt). The **sandbox** is
*enforcement*: the file-writers (`write_file` / `edit_file` / `multi_edit` / `move_file`)
refuse any path outside `[sandbox] workspace_root` (default: the current dir, so
edits stay in the project), resolving symlinks and `..` so a link can't tunnel
out. `forbid_read` optionally hides sensitive files or directories from the agent's
read/list/search tools; use absolute paths or `${HOME}` / `${VAR}` references,
not `~`, because config expansion is environment-variable based. `bash` is
itself jailed by default when an OS sandbox is available (`[sandbox] bash`,
Seatbelt on macOS and bubblewrap on Linux):
commands may write only those same roots plus platform-specific command
temp/cache roots, cannot read configured `forbid_read` roots while the OS
sandbox is active, and reach the network only when `[sandbox] network` is set.
Reasonix always removes saved provider and bot credential variables from tool
subprocess environments and automatically adds its global credential `.env` to
the runtime read-deny boundary. Project `.env` files keep their existing
workspace-scoped behavior.
**Windows note:** Reasonix does not ship an OS-level Bash sandbox on Windows.
The effective mode is fixed to `off`; even an older config containing
`bash = "enforce"` resolves to `off`, `reasonix doctor` flags the ignored value,
and the desktop selector is read-only. Bash commands therefore run unconfined,
while the dedicated file tools still enforce `workspace_root`, `allow_write`,
and `forbid_read` in process. Saved credential variables are still removed from
the child environment, but an approved unconfined shell runs as the user and is
not a security boundary for other user-readable files.

When no OS sandbox backend is available, `bash = "enforce"` refuses bash
execution instead of running unconfined. Install the platform sandbox backend
(bubblewrap/`bwrap` on Linux, `sandbox-exec` on macOS) or set
`[sandbox] bash = "off"` to explicitly restore the pre-1.16 unconfined shell
behavior. On Windows the compatible value is always `off`.

For coding-quality reports, run `reasonix doctor quality <branch-id-or-path>`
(add `--json` for structured output). This reads the selected session but emits
only content-free counts and profile categories: model family, runtime profile,
collaboration / approval modes, message and tool-call counts, verification and persisted
compaction-summary counts, plus desktop token/cache telemetry when available.
It omits transcript text, paths, session identifiers, tool arguments and output,
endpoints, and custom model names, so the result is suitable for a public issue
or Discussion. This differs from `reasonix doctor session`, whose support zip
contains the complete unredacted transcript and must remain in a trusted support
channel.

## Capability diagnostics

Use this when a skill, slash command, hook, plugin package, MCP server, or
`AGENTS.md` is missing, shadowed, untrusted, or fails to start. Full flag
reference, JSON schema, and issue codes:
**[Capability diagnostics](./CAPABILITY_DIAGNOSTICS.md)**.

```bash
# Static (default): no network, no MCP child processes
reasonix doctor capabilities

# Machine-readable (stdout is pure JSON)
reasonix doctor capabilities --json

# Another workspace root
reasonix doctor capabilities --root /path/to/project

# Live MCP probe — only when you explicitly allow starting third-party servers
reasonix doctor capabilities --live --timeout 5s
```

| Surface | How |
| --- | --- |
| CLI | `reasonix doctor capabilities` (above) |
| Desktop | **Settings → Diagnostics** — refresh, copy redacted JSON, optional “include current session runtime” (reads the active tab Host only; does **not** start MCP) |
| Agent | `/reasonix-guide` (built-in inline skill) or ask naturally; it prefers static doctor JSON before `--live` |

Exit code `0` allows warnings/info; `1` means at least one `error` (or a live
start failure); `2` is bad flags. This is separate from `reasonix doctor`
(providers/sandbox) and `reasonix plugin doctor <name>` (one package).

## Plugins (MCP)

Reasonix is an MCP client. A `[[plugins]]` entry's `type` selects the transport:
`stdio` (default) launches a local subprocess (`command`/`args`/`env`); `http`
(Streamable HTTP) connects to a remote `url` with optional static `headers`
(`${VAR}` / `${VAR:-default}` expanded from the environment, so tokens stay out
of the file).

The normal setup path is intentionally one step. Use Desktop's **Add and
connect**, `/mcp add`, or ask Reasonix to install a package, URL, or `.mcp.json`.
That explicit install is also authorization: the server is saved and connected
in the current session, and no second trust step appears now or on the next
startup. Explicit deny rules still win. With no advanced approval override, the
installed server's calls run directly, including tools that declare
`destructiveHint`; choose `auto`, `prompt`, or `writes` when those calls should
retain fresh review. Plan/read-only sub-agents still expose only eligible tool
identities. A server merely discovered in repository-controlled
`reasonix.toml` or `.mcp.json` is different: Reasonix asks once to confirm the
exact command or endpoint, records that decision without launching a temporary
inspection process, then starts the server once. It reconnects automatically
while that value is unchanged and asks again only after a change.

stdio servers keep one process for initialize, reads, and writes, so stateful
servers such as browsers retain sessions and open pages. Because an OS sandbox
is fixed when a process starts, this shared process uses the server's normal
writer sandbox for every call; `readOnlyHint` and read-only sub-agent filtering
are dispatch policy, not a second per-call process sandbox.

Tools surface to the model as `mcp__<server>__<tool>`. A tool declaring MCP's
`readOnlyHint: true` joins parallel dispatch and the ordinary
permission reader-default. Because the annotation is supplied by a third-party
server, it is accepted by the main Plan workflow only as ordinary permission
classification; it does not grant access to the dedicated planner or read-only
research sub-agents. Use the local `trusted_read_only_tools` override for a
reader you have audited. Tools without the hint remain write-capable. While planning, built-in
writers keep the ordinary permission posture; installed MCP and proxy-resolved
MCP writers, destructive targets, and untrusted readers are hard-blocked before
any approval and return to their normal approval flow once Plan exits.

MCP `destructiveHint: true` is stricter than the read-only classification. Under
`auto`, `prompt`, or `writes`, every destructive call requires fresh human
approval even if the tool also reports `readOnlyHint`, the current posture is
Auto/YOLO, or an allow rule was saved — Guardian, `auto_review`, and session
grants cannot authorize it. An effective `approve` mode records that the user
authorized the server or tool and permits the call directly.

`approvals_reviewer = "auto_review"` routes the calls that actually need a
review — `prompt` mode, writer hits under `writes`, and `auto` calls the global
posture would Ask about — to the session Guardian, and a successful verdict
(allow or deny) is final. When the reviewer is missing, times out, fails, or
returns no verdict, the call falls back to fresh human approval: a prompt that
Auto/YOLO, the approved-plan window, and session grants cannot answer.
Non-interactive runs and sub-agents fail closed in every reviewer-required
case.

Server and raw-tool approval policy stays local and never changes the schema
sent to the model:

```toml
[[plugins]]
name = "github"
command = "github-mcp"
default_tools_approval_mode = "writes" # auto|prompt|writes|approve
tools = { "delete_repository" = { approval_mode = "prompt" } }
approvals_reviewer = "auto_review"     # user|auto_review
trusted_read_only_tools = ["issue_read", "pull_request_read"]
```

For a user-authorized server, omitting these advanced approval fields permits
all calls directly. If a field is present, `auto` delegates to the global
Ask/Auto/YOLO permission posture; `prompt`
reviews every call; `writes` reviews only writer-classified calls; and `approve`
allows calls directly, including destructive calls. Explicit deny rules always
win; `destructiveHint` forces a new review for every mode except `approve`. A
raw-tool `tools` entry overrides the server default. `trusted_read_only_tools`
remains a compatibility field and explicit local declaration for audited
readers on servers that omit or cannot reliably maintain annotations.

Two boundaries are worth knowing. `writes` trusts the server's read-only
classification, so a server that mislabels a writer as `readOnlyHint` escapes
that review — use `prompt` for servers you do not trust to maintain
annotations. And when Guardian is enabled with no `approvals_reviewer`
configured, `prompt`/`writes` reviews keep the legacy routing: Guardian
pre-screens the call and may allow it without a human prompt; set
`approvals_reviewer = "user"` when every review must reach a person. A
project's `.mcp.json` merges these fields into the session, so review
`approve`/`writes` policies in a repository you did not author like any other
code — explicit deny rules still apply, and destructive reviews apply unless
the effective mode is `approve`.

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
disable a server for the current session. For a read-only config/runtime health
report across skills, hooks, packages, and MCP (without changing settings), see
[Capability diagnostics](./CAPABILITY_DIAGNOSTICS.md)
(`reasonix doctor capabilities` or **Settings → Diagnostics**).

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
`/tree`, `/branch`, `/switch`, `/todo`, `/model`, `/work-mode`, `/mcp`, `/skills`, `/hooks`,
`/memory`, `/goal`, `/output-style`, `/sandbox`, `/language`,
`/auto-plan`, `/reasoning-language`, `/help`) run
locally — `/help` lists them all. Built-in **skills** such as `/init`,
`/explore`, `/test`, and `/reasonix-guide` also appear in the slash menu and via
`run_skill` (bodies load on demand; only the index line is cache-stable). Use
`/reasonix-guide` when you need config or capability troubleshooting; it points
at `reasonix doctor capabilities` (see
[Capability diagnostics](./CAPABILITY_DIAGNOSTICS.md)). `/new` starts a new
session while saving the previous transcript for history/resume; `/clear` asks
for confirmation, then discards the current context without saving it. `/tree`
shows saved conversation branches, `/branch [name]` forks the current
conversation tip, `/branch <turn> [name]` forks from an earlier checkpointed
turn, and `/switch <id|name>` loads another branch. **Custom commands** are
Markdown files under `.reasonix/commands/` (project) or `~/.reasonix/commands/`
(user) — `review.md` becomes `/review`, a subdirectory namespaces it
(`git/commit.md` → `/git:commit`). The body is a prompt template; invoking the
command sends it as a turn.

### Subagent profiles

Subagent profiles are manual Skills with `runAs: subagent` and
`invocation: manual`. They are stored in the same project/global Skill roots as
the desktop settings page, so profiles created on either surface are immediately
available to the other after the session refreshes. In interactive chat, invoke
one with `/<name> <task>`; Reasonix runs an isolated child loop and keeps only
the task and final answer in the parent conversation.

The headless CLI provides explicit management and execution commands without
changing the ordinary `reasonix run` task semantics:

```bash
reasonix subagent list
reasonix subagent create reviewer --description "Review changes" --prompt-file reviewer.md --tools read_file,grep,bash
reasonix subagent edit reviewer --effort high --model deepseek-pro
reasonix subagent try reviewer "review the current diff"   # always read-only
reasonix subagent run reviewer "review and fix the current diff"
reasonix subagent delete reviewer --yes
```

`create` defaults to project scope when a workspace is available and to global
scope otherwise; pass `--scope project|global` to choose explicitly. `edit`
changes only explicitly supplied fields, and an empty value such
as `--model=` or `--tools=` clears that field. The profile editors deliberately
refuse custom-path or richer hand-authored Skills so they cannot discard
frontmatter, references, or scripts; manage those files through the Skills
workflow instead. Built-in profiles have no editable file, so `edit` accepts
only `--model` and `--effort` for them and stores the same per-name overrides as
the desktop settings page.

See [Subagent profiles](./SUBAGENT_PROFILES.md) for the complete CLI reference,
Skill file format, model precedence, safety behavior, and troubleshooting.

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
The Memory v5 execution compiler has been removed. Earlier releases (up to
v1.17.x) could compile a user turn into a `<memory-compiler-execution>` contract
and store local compiler state; current releases never do either, the
`[agent].memory_compiler` config key is retired (a one-time migration removes it
from existing configs), and transcripts recorded by those older releases still
display normally — the original prompt is recovered from the legacy contract
block for previews and history.
For implementation details of session memory retrieval, see
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

`reasonix setup` manages providers, model lists, credentials, connection tests,
and the default model. It stages changes until Save and exit, and synchronizes
provider access with the desktop app. See the [CLI reference](./CLI.md#configure-providers).
Running two models together (executor + planner, separate cache-stable sessions)
is a one-line edit afterwards — set `planner_model` to any other enabled provider:

```toml
[agent]
planner_model = "deepseek-pro"   # used as the low-frequency planner
```

The planner sees loaded `REASONIX.md` / `AGENTS.md` memory and a small read-only
research tool set, so it can inspect relevant files before handing a plan to the
executor. Writer and workflow tools remain executor-only. Reasonix manages
normal execution automatically: if an active todo produces no new completion,
unique read, command, or mutation for 8 tool-call rounds, the host asks the
executor to reassess. After 16 no-progress rounds it pauses with saved work that
can be resumed in the next user turn. Exact repeats do not count as progress;
new host-observed work renews the lease. Two-level task lists keep the same
single-current contract: the active level-1 sub-step is the one `in_progress`
item while its level-0 phase stays `pending`; sub-steps are worked and signed
off in order, and once every sub-step has completed the phase itself becomes
`in_progress` for its own final sign-off.

Existing `[agent].max_steps` and `planner_max_steps` keys remain syntactically
accepted during upgrades, but their values are ignored and removed with a
one-time notice. This prevents a stale hidden limit from truncating automatic
progress or inherited subagent work. Use the one-off CLI `--max-steps` flag when
an explicit run budget is needed; unattended bots retain `[bot].max_steps`.

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
reached, but writer-capable `task` / `run_skill` remain unavailable inside these
read-only child registries. In token economy mode, connect this narrow surface
with `connect_tool_source(source="read_only_skill")` when that isolation is
required; loading the full `skills` source in Plan is allowed, and subsequent
writer calls still pass through Permissions/Sandbox.

Every strict read-only child is built through one shared construction
pairing — `RunReadOnlySubAgentWithSession` for batch children and
`NewReadOnlyAgent` for the interactive two-model planner — which marks the
child permanently read-only and applies a final registry filter. The filter
removes writers, destructive MCP targets, MCP readers backed only by a
third-party server hint, and every host-mutating tool. An MCP reader is
eligible only when explicitly declared in local configuration or declared by
a currently verified signed official package. Eligible readers may still
start on demand. These are
the strict read-only entrances:

| Entrance | Purpose |
| --- | --- |
| `read_only_task` | Isolated read-only research child from the main session |
| `parallel_tasks` (read-only) | Concurrent read-only research children |
| `fleet` with `read_only: true` | Parallel profile-aware batch (forced read-only per item) |
| `read_only_skill` | The same isolation driving an existing skill |
| `reasonix review` (CLI) | Read-only review of a diff or branch |
| Desktop preview/review subagents | Read-only desktop analysis surfaces |
| Two-model planner | The dedicated planner's read-only registry |

Inside a strict child, `use_capability` re-checks the resolved target before
commit/permission/hooks/execution. An unconnected eligible MCP reader may start
on demand from the current schema cache; its cached security fingerprint is
checked against the live initialize/tools-list result before `tools/call`.
Schema or safety drift means zero executions and a normal retry with current
policy. `auto_review` cannot raise
privileges there; a reader that would need a local prompt fails closed. This
is a stricter layer than the main Plan workflow: Plan hard-blocks MCP
writer/destructive targets for the entire planning phase — no approval can
release them until Plan exits — while built-in writers keep
Permissions/Sandbox, whereas a strict read-only child never exposes writers at
all.

Choose the startup runtime profile with
`--profile economy|balanced|delivery` (for example, `reasonix run --profile
delivery "fix and verify this bug"`). Economy starts with nine tools: direct
read/bash/edit/write, background-shell lifecycle controls, `ask`, and
`connect_tool_source`. Dedicated search/file/workflow tools, session history,
memory mutation, slash commands, Skills, MCP, LSP, web access, installation, and
subagents are connected only when the task needs them. Balanced is the default
with the complete tool surface. Delivery keeps that complete surface,
adds one stable proxy tool (`use_capability`) for on-demand MCP inspect/call
without schema churn, and adds a stable contract to establish acceptance
criteria, fix root causes, verify the result, and review the final diff. The
host enforces that contract: mutations and verification commands are blocked
until a concrete `todo_write` acceptance list exists; a changed result cannot
finalize until it has been reviewed, verified after the latest mutation, and
signed off with `complete_step`; Skill/MCP `require`/`prefer` routes must be
invoked or declined with host-proven reasons; and medium/high-risk changes
require structured review (and security review when high). Meta tools such as
`task`, `run_skill`, and `review` are not counted as mutations by themselves —
only real child writes are. Read-only analysis remains available without
forcing a write.
Inside an interactive TUI session, use `/work-mode` to inspect the current
choice or `/work-mode economy|balanced|delivery` to switch it. `/profile` is a
compatibility alias. The switch atomically rebuilds the controller while
preserving history, the session path, leases, and the Ask/Auto/YOLO posture; it
is rejected while a turn, approval/question, background job, or another runtime
switch is active. A failed build leaves the previous controller usable. This
command changes only the current session and does not persist a new global
default. Crossing profiles creates one new provider cache prefix. Within
Balanced and Delivery the system contract and tool schema then stay stable; in
Economy each successful `connect_tool_source` call adds the connected schemas
to the next request, creating one more prefix that stays stable until the tool
surface changes again.

Desktop tabs expose the same three choices and persist Economy or Delivery;
legacy empty/`full` values remain Balanced.

For interactive frontends, plan mode is manual by default. Set
`agent.auto_plan = "on"` to make complex-looking tasks enter plan mode
automatically: Reasonix first drafts a plan, then waits for approval before the
workflow switches to implementation. Tool calls made while drafting still use
the current Permissions and Sandbox. `auto_plan_classifier` can
name a cheap provider such as `deepseek-flash`; it is only called for borderline
inputs and falls back to the heuristic if classification fails. Use
`/auto-plan off|on` inside `reasonix` to change the user-level setting, or
`reasonix config auto-plan off|on` from a shell/script. Auto-plan is user-level
only; `agent.auto_plan` in a project `reasonix.toml` is ignored. The visible
reasoning language uses a similar shape: `/reasoning-language auto|zh|en` in the
session, or `reasonix config reasoning-language auto|zh|en` in a shell/script.
Pass `--local`
to the reasoning-language shell command only when you intentionally want a
project-local override.

The why behind separate sessions (keeping each model's prefix cache-stable) is in
[`SPEC.md` §3.5](./SPEC.md#35-two-model-collaboration-coordinator).
