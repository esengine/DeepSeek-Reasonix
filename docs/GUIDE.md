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
- [Billing and display currency](./BILLING.md)
- [CLI reference](./CLI.md)
- [Environment variables](#environment-variables)
- [Web frontend](#web-frontend)
- [Configuration paths](./CONFIG_PATHS.md)
- [Reasoning language](./REASONING_LANGUAGE.md)
- [Task contracts and pause policy](./TASK_CONTRACT.md)
- [Custom OpenAI-compatible providers](#custom-openai-compatible-providers)
- [Hooks](#hooks)
- [Desktop hooks](#desktop-hooks)
- [Keyboard shortcuts](#keyboard-shortcuts)
- [Permissions & sandbox](#permissions--sandbox)
- [Capability diagnostics](#capability-diagnostics)
- [Plugins (MCP)](#plugins-mcp)
- [Slash commands](#slash-commands)
- [Embedded documentation retrieval](#embedded-documentation-retrieval)
- [@ references](#-references)
- [Two-model collaboration](#two-model-collaboration)
- [Letting the model change a setting](#letting-the-model-change-a-setting)
- [Google Gemini, native (experimental)](#google-gemini-native-experimental)

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
show_turn_usage = false             # hide per-request token/cost receipts in the TUI; default true

[agent]
reasoning_language = "auto"      # visible reasoning text: auto|zh|en
# plan_mode_read_only_commands = ["gh issue view"]   # legacy compatibility only; Plan bash now uses Permissions
# planner_model = "deepseek-pro"      # optional low-frequency planner
# subagent_model = "deepseek-pro"     # optional default for runAs=subagent skills
# subagent_models = { review = "deepseek-pro", security_review = "deepseek-pro" }
# max_subagent_depth = 8              # nested delegation depth; a named stop against runaway recursion, 0 removes it; set 1 for the single-layer boundary (user config only)
# max_subagent_concurrency = 0        # session-wide sub-agent concurrency (task/fleet/skills); 0 = no limit (user config only)
# max_parallel_writers = 0            # concurrent writers with non-overlapping write_paths; 0 = no limit, write claims keep writers apart (user config only)
# max_side_sessions = 0              # side conversations open at once; 0 = no limit (user config only)
# max_provider_retries = 10          # retries of a failed provider connection before the turn stops; 0 = keep trying until cancelled (user config only)
# max_queued_inputs = 0              # prompts waiting in a session's queue; 0 = no limit, the queue's byte limits still apply (user config only)
# max_subagent_steer_bytes = 16384    # bytes in one message of guidance to a running sub-agent, which keeps one message from filling its context; 0 = no limit (user config only)
# max_subagent_steers_per_minute = 30  # messages of guidance per minute to one running sub-agent, a stop for a loop; 0 = no limit (user config only)
# write_lease = "strict"              # user file only; default optimistic: a writer that declares no path (bash, MCP) does not hold the whole workspace; strict makes it hold it
# write_lease_max_wait_minutes = 0    # minutes a write waits behind another session's claim before it fails with workspace.claim_timeout; 0 (default) = wait as long as the turn lasts, 120 is a sensible limit (user config only)
# subagent_claims = "strict"          # user file only; default optimistic: a sub-agent that declares no write_paths holds only the path it is writing; strict makes it hold the whole workspace
# plan_approval = "auto"             # user file only; a submitted plan starts executing without the approval click, and the transcript says so; default ask
# default_mode = "plan"              # user file only; a new conversation starts in plan mode (a resumed one keeps its own; Settings has a switch); default normal
# host_verification = "on"           # user file only; "on" holds a turn that changed files open until a check, a review and a sign-off exist; unset or "off" does not (unset still holds them under the delivery preset); the receipt always says what was checked
# compact_ratio = 0.85             # sole auto trigger; presets 0.70 / 0.80 / 0.85
# max_output_tokens = 0            # recommended: automatic (DeepSeek default high → ~64K; not unlimited)
# max_output_tokens = 32768        # ordinary coding / cost control
# max_output_tokens = 65536        # heavy reasoning / long tool loops
# max_output_tokens = 131072       # only after repeated finish_reason=length
# max_output_tokens never changes compact_ratio; only the final send-time clip does

[[providers]]
name        = "deepseek-flash"
kind        = "anthropic"
base_url    = "https://api.deepseek.com/anthropic"
model       = "deepseek-flash"
api_key_env = "DEEPSEEK_API_KEY"
web_search  = true
# also preset: deepseek-pro
# idle_timeout_seconds = 300   # per-provider stream idle watchdog; how long a call may send nothing before it is read as dropped (default 300s; a silent pre-header wait is retried once, so up to ~2×)

[tools]
enabled = []   # omit/empty = all built-ins
# disabled = ["web_fetch", "ask"]   # removed from every request by name, framework and MCP tools included; a call to one is refused with the reason; a tool the host itself reads (todo_write, complete_step, await_user, conclude_blocked, update_goal, use_capability) stays on and a warning says which; a project file can only add names, and Settings lists those as turned off by the project; Settings has a switch per tool; applies to conversations opened afterwards
bash_timeout_seconds = 120   # foreground safety cap; set 0 for no tool-local cap
protect_changed_files = true   # refuse a whole-file write over a change made since the agent last read or wrote the file
browser_tools = true   # built-in browser tools; the browser starts on first use
# mcp_resources = true  # user file only; a read_mcp_resource tool for resources a connected MCP server lists or builds from its declared templates (a {+x} template is wide: read the full address on the card before approving); asks first; default off
# web_search_backends = ["searxng"]  # user file only; ordered chain for web_search
# searxng_url = "http://localhost:8888"  # your own SearXNG instance
mcp_startup_timeout_seconds = 30   # background initialize + tools/list safety cap
mcp_call_timeout_seconds = 300   # default MCP call safety cap; per-plugin/tool overrides may raise it

[tools.system_one]
api_key_env = "TYPESAFE_API_KEY" # optional TypeSafe AI System One decision protocol
model = "jev-latest"
# base_url = "https://api.typesafe.ai"

[tools.system_one.laya]
local = false                      # true: run the official Python SDK locally (`pip install laya`)
python = "python"                  # interpreter containing the laya package
model = "auto"                     # auto|english|multilingual|typed-decisions
# http_base_url = "http://127.0.0.1:8080" # self-hosted POST /v1/systemone gateway
# http_api_key_env = "LAYA_API_KEY"       # optional gateway Bearer token

[environment]
enabled = true   # inject a stable startup summary of OS, shell, and common tools
offline = false  # set true when outbound network access is unavailable; prevents futile retries
# [environment.tools]
# go = "/opt/homebrew/bin/go"   # optional explicit trusted path; workspace-local paths are not auto-executed

[skills]
# paths = ["~/my-skills", "../shared/skills"]   # extra custom skill roots
# excluded_paths = ["~/.agents/skills"]         # hide convention roots without deleting folders
# disabled_skills = ["review"]                  # hide skills until /skill enable <name>
# suppress_warnings = true                      # quiet non-fatal skill loading warnings; doctor still reports issues

[permissions]
mode  = "ask"                                # writer fallback when no rule matches: ask|allow|deny
deny  = ["Bash(rm -rf*)", "Bash(git push*)"] # hard-blocked in every mode
allow = ["Bash(go test:*)"]                  # never prompted

[sandbox]
# workspace_root = ""          # file-writers confined here; empty = current dir
# allow_write    = ["/tmp"]    # extra dirs write_file/edit_file/multi_edit/move_file may touch
# forbid_read    = ["${HOME}/.ssh"]   # paths the agent must not read or list
# allow_devices  = ["/dev/nvidia*"]   # host device nodes bound into the Linux bash sandbox (user config only)
# egress_unlisted = "allow"    # unattended long tasks (user config only): hosts outside allowed_domains pass without asking, said once each; denied_domains still refuse

[peers]
# enabled = false              # your sessions on this machine can message each other; every message waits for that session's user to approve it; also a switch in Settings, applied at the next turn (user config only)
# dispatch = false             # the model may hand a task to a session in another of your projects, via dispatch_to_project, and ask you to open folders as projects with open_projects (one card lists every path; nothing is added or trusted until you approve it, and no mode answers it for you); needs enabled; turning it on is your consent for those sessions' tasks and replies, no card, and a project in your sidebar you have not decided about is trusted by being named; the opened session keeps the sender's model, preset, approval mode and planning, so a YOLO sender opens a YOLO session that answers the target project's ask rules too (deny rules still hold) (user config only)
# max_dispatch_depth = 1       # how many sessions deep a dispatch may chain; 0 removes the ceiling (user config only)
# max_dispatched_sessions = 8  # sessions one session may keep open from its dispatches; 0 removes the ceiling (user config only)
# release_projects = []        # projects that take messages from your other sessions without the card; their own approvals and sandbox still apply; they start over a pause the host set itself (session switch, unreadable @ reference, reopen) but never over one you pressed (user config only)

[mods]
# enabled = false              # run the hooks module of installed Claude Code mods; full trust, no sandbox (user config only)
# hook_budget_ms = 0           # one hook's own running time in milliseconds; 0 keeps 10000 (user config only)

[serve]
# auth_mode = "token"          # token (default)|password|none; none still requires the launch token for changes
# token = ""                   # optional fixed token; empty token mode generates one at startup
# password_hash = ""           # bcrypt hash generated with reasonix serve --hash-password --password '...'
# behind_proxy = false         # true only behind a trusted reverse proxy

[desktop]
# max_workspaces = 0           # most projects the sidebar remembers; 0 = no limit (user config only)
# session_ref_tokens = 8000     # most tokens one referenced past conversation (@session:<id>) adds to a message; 500..200000

[[plugins]]
name    = "example"
command = "reasonix-plugin-example"
startup_timeout_seconds = 60   # optional initialize + tools/list cap
call_timeout_seconds = 600   # optional per-server MCP call timeout
tool_timeout_seconds = { "generate_video" = 1800 }   # optional raw MCP tool names
```

For the full schema and every field's contract, see [`SPEC.md` §5](./SPEC.md#5-configuration-toml).

Installed and project-configured MCP servers need no per-tool trust
list. The dedicated two-model Planner may use every non-destructive MCP tool,
even when the server omits `readOnlyHint`; strict read-only sub-agents still
require `readOnlyHint: true` and no `destructiveHint`.

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

To hand extra variables to every command the `bash` tool runs, set the
user-global `[tools.shell.env]` table in `config.toml` (for example
`BASH_ENV`). It is applied over the inherited environment; a project
`reasonix.toml` cannot set it, so a cloned repository cannot inject variables
into your commands.

A preset is not scoped to a command you asked for: `BASH_ENV` is sourced before
*every* bash command, including the ones the host classifies as read-only and the
ones it runs as verification.

So keep it side-effect free and outside the workspace's writable roots: anything
it writes can invalidate what a verification just proved. The table reaches the
local bash tool only — a remote workspace and `reasonix review` build their own
sandbox and do not carry it.

### CLI telemetry

The CLI can send a once-per-day anonymous active-install ping and bounded,
content-free event counters to `https://crash.reasonix.io`. Configure the
user-global policy with:

```bash
reasonix config telemetry          # print the effective mode
reasonix config telemetry auto     # default: local interactive TTY only
reasonix config telemetry on       # also allow local headless `reasonix run`
reasonix config telemetry off      # disable and delete pending counter files
```

On the first eligible release-build interactive session, Reasonix explains the
exact data boundary and asks once before any telemetry request. The prompt is
`[Y/n]`: pressing Enter, `y`, or `yes` stores `auto`; `n` or `no` stores `off`
and deletes pending counters. After the choice is saved, enabled reporting is
silent and the prompt is not shown again. If the preference cannot be saved,
nothing is uploaded.

Reporting is always disabled in CI, development builds, and when
`DO_NOT_TRACK` is set or `REASONIX_TELEMETRY=0`. Under `auto`, redirected/piped
or otherwise non-interactive sessions do not report. When no choice has been
saved yet, these ineligible sessions neither prompt nor report. Network failures
after consent are silent and never change stdout, stderr, or the process exit
code; unsent counters stay in a bounded local queue for a later invocation.

The ping contains a dedicated random 128-bit CLI install ID, CLI version, OS,
architecture, and the `cli` surface marker. This ID is separate from the desktop
install ID and is not an account, hardware, repository, or session identifier.

Counter batches use that same ID for daily active-install deduplication and
contain only fixed buckets such as CLI mode/profile, permission/session mode,
turn latency, finish reason, cache-hit range, generic Provider/tool error class,
compaction, recovery counters, per-turn token-volume buckets, workspace-lease
contention buckets, and normalized UI language.

Closing the prompt without answering (end of input, Ctrl+D) stores nothing and
uploads nothing; it asks again the next time.

Reasonix never uploads prompts, answers, reasoning, tool names/arguments/output,
paths, repositories/branches, session IDs, exact token or cost values,
Provider/model names, base URLs, or environment variables.

### Trace export (OpenTelemetry)

Reasonix can hand each turn to your own observability stack as OpenTelemetry
GenAI traces. It is off, and nothing leaves the machine, until your user config
switches it on and names a collector:

```toml
[observability.otlp]
enabled = true
endpoint = "http://localhost:4318"   # OTLP/HTTP; /v1/traces is appended
header_name = "Authorization"        # optional: the header carrying a credential
header_env = "OTEL_EXPORTER_AUTH"    # the variable holding its value, never the value
service_name = "reasonix"            # optional
```

- **What is sent.** One trace per turn: an `invoke_agent` span, a `chat` span per
  model attempt (provider, model, token counts, finish reason), and an
  `execute_tool` span per tool call (tool name, failure class), nested under the
  tool that delegated the work.
- **What is never sent.** Prompts, answers, tool arguments and results, file
  paths and error text. Spans are built from typed event fields through one fixed
  attribute set, and a name that does not look like a name is sent as `unknown`.
- **Who decides.** Only your user config. A repository's `reasonix.toml` cannot
  switch it on or move it; the ignored key is reported at load.
- **Credentials.** `header_env` must be an `OTEL_*` or `REASONIX_OTEL_*` variable, so no
  setting can read an unrelated secret. A credential is refused over plain `http`
  to any host but loopback, the endpoint may not contain user info, and redirects
  are not followed.
- **Failure.** Export never blocks a turn: a slow or dead collector drops spans
  from a bounded queue.
- **Stability.** The upstream conventions are marked Development (read
  2026-10-18); every payload carries `reasonix.semconv_experimental = true` and
  attribute names can change. Not run against a real backend: built against the
  OTLP/HTTP JSON encoding and tested with a local collector.

Studio has the same switches under Settings, Network. A save reaches sessions
opened afterwards.

### CLI crash reports

An unhandled Go panic that reaches the CLI entrypoint is saved locally as a sanitized report under
`<Reasonix home>/cli-crash-reports`. Reasonix keeps at most 10 files with owner-only
permissions. The panic value is never serialized. Absolute source paths become
`<path>/<file>.go:<line>`, function arguments are removed, and the same secret,
token, email, and long-identifier scrubbers run both when saving and immediately
before sending.

Crash reports are never uploaded automatically. Review and manage them with:

```bash
reasonix report                 # preview newest; prompt before sending on a TTY
reasonix report list            # list local reports
reasonix report show [ID]       # preview without sending
reasonix report send [ID]       # explicit send; delete locally only after success
reasonix report delete [ID]     # delete without sending
```

Piped or redirected `reasonix report` calls only preview and never prompt or
send. The CLI telemetry setting does not auto-send or auto-delete
these separately reviewed reports. Runtime fatal throws, operating-system kills,
and panics in unwrapped background goroutines cannot be recovered by Go and do
not produce this local report.

## Web frontend

For local use, `reasonix web` starts the browser UI and opens it in your default
browser. Inside an interactive CLI session, `/web` snapshots the current session,
restores the terminal, and opens an explicit `/sessions/<id>#token=...` deep link.
Even a never-used session keeps its reserved ID without forcing an empty
transcript onto disk, so the first Web turn continues the same session identity.

Studio and the Web UI offer **Side question** beside the composer for read-only
questions without adding them to the main conversation.

A submitted question appears immediately. Answer text streams as it arrives,
with the current phase, tool name and elapsed time shown while it runs.

Collapsing the panel keeps the request running; closing it cancels the request
and discards that side conversation. If a request fails, partial text is marked
interrupted and the draft remains for retry.

```bash
cd your-project
reasonix web
```

Use `reasonix web --no-open` when you want to start the foreground Web server
and print its URL without opening a browser tab. The lower-level
`reasonix serve` command starts the same engine without opening a browser by
default. It remains the right entry point for remote development boxes,
supervisors, tunnels, reverse proxies, and shareable authenticated sessions.

`reasonix web` starts at `127.0.0.1:8787`, automatically tries 8788, 8789, and
so on when a port is busy (up to 100 retries), and defaults to a newly generated
token even when `[serve].auth_mode` is `none`. Each live process registers a
single-writer heartbeat file under `<Reasonix home>/server/instances/`; clean
shutdown removes its own file, while later instances lazily remove records whose
owner process is confirmed dead. Multiple Web instances can therefore share one
Reasonix home without overwriting registry state. The process stays attached to
the terminal; stop it with Ctrl-C.

`reasonix serve` also defaults to a freshly generated per-launch token unless
`[serve].auth_mode` or `--auth` says otherwise.

- `--auth none` (or `auth_mode = "none"`) is an explicit opt-out for a Serve
  fronted by its own authentication. Reads are open to whoever reaches a
  loopback or bound address; other Host names get 421 `serve.host_rejected`.
- Every state-changing request, approvals included, still needs the launch
  token: `Authorization: Bearer <token>`, or the cookie the printed
  `approvals:` link sets. Without it the answer is 403
  `auth.launch_token_required`.
- Serve writes the token to a 0600 file under `<Reasonix home>/remote/` and
  prints only its path; append `#token=<file contents>` to the printed link.
  A managed launch with `--token-file` names that file instead. The phone QR
  code a terminal shows in token mode still carries the token.
- Prefer `--token-file` over `--token`: argv is visible to other processes,
  sandboxed ones included. A plaintext `[serve].token` in the global config is
  readable from inside the sandbox; keep the secret in a file.
- On macOS and Linux the OS sandbox denies the remote state directory and every
  token file. Windows has no bash sandbox, so there only the read tools refuse.
- `[serve]` is read from the user config only; a project `reasonix.toml`
  cannot set it.

Choose the mode explicitly when binding outside loopback:

```bash
reasonix serve --auth token
reasonix serve --addr 0.0.0.0:8787 --auth token
reasonix serve --auth password --password 'temporary-password'
```

Token mode prints a share URL with `#token=...`; the Web page exchanges the
fragment for an HttpOnly cookie before starting API or SSE requests, keeping the
token out of request URLs, browser history, referrers, and access logs. Pass `--token` or set
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
`todo_write` tool, extension status/card/form/notification surfaces, and
provider balance when configured. Extension-hosted providers appear in the
model picker. Run `/reload` while idle to fail-atomically reload extension
sidecars and the runtime generation without restarting Serve. Use `--model`,
`--max-steps`, or `--resume` for one-off launches; otherwise `serve` uses the
user-global `default_model`.

If the selected Provider has no saved API key, a loopback-bound Serve still
starts and shows a Provider setup page instead of failing before the browser can
connect. After authentication, enter the key there; Reasonix writes it to this
host's global credential file with restricted permissions, rebuilds the active
controller in the same process, and opens the normal UI. The credential-writing
endpoint is disabled for non-loopback listeners. For a remote SSH window,
"this host" means the remote host reached through the SSH tunnel; the key is
not copied from the desktop machine.

## Editor integrations over ACP

`reasonix acp` exposes Reasonix as an ACP v1 stdio agent for editors and other
host clients. The dedicated **[ACP editor integration](./ACP.md)** guide covers
startup, capability negotiation, session lifecycle, independent model/work/
collaboration/approval controls, client filesystem and terminal capabilities,
MCP servers, permission requests, and the Reasonix mid-turn steering extension.

## Using Reasonix from DeepSeek Harness

DeepSeek Harness (`dsh`) can delegate a task to Reasonix as a subagent over ACP, with configuration only. [Using Reasonix from DeepSeek Harness](./DSH.md) covers the install through DSH's plugin market or by hand, switching the plugin on, the permission policy and the limits.

## Remote SSH

The remote module runs Reasonix on a remote host and reaches it over your own
SSH connection — VS Code Remote-SSH style. It bootstraps a persistent headless
`reasonix serve` on the remote host, forwards a local loopback port to it, and
opens the existing serve web client through that tunnel. The agent, its tools,
and its files all live on the remote host at full fidelity; nothing runs through
a lossy file proxy.

Linux, macOS and Windows remote hosts are supported. A Windows host needs PowerShell and OpenSSH, whichever login shell (cmd,
PowerShell or Git Bash) its `DefaultShell` names.

Hosts live in a user-global `[remote]` section of `config.toml`. Like
`[secrets]`, a project `reasonix.toml` cannot inject or override remote hosts —
a cloned repo can never steer where Reasonix opens SSH connections. Credentials
follow the provider idiom: the host names an env var (`passphrase_env`,
`password_env`) whose value lives in Reasonix's global `.env`; key material
itself is never stored — `identity_file` is a path.

Studio uses that host's explicit `workspace` as the default for host-level reads
and opens that omit a workspace.

Only when it is unset does the first nonempty
`workspaces` entry supply the default; with neither set, the attach receives an
empty workspace. An explicit open path takes precedence.

Reordering other
entries cannot override a declared `workspace`. These values come only from the
user's host configuration, and disabled hosts are refused before any link reuse.

The active SSH project's secondary line shows its actual connection directory,
including isolated worktrees. Its project menu shows the default connection
directory used by Refresh; these can differ. Settings' default action explicitly
chooses a new default.

A host-level read never borrows an unrelated pane's path.

```toml
[remote]
[[remote.hosts]]
name          = "gpu-box"
host          = "203.0.113.7"
user          = "dev"
identity_file = "~/.ssh/id_ed25519"
workspace     = "~/projects/app"
serve_install = "auto"            # Remote CLI: auto | npm | upload | never
provider      = "local"           # Model credentials: local (this machine, over the tunnel) | remote (that host's own)

[[remote.hosts.forwards]]
type   = "local"                  # local (-L) | remote (-R)
bind   = "127.0.0.1:5432"
target = "127.0.0.1:5432"
```

A WSL distribution on a Windows machine is a host with `kind = "wsl"` and a `distro` (see `reasonix remote wsl list`):

- It has no address, so it takes only the folder and install fields. `host`, `port`, `user`, keys, passwords,
  `proxy_jump`, `use_ssh_config` and forwards are rejected on it.
- Add one under Settings, Remote: the "WSL distribution" choice appears only on a machine that has WSL, and lists
  its distributions (WSL 1 ones are shown disabled). Studio then opens it like any other machine in the list.
- Connecting needs WSL 2 and a distribution that has `ps`
  and `nohup`; a minimal image without them is refused with the missing name.
- The kernel runs inside a shell session Studio keeps open, because WSL ends everything a session started when it ends.
  - When Studio closes its link, the kernel goes away with the distribution (WSL stops an idle one after about 15
    seconds).
  - Conversations are kept on disk and resume on the next connect.
  - Nothing keeps a distribution running behind your back.
- Model credentials come from the distribution's own configuration (`provider = "remote"`): under WSL's default
  networking the distribution cannot reach this machine's loopback, so this machine's provider is not shared.
- A folder chosen as a Windows path (`C:\work\app` or `\\wsl.localhost\<distro>\home\me\app`) is translated by
  the distribution; a folder it cannot see is refused.
- From the command line, `reasonix remote wsl add <name> <distribution>` adds one, and `remote test` and
  `remote connect` then work as they do for an SSH host.
  - `connect` keeps the session open until you press Ctrl-C, and the kernel stops with it.
  - `status`, `forward`, `serve` and `fs` stay SSH-only and refuse a WSL machine with
    `remote: host is not reached over ssh`.

For an agent's own shell commands, only `list`, `help` and `wsl list|test` run:

- Every other `reasonix remote` subcommand refuses there (exit 77), because it changes the machine book or opens a
  link, which a person does.
- The host marks the commands it runs for the agent to tell them apart. It guards against an agent that does not
  hide; it is not the sandbox.

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
so it never appears in `ps`), `serve-<slug>.broker` (0600; the provider broker
token, see below), and `serve-<slug>.log`.

### Getting the kernel there

The remote runs a `reasonix serve`. The binary is a static Go build — no
runtime, no Node, no glibc — so putting it there is a file transfer, not an
install. `serve_install = "auto"` tries, in order:

1. **The remote fetches its own release.** Reasonix reads `SHA256SUMS` here and
   hands that host the archive URL and the digest; the host downloads over its
   own connection and keeps the archive only if it matches. Nothing but the
   command crosses the SSH link, and a machine in a datacenter pulls the ~21MB
   archive far faster than a laptop can push it.
2. **Upload this machine's binary**, when the platforms match.
3. **Download here and push over SFTP**, for a host with no route to the
   release.
4. **npm**, last. `npm i -g reasonix` installs the same static binary but needs
   Node ≥18 over there to do it; it stays because a network that mirrors npm
   while throttling GitHub is a real one.

Set `serve_install` to `npm`, `upload` or `never` to take exactly one route.
`reasonix remote test <name>` reports which routes are open on a host before
you connect, and names what closes each one that is not.

### Model credentials

A remote session resolves its models on **your** machine by default. The
connection carries a reverse (`-R`) forward back to a provider broker Reasonix
runs on loopback here; the remote kernel calls that instead of a model
endpoint, and the request is issued from this machine with this machine's key.

So a remote host needs **no API key of its own and no egress to the model
API** — a locked-down build machine works. The key is never written to the
remote disk: only a per-connection broker token is, and only as a 0600 file.
The remote also never sees the endpoint, headers, or proxy settings behind a
provider; it sees the same catalog you do and nothing else.

Set `provider = "remote"` on a host to leave it resolving providers from its
own config instead, for a machine whose providers are deliberately not yours.
`reasonix remote serve start` never uses a broker: that serve is meant to
outlive the command that started it, and a broker published from a command that
has exited is a port nobody holds.

Two costs are worth knowing. Model traffic now leaves through your own uplink
rather than the remote's, which is a downgrade when the remote sits on a much
fatter pipe. And the models available to a remote pane are the ones configured
here, so a provider only that host had configured no longer appears unless you
switch it to `provider = "remote"`.

In the desktop app, manage hosts under **Settings -> Remote SSH**, then use the
status-bar chip or the host row's **Remote explorer** button to browse and edit
files over SFTP, manage port forwards, and start/open the remote workspace.
Opening a workspace creates a separate native Reasonix window, similar to a
VS Code Remote SSH window. The primary window owns the SSH tunnel; the remote
window is an isolated, lightweight shell and does not restore or acquire local
conversation sessions. The remote window uses **this** machine's providers
through the broker described above, so opening a workspace on a host that has
never held an API key just works. On a host set to `provider = "remote"` the
old path applies: the window shows the authenticated setup page first, saves
the key only in the remote Reasonix credential file, and activates the Provider
without restarting the remote Serve process. A transient SSH outage keeps the
remote window open;
the desktop reconnects in the background, re-attaches its loopback forward, and
reloads the window against the recovered Serve. An authentication or host-key
failure is terminal and closes the unusable remote window instead.

### Lifetime of the remote serve

- The `reasonix serve` process on the host is persistent. Quitting the desktop
  or losing the link does not terminate it, and the next connect attaches to it.
- A pane's session on that serve does not outlive the desktop. Each pane drives
  its own runtime there; closing the pane or quitting the desktop makes Studio
  call `POST /runtimes/{id}/close` on the remote serve before it takes the
  tunnel down.
- That close cancels a turn in flight and releases the session lease, so another
  window can open the session. Only the pane's runtime is closed: the serve
  process keeps running and the next connect can attach to it. Runtimes opened
  by other clients are left alone too, whichever way the serve was started.
- `provider = "remote"` decides only where model credentials come from. It does
  not detach a pane from the desktop.
- A serve already running for the workspace is attached to, never replaced,
  whether `reasonix remote serve start`, another window or a hand-started
  `reasonix serve --port-file` launched it. A later connect never deletes its
  port or pid files and never stops it.
- `serve start` uses no broker, so a host with `provider = "local"` cannot
  attach to it. That connect fails with an explanation and leaves the serve
  running. Set the host to `provider = "remote"` (it then needs its own
  credentials), or stop the serve with `reasonix remote serve stop <host>`.

## Custom OpenAI-compatible providers

In the desktop app, open **Settings -> Model -> Access -> Add model service ->
Custom provider** for proxies, aggregators, or self-hosted services that speak
the OpenAI-compatible chat API or Anthropic-compatible Messages API.

For common providers, choose **Add model service -> Recommended preset** instead.
New official DeepSeek entries use the Anthropic-compatible Messages endpoint by
default and enable provider-side `web_search`; the same `DEEPSEEK_API_KEY` works
for both protocols. On startup, Reasonix upgrades unmodified legacy
`deepseek-flash` / `deepseek-pro` entries that still use the official endpoint
and standard key/model settings. Customized official Chat Completions entries
stay unchanged and show an **Upgrade protocol** action in Settings. Proxy
endpoints, custom headers, model lists, and capability overrides are never
migrated automatically. Existing
separately named `deepseek-anthropic` entries remain compatible, but that
redundant preset is no longer offered for new access. Reasonix can prefill editable custom-provider entries for Kimi CN,
Kimi Global,
Kimi Coding Plan, MiMo API, MiMo Anthropic, MiMo Token Plan CN/SGP/AMS and their
Anthropic-compatible variants, MiniMax CN/Global API, MiniMax CN/Global
Anthropic, GLM CN, Z.AI Global, GLM/Z.AI Coding Plan OpenAI-compatible and
Anthropic-compatible endpoints, OpenCode Go, OpenCode Go Anthropic, OpenCode Go
DeepSeek Anthropic, OpenCode Go DeepSeek Responses, OpenCode Zen
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
max-effort support, and OpenCode Go per-model reasoning overrides. The dedicated
OpenCode Go DeepSeek Anthropic and DeepSeek Responses presets expose the verified
Flash routes and enable provider-side `web_search` by default; the Responses
variant uses stateless context replay. The existing mixed OpenCode Go Anthropic
preset remains scoped to Qwen and MiniMax so server tools are not sent to
unverified models. DeepSeek Pro remains on the Chat Completions preset because
live Anthropic and Responses requests currently fail in the OpenCode Go upstream
conversion. The OpenCode Go preset includes its native `kimi-k3` subscription
route with image input,
`high`/`max` reasoning effort, and a 1,048,576-token context window. Existing untouched
OpenCode Go preset installs are upgraded automatically; edited model catalogs
are preserved. The Kimi CN and Kimi Global direct-API presets also include
`kimi-k3` with image input, a 1,048,576-token context window, and the official
`low`/`high`/`max` effort scale (default `max`). For the official K3 endpoints,
Reasonix preserves complete assistant messages across turns, sends output limits
as `max_completion_tokens`, and omits K3's fixed sampling parameters. Untouched
legacy Kimi direct-API catalogs are upgraded automatically without changing the
default model; custom catalogs and endpoints are preserved. After adding a
preset, open its provider card if you need to change models, headers, endpoint,
or compatibility settings.

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
| Balance URL | Optional endpoint for wallet/balance lookup. | Set it when the provider exposes a balance endpoint and you want the desktop status bar to show it. The key is sent only to this address and never follows a redirect. |
| Balance format | How that endpoint answers: `deepseek`, `moonshot` or `openrouter` (`balance_format`). | A vendor's own domain (`api.deepseek.com`, `api.moonshot.cn`/`.ai`, `openrouter.ai`) is read in that vendor's format with nothing set. Any other address, a relay for example, must name the format, or it is shown as not configured and the endpoint is not asked. Existing entries on such addresses are written as `deepseek` once, which is how they were always read. Reasonix 1.x rewrites the shared config file without keys it does not know, so a format it dropped is put back at the next start from `provider-keys.json` beside the config, unless you removed it in the connection editor. |
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

## Hooks

Reasonix has hooks: shell commands that run at fixed points of the agent loop
and can observe, inject context, or (on two events) veto what is about to
happen. They are separate from permissions: `[permissions]` rules decide
whether a tool call is allowed or prompted, a hook runs your own code.

| Event | Fires | Can block |
| --- | --- | --- |
| `PreToolUse` | before a tool call, after matching; `toolName` and `toolArgs` are in the payload | yes |
| `PostToolUse` | after a tool call (success or failure) | no |
| `PostToolUseFailure` | after a tool call returned an error | no |
| `PermissionRequest` | before an approval prompt is shown | no |
| `UserPromptSubmit` | before a user prompt starts a turn | yes |
| `Stop` / `StopFailure` | when a turn ends / fails | no |
| `SessionStart` / `SessionEnd` | when a session becomes active / is closed or rotated by `/new` | no |
| `SubagentStart` / `SubagentStop` | around a foreground `task` call | no |
| `Notification` | when the agent needs the user's attention | no |
| `PreCompact` | before compaction; stdout becomes extra summary guidance | no |
| `PostLLMCall` | after each model turn; non-empty stdout on exit 0 replaces the stored reasoning text | no |

Hooks are configured in `<Reasonix home>/settings.json` (global) or
`<root>/.reasonix/settings.json` (project). Each event maps to a list of hooks:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "match": "bash",
        "command": "sh ~/.reasonix/hooks/no-git-push.sh",
        "description": "block git push",
        "timeout": 3000
      }
    ],
    "SessionStart": [
      { "command": "echo 'Team rule: run make lint before every commit.'" }
    ]
  }
}
```

Fields:

- `command` (required): run by the platform shell.
- `match`: tool events only; an anchored regex over the tool name, so `file`
  does not match `read_file`. Empty or `*` means every tool.
- `description`: the label shown in `/hooks` and in block notices.
- `timeout`: milliseconds; default 5000 for `PreToolUse`, `PermissionRequest`
  and `UserPromptSubmit`, 30000 otherwise.
- `cwd` and `env`: working directory and extra environment.

A malformed file loads no hooks and does not stop Reasonix.

**Contract.** The event payload arrives as one line of JSON on stdin
(`event`, `sessionId`, `cwd`, and by event `toolName`, `toolArgs`, `prompt`,
`toolResult`, `error`, ...). The exit code is the verdict:

- `0` passes. On `SessionStart`, stdout (plain text, or JSON with
  `hookSpecificOutput.additionalContext`) is injected once into the next real
  user turn, as described under [Desktop hooks](#desktop-hooks).
- `2` blocks, but only on `PreToolUse` and `UserPromptSubmit`. A blocked tool
  call is not run and the model receives `blocked: <hook> hook (<scope>) stopped
  this call — <stderr, or stdout if empty> · command: ... · source: ...`, so write
  the reason for the model to read.
- Any other code, or exit 2 on a non-blocking event, only warns the user.
- A timeout blocks on `PreToolUse` and `UserPromptSubmit` and warns elsewhere.
  A hook that cannot be spawned does not block, and neither does a script that
  crashes (exit 1, or 127 for a missing file): only an explicit exit 2 vetoes.

**Example: refuse `git push` in bash.** The payload's `toolArgs` for `bash` is
the tool's argument object, so the command is `toolArgs.command`. Save as
`~/.reasonix/hooks/no-git-push.sh` (needs `jq`):

```sh
#!/bin/sh
cmd=$(jq -r '.toolArgs.command // empty')
case "$cmd" in
  *"git push"*)
    echo "git push is disabled in this setup; ask the user to push." >&2
    exit 2 ;;
esac
exit 0
```

Without a hook, the simpler tool is a permission rule, which needs no script
and holds in every mode:

```toml
[permissions]
deny = ["Bash(git push*)"]
```

Prefer the rule when a command pattern is enough; use a hook when the decision
needs code (inspect arguments, consult a file, log, or react to a prompt).

**Scope.**

- Global hooks (`<Reasonix home>/settings.json`) and hooks from installed
  plugin packages apply, except in the read-only observe posture, which loads
  no hooks at all.
- Project hooks (`<root>/.reasonix/settings.json`) run only after the user
  approves them as they stand (`reasonix trust`); until then they are held back
  and a notice says so. Editing the file or a workspace script it names
  withdraws the approval.
- `reasonix review` never runs project hooks, approved or not: the checkout is
  untrusted input, so only global and plugin hooks apply.
- Only tool hooks (`PreToolUse`, `PostToolUse`, `PermissionRequest`),
  `PostLLMCall` and `PreCompact` fire in every agent a session runs, subagents
  included (see the `session_id` table below).

**Limits.**

- A hook cannot force the model's wording: a block reaches the model as a
  refused call, and the model decides what to say or try next.
- There is no per-session scope: hooks come from the settings files and apply
  to every session that loads them.
- Native hooks cannot answer an approval prompt; only a `PreToolUse` block
  refuses a call.

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
can still reduce cache reuse for that turn.

Tool hooks (`PreToolUse`, `PostToolUse`, `PermissionRequest`), `PostLLMCall`
and `PreCompact` fire in every agent a session runs. Each fires under a
`session_id` derived from the parent session's id at the moment the hook runs:

| Agent | Hook `session_id` |
| --- | --- |
| Executor | `<session>` |
| Planner | `<session>:planner` |
| Guardian | `<session>:guardian` |
| `task`, `read_only_task`, `parallel_tasks`, `fleet`, `run_skill`, `read_only_skill` children | `<session>:subagent:<call>` |
| `reasonix review` | a fresh id per run |

`/new` or a branch switch moves every child to the new id along with the parent.

`SubagentStart` and `SubagentStop`:

- bracket a foreground `task` call only; `read_only_task`, `parallel_tasks`,
  `fleet`, skill children and background tasks fire neither;
- carry the call's id as `callId`, so a consumer can pair them;
- `SubagentStop` fires on every end: answer, failure, cancel or refusal;
- neither can block: exit 2 only warns.

## Keyboard shortcuts

Shortcuts are documented by client because users usually look for the keys that
work in the surface they are using. Desktop keeps its Plan toggle, while the CLI
cycles Ask, Auto, and Plan with `Shift+Tab`. Desktop uses `Cmd+Y` on macOS or
`Ctrl+Y` elsewhere for YOLO by default. If YOLO is rebound on Windows/Linux,
`Ctrl+Y` becomes the standard composer redo fallback. Desktop paste stays on the
platform paste key; in the CLI, terminal-native text paste and
application-owned image paste use separate shortcuts.

`[ui].shortcut_layout` is still accepted for old configs, but the shortcut
behavior below is unified across layouts.

For CLI/TUI text input, `[ui].cursor_shape` accepts `underline`, `block`, or
`bar`. The default is `bar`: it remains easy to locate without covering
double-width CJK characters in mixed-language input. Set it to `block` for a
traditional terminal cursor or `underline` for a lower-profile cursor. This
setting does not change desktop or web text fields.

### Desktop GUI

Desktop shortcuts are managed from **Settings → Shortcuts**. Pick a configurable
row, press a new key combination, and Reasonix saves it for the desktop app.
Standard editing shortcuts such as Undo and Redo are shown as locked rows because
the WebView's native text history uses those platform chords. Conflicting
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
| `Cmd++`, `Cmd+-`, `Cmd+0` on macOS; `Ctrl++`, `Ctrl+-`, `Ctrl+0` elsewhere | Increases, decreases, or resets the interface size | Steps through the same sizes as **Settings > Appearance > Size > Interface** (80% to 180%) and saves the same setting. `=` is accepted for the plus key on keyboards that report it that way. |
| `?` | Opens the keyboard shortcuts sheet | The sheet shows the current effective desktop bindings. |

Composer shortcuts:

| Key or control | What it does | Notes |
| --- | --- | --- |
| `Enter` | Sends the current message | IME composition confirmation is left alone. |
| `Shift+Enter` | Inserts a newline | The composer keeps focus. |
| `Shift+Tab` | Toggles Plan on/off | Plan changes the workflow instruction; built-in writers keep the active Ask/Auto/YOLO and Sandbox boundary, while MCP writer/destructive targets stay hard-blocked for the whole planning phase. |
| `Cmd+Z` on macOS, `Ctrl+Z` on Windows/Linux | Undoes the latest composer edit | Native typing stays in the WebView history; Reasonix-managed paste, cut, folded blocks, and structured tokens are restored as complete transactions. |
| `Cmd+Shift+Z` on macOS, `Ctrl+Shift+Z` on Windows/Linux | Redoes the latest composer edit | On Windows/Linux, `Ctrl+Y` is also accepted after the YOLO shortcut has been rebound. |
| `Cmd+Y` / `Ctrl+Y` (default) | Toggles YOLO on/off | Turning YOLO off restores the previous Ask/Auto base when known. The current binding is shown in **Settings → Shortcuts**. |
| `Cmd+V` on macOS, `Ctrl+V` on Windows/Linux | Pastes clipboard content | Clipboard images are attached. Studio references ordinary files by their native path when available, including files selected with Add attachment, pasted, or dropped. Sources without a native path stream into the workspace. |
| Plain `Up` / `Down` at the prompt boundary | Recalls older or newer submitted prompts | Modified arrows and native text navigation stay with the textarea. |
| `Esc` while a turn is running | Cancels the running turn | If the turn has not produced a response yet, the draft is restored. |

Remembered projects have Move up / Move down in their project menu. Open it with the keyboard, navigate with arrows or Home/End, and use Escape to return to the trigger. Moving a project changes sidebar order without changing the launch project.

Studio remembers which local and remote projects you collapse in the sidebar,
including Collapse all / Expand all, when you reopen it in the same browser or
desktop profile. New projects start expanded. These display choices stay on
this device; they do not change sessions or synchronize to another client.

Ordinary Reasonix-managed worktree chats appear under their original project,
without a separate project row for each checkout. The chat header shows Local
checkout or Worktree, with its actual working directory available on hover;
worktree chats also carry a sidebar marker.

Reopening history uses that chat's working directory, while starting a chat
from the project uses the original directory. Ordering, search, paging, pins
and archives span the project.

When reading legacy history for the first time, the sidebar shows discovered
conversations while summaries and turn counts are filled in the background.
Conversations marked as reading their summary can already be opened.

Search indicates incomplete coverage instead of reporting a definitive empty result.
Failed summaries can be retried without blocking other conversations. Summary
caches live in the current version's directory; legacy history is not rewritten.

History search index rebuilds after an upgrade run in the background. Background
reads yield when a conversation is opened or read, then continue after the
foreground operation. Search results may remain incomplete until indexing finishes.

Removing the project closes its idle panes and retains every directory and
transcript. Existing transcripts stay in place and group on restart without migration.
Worktrees added separately outside Reasonix-managed storage remain independent projects.

With a paging-capable server, long conversations initially display the latest 200 messages. Scroll upward or
choose Load earlier messages to read more.

In-conversation find reads earlier pages as needed and indicates incomplete
coverage; clearing the query stops further prefetching. Rewind, compaction and
session changes reload recent content.

Paging affects presentation only, not the full conversation used by the model.

The list holds up to 32 projects; adding another requires removing one first. `serve-workspaces.json` now stores an object with `paths` and `launch`. Older binaries read only arrays and cannot preserve this format: do not share the same state home with an older binary after reordering.

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

#### Studio file attachments

Ordinary local files, including large APKs, use path references whenever the
desktop shell can identify the file. Selecting one external file grants read
access to that file, not its siblings; selecting a folder grants its descendants,
but a symlink inside it does not grant access outside that folder.

Files without a native path use binary streaming into `.reasonix/attachments`.
Streaming bounds memory use. Ordinary uploads have a default 1 GiB safety ceiling
per request to protect disk space; native path references do not copy bytes.

Incomplete files are removed if the transfer fails or is canceled. Removing an uploading
attachment cancels its transfer.

| Entry point | Limit |
| --- | --- |
| Native ordinary-file references | No attachment size cap; the file stays where it is. |
| Ordinary-file binary upload | Default 1 GiB safety ceiling per request, adjustable in user config. Disk space and upstream limits still apply. |
| Image upload | 10 MiB per image, checked before upload and by the kernel. |
| Legacy JSON/Base64 attachment endpoint | 25 MiB general-file limit; retained for older clients. |
| Encrypted cloud relay | 16 MiB per request body; oversized uploads stop at that budget. This is a relay limit, separate from native references and direct uploads. |

In the user's `config.toml`, `[attachments] max_stream_bytes = 1073741824`
sets the ordinary upload ceiling in bytes.

- `0` removes that ceiling; negative values use the default. Project `reasonix.toml` cannot set this value.
- An upload one byte over the limit is refused and its partial file removed.
- The typed `attachment.too_large` response includes `limit_bytes` and `limit_source` (`default` or `user`).
- A malformed user config refuses the upload until repaired. The image and relay limits remain independent.

These limits govern attachment transport. A referenced file is read on demand;
its full bytes do not become model input automatically.

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
| `Enter` | Sends the current message | While a turn is running, non-empty input is durably queued as a follow-up before the composer clears. On an empty composer it jumps back to the newest output and keeps following it. |
| `Ctrl+Enter`, `Ctrl+S`, or `/steer <text>` | Adds guidance to the active turn | The guidance is persisted first; if the turn cannot accept it, it remains a normal follow-up. On an idle composer `Ctrl+S` sends the message as `Enter` does, and `Ctrl+Enter` does nothing. |
| `Shift+Enter`, `Alt+Enter`, or `Ctrl+J` | Inserts a newline | Plain `Enter` is reserved for send/confirm. |
| Plain `Up` / `Down` | Recalls older or newer submitted prompts | Only from the composer's first or last line, so multi-line editing keeps the arrows. While a turn runs (or the queue is paused) and messages are queued, `Up` selects the newest queued message into the composer instead; see the queue keys below. |
| Queue keys, with a message selected | Edit, reorder, delete or retry a queued message | `Up` / `Down` move the selection (past the newest puts your draft back); `Alt+Up` / `Alt+Down` move the message in the queue; `Enter` saves your edit in place; `d` twice deletes it (the first press only asks, and is not typed) and `r` runs a blocked one again (both only while the composer still holds the full message unedited and, in vi command mode, before the vi keys: once you edit, those letters are typed); `Enter` and the edit keys wait until the full message has been read, and a message that cannot be read, or that has already started, ends the selection and brings your draft back; `Esc` lets go. `Space` on an empty composer pauses or resumes the queue. Up to three queued messages are drawn under the working line; `/queue` (below) shows the rest. |
| `PageUp` / `PageDown`, also with `Shift` | Scrolls the transcript | Works regardless of the current chat state. |
| `Ctrl+Home` / `Ctrl+End` | Jumps to the top or bottom of the transcript | Useful after long tool output. |
| `Ctrl+L` or `/cls` | Clears only the visible transcript | The LLM context, session file, tools, memory, and plugins stay loaded. Use `/clear` when you want to discard the conversation context. |
| `Esc` | Backs out of the current action | It cancels a running turn; otherwise it clears non-empty input or leaves `!` shell mode. |
| Double `Esc` on an empty idle composer | Opens the rewind picker | Same entry point as `/rewind`. |
| Transcript text selection | Copies transcript text | Releasing an in-app drag writes through the verified native clipboard path in a local session (`pbcopy` on macOS, the available Wayland/X11 tool on Linux, or the Windows clipboard). SSH falls back to OSC 52 and labels the fallback instead of claiming native success. `Ctrl+C`/`Super+C`/`Meta+C`/`Ctrl+Insert` or right-clicking the active selection copies it again. |
| Right-click with no active selection | Pastes clipboard text locally | In a local session with in-app mouse capture on, Reasonix reads text only and routes it through the normal bracketed-paste handling. Over SSH, use the terminal paste shortcut because the remote process cannot read the local clipboard; `/mouse` restores the terminal's native right-click menu. Right-click with an active selection still copies that selection. |
| `/mouse` | Toggles in-app mouse capture | Off hands the mouse back to your terminal, restoring its native click-drag selection and right-click context menu, at the cost of in-app drag-select, the transcript scrollbar, and wheel-scroll. Set `REASONIX_DISABLE_MOUSE=1` to start every session with it off. Remote (SSH) sessions start with capture off so native selection works out of the box; `REASONIX_DISABLE_MOUSE=0` forces capture on everywhere. |
| `Ctrl+C` | Copies, cancels, clears, or quits | Copies an active transcript selection first. Otherwise it cancels a running turn, clears non-empty input, or quits on a second empty-composer press. A second press while a cancelled turn is still stopping quits too. |
| `Ctrl+D` | Quits the TUI | Immediate quit on an empty idle composer. |
| `Ctrl+Z` | Suspends the TUI to the shell | macOS and Linux; resume it with `fg`. Windows has no job control, so the key does nothing there. |
| `/quit` or `/exit` | Quits the TUI | Runs at once, also while a turn is running. Typing a bare `exit`, `quit` or `:q` sends it to the model as a normal message. |
| Your terminal's text-paste shortcut | Pastes text | Text stays on the terminal's bracketed-paste path (`Cmd+V` on macOS, commonly `Ctrl+Shift+V` on Linux, and the terminal's configured shortcut elsewhere). Reasonix consumes the resulting paste event and never probes for an image first. For terminals that hand `Shift+Insert` to the app instead of pasting, that key reads the clipboard text in a local session and pastes it the same way; over SSH it says to use the terminal's paste shortcut. |
| `Ctrl+G` | Keeps folded pastes as files | A paste of 1,000 characters or 5 lines or more is folded into a `[Pasted text #N · M lines]` label and sent whole. `Ctrl+G` stores every folded paste still in the draft as a file under the workspace's attachments and relabels it `[Pasted file #N · M lines]`; the message then carries a pointer to the file instead of the text. |
| `Ctrl+V` on macOS/Linux; `Alt+V` on Windows | Pastes a clipboard image | Image paste is a separate application action. The footer shows `Pasting image…` while the clipboard is read, then inserts an editable `[image #N]` token at the cursor. |
| `/paste-image` | Pastes a clipboard image | Command form of the same image-only action. |
| A line starting with `!` | Runs a shell command directly | The command runs locally without asking the model. |

`/queue list` shows bounded previews without loading full bodies. Use `/queue
show|edit|delete|move`, `/queue pause|resume`, and `/queue retry|refresh` to
inspect or manage pending work. After crash recovery the inbox is paused, so
review it and run `/queue resume` before dispatch continues. Each item is
limited to 4 MiB; a session accepts at most 64 items and 64 MiB total.

Mode and display shortcuts:

| Key or command | What it does | Notes |
| --- | --- | --- |
| `Shift+Tab` | Cycles Ask → Auto → Plan → Ask | YOLO remains outside this composer-mode cycle; the footer shows the active mode. |
| `Ctrl+Y` | Toggles YOLO on/off | Turning YOLO off restores the previous Ask/Auto base when known. Terminals that forward Command/Super may also send `Cmd+Y`, but `Ctrl+Y` is the reliable terminal shortcut. |
| `--yolo`, `--dangerously-skip-permissions` | Starts chat in YOLO | Same runtime mode as `Ctrl+Y`. |
| `[ui].commandmode = "vi"` | Opts the composer into a vi command mode | User/global only; a project `reasonix.toml` cannot set it, so a cloned repo never rebinds the user's keys. `Esc` enters command mode, a running turn or not; only `Ctrl+C` interrupts. `Ctrl+C` with a non-empty prompt clears it and leaves shell mode, in vi mode and out; in vi mode it first saves the trimmed draft to the prompt history, so `Up` recalls it. Unset or `""` keeps the default, where `Esc` stops a running turn. The footer names the current input mode, `NORMAL` in command mode and `INSERT` otherwise. |
| `auto_submit = true` | Commits a multi-question ask once its last question is answered | Top-level config; user/global only, so a project `reasonix.toml` cannot set it; default `false`. The ask card drops its Submit tab: answering a question moves on to the next unanswered one, answering the last with nothing left unanswered commits the batch at once, and a skipped question pulls the panel back to it. Unset keeps the Submit tab and its explicit `Enter`. |
| `/preset [balanced|delivery]` | Shows or switches the current session's execution setting (执行设定) | `/work-mode` and `/profile` are compatibility aliases; the retired `light` and `economy` select `balanced`. Switching updates the execution setting in place without rebuilding the controller; blocked while a turn, approval, or background job is active. |
| `/theme [auto|light|dark|style]` | Shows or switches the CLI theme | Bare `/theme` lists background modes and named accent palettes. The choice is saved to the user config; `REASONIX_THEME` and `REASONIX_THEME_STYLE` can override it for one run. |
| `Ctrl+O` | Toggles verbose reasoning display | Also available through `/verbose`. |
| `Ctrl+B` | Expands or collapses long shell output | Long shell-output hint lines can also be clicked in the transcript; text selection is handled in-app while the full-screen TUI has mouse reporting enabled. |
| `/goal <objective>`, `/goal status`, `/goal pause`, `/goal resume`, `/goal clear` | Starts, checks, pauses, resumes, or clears Goal | A Goal is unbounded unless `[agent].goal_token_budget` is set. |
| `/migrate`, `/migrate --from <legacy-dir>` | Retries legacy migration or imports sessions from a chosen v0.x source | Use `--from` for custom Windows v0.52 install/data directories; it imports sessions only. See [Configuration paths](./CONFIG_PATHS.md). |

Picker and approval shortcuts:

| Context | Keys | What they do |
| --- | --- | --- |
| Slash or `@` completion | `Up` / `Down`, `Ctrl+P` / `Ctrl+N`, `Tab` / `Enter`, `Esc` | Move, accept, or close the completion menu. While a turn is running, `Esc` also stops it, except in vi mode. |
| Tool approval prompt | `Up`/`Down` or `j`/`k`/`Tab`, `Enter`, `y`, `a`, `p`, `n`, number keys, `Esc`, `Ctrl+C` | Move, answer the highlighted row (the cursor starts on allow once), allow once, allow for this session, always allow, deny, pick a row by its position, deny, or cancel the turn. `a` and `p` are offered only when the call can be allowed that way, so the numbers follow the rows shown. In vi mode `Esc` does not answer; a row key or `Ctrl+C` does. |
| Plan approval prompt | `Up`/`Down` or `j`/`k`/`Tab`, `Enter`, `y`, `n`, `x`, number keys, `Esc` | Move, answer the highlighted row, start executing the plan, revise it, leave plan mode, or pick a row by its position. `Esc` asks for a revision, except in vi mode. |
| Ask question card | `Up`/`Down` or `j`/`k`, `Left`/`Right` or `h`/`l`, `Space`, `Enter`, `1`-`9`, `Esc`, `Ctrl+C` | Navigate answers/tabs, toggle multi-select answers, submit/activate, pick numbered options, dismiss, or cancel the turn. |
| Rewind picker | `Up`/`Down` or `j`/`k`, `Enter`, `b`, `c`, `d`, `s`, `u`, `y`, `Esc` | Choose a turn, apply both/conversation/code/summarize actions, confirm with `Enter` or `y` a file restore the checkpoint covers only partly, or go back/close. |
| Model, provider, or resume picker | `Up`/`Down` or `Ctrl+P`/`Ctrl+N`; `j`/`k` while search is empty; type to filter; `Enter`; `Esc` | Search, select an item, or close the picker. Once search input starts, `j`/`k` become query text. `/provider` opens that provider's model list. In the resume picker `Ctrl+D` twice deletes the highlighted saved session: the first press only asks, and any other key takes it back. |
| `/mcp` panel | `Up`/`Down` or `j`/`k`, `Enter`/`Right`/`l`, `Esc`/`Ctrl+C`/`Left`/`h`, `q`, `Space`, `n`, `d`, `a`, `u`, `r` | Move; show a server's tools; go back from them or close; close; enable or disable a server (enabling one the repository declares asks `y` first); add a server; remove one (asks `y` first); sign in to a server that answered 401; grant the extra access a server asks for; reconnect a failed server. To add, type its command, URL or config and press `Enter`, then on the review install it with `y` (user), `l` (local) or `p` (project), or leave with `n`/`Esc`; keys are ignored for a moment after the review opens, and a server too long to show whole cannot be installed from it. |
| `/clear` confirmation | Arrow keys or `j`/`k` / `Tab`, `Enter`, `y`, `n`, `Esc` / `Ctrl+C` | Toggle Clear/Cancel, confirm clear, or cancel. |

Mode meanings:

| Mode | Meaning |
| --- | --- |
| Ask | Prompts for fallback writer approvals. |
| Auto | Auto-allows fallback approvals; explicit `ask` / `deny` rules still apply. |
| YOLO | Skips ordinary tool approval prompts; `deny`, user `ask` questions, and plan approval prompts still wait. |
| Plan | Directs the model to plan first — a plan-first workflow, not an all-tools read-only mode. Built-in writers still follow the active Ask/Auto/YOLO rules and Sandbox; installed MCP writers, destructive targets, and readers from unauthorized servers are hard-blocked for the whole planning phase (approval cannot release them; they return once Plan exits), and explicit phase-only tools such as `complete_step` wait until approval. |
| Goal | Pursues a saved objective until complete, blocked, or cleared. |

## Permissions & sandbox

Permissions gate each tool call: `deny` > `ask` > `allow` > fallback. Bash and
file mutation tools require approval by default; read-only tools generally do
not. Approvals are stored and matched as permission rules, not button labels:
for example `Bash(npm run build)`, `Bash(npm run test:*)`, and `Edit(docs/**)`.
`reasonix` can grant Bash as an exact command or as a conservative command
prefix (for example `Bash(go test:*)`), while file-editing tools share session
edit grants and persist path-scoped rules such as `Edit(src/app.go)`.
Parameter/arithmetic expansions, assignments, heredocs, file redirects, and globs cannot reuse a bare
Bash, prefix, or glob allow; a user-approved reusable choice saves the whole
command as `Bash=<literal>`. They still follow normal fallback, so Auto executes
them without an extra prompt. Command/process substitution, a dynamic command
name, `eval`, `source`, shell `-c`, inline runtime code, and unparseable forms
require a human in interactive Ask/Auto. Headless Ask/Auto/DontAsk reject that
nested/indirect class unless an exact literal exists; YOLO may bypass it.
Advanced users can set `[permissions] allow_dynamic_bash = true` to let an
Allow fallback, including Auto, cover that class; explicit `ask` and `deny`
rules still take precedence.
Because a headless run has no approval UI, the default Ask posture also fails
closed on ordinary writer fallback and explicit ask rules. Use
`reasonix run --auto ...`, `-y`, or `--permission-mode auto` when unattended
automation should allow ordinary writer fallback; configured `ask` and `deny`
rules always remain authoritative.

Text from outside is a separate question. `[permissions] ask_on_external_origin
= true` (user file only, off by default) makes Auto and YOLO ask first when:

- the call is not a read;
- one of its string arguments, at least 12 bytes, is copied byte for byte from a
  web page, MCP tool result or other external result the task read;
- you did not write that string yourself.

The question is fresh. Auto, YOLO and a remembered answer do not settle it. With
nobody to ask, such as a headless YOLO run, the call is refused with the origin
named instead of being executed.

Not asked about: a call your own `allow` rule covers, an installed MCP server,
and any call that is not a verbatim copy (re-encoded, paraphrased, built by a
script the model wrote first). A resumed session starts with nothing recorded.

Ask is not read-only: after approval, a writer can still run. Permissions decide
whether to allow or prompt; the Sandbox is the enforced capability boundary.
The sandbox remains a second boundary after authorization; confinement cannot
make ambiguous command parsing safe to authorize automatically.

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

**Device nodes (Linux).** The bubblewrap sandbox shows a private `/dev`, so host
devices such as the GPU are not there. `[sandbox] allow_devices` binds some in:

- It lists globs under `/dev` (for example `/dev/nvidia*`, `/dev/dri/*`).
- Only existing character or block device nodes qualify; symlinks, directories
  and files are skipped, and at most 64 are bound.
- It is empty by default.
- It is read from your user config only. A project's `reasonix.toml` cannot set
  it, because a device is a host capability, not a workspace file.

**Git metadata is host-protected.** Inside the bash sandbox, the Git
configuration and hooks of the workspace repository stay read-only, because the
host's own git reads them.

The repository is the one git itself discovers from each writable root. A
`.git` file is followed to the gitdir it names the way git resolves it,
relative to the file with symlinks followed, so the protection lands on what
git will read. Protected:

- `.git` itself, the gitdir, the common dir and every symlink on the way to
  them: none can be removed, renamed or replaced by a symlink.
- `config`, `config.worktree`, `commondir` and `hooks/` of the gitdir and of
  the common dir.
- `config`, `config.worktree` and `commondir` of each existing `worktrees/*`
  entry, and `config` and `config.worktree` of entries created later.
- `config`, `config.worktree`, `commondir` and `hooks/` of each submodule
  gitdir under `modules/`, existing or created later.

Everything else under `.git` (objects, refs, index, logs, lock files) stays
writable, so add, commit, branch, checkout, merge, rebase, stash, tag and
worktree creation keep working. These operations change:

| Operation | In the sandbox |
| --- | --- |
| `git config` without `--global`, `git remote add` / `set-url`, `git branch -m`, `git submodule init`, `git submodule update --init`, `git sparse-checkout init`, `git maintenance register`, `git init` in an existing repository | Fails |
| Installing a hook into `.git/hooks` | Fails |
| `git worktree remove` / `prune` of a linked worktree that existed before the command | Fails; one added in the same command can be removed |
| Cloning a new submodule (`git submodule add`, `git submodule update` for one not yet cloned) | Fails on macOS; on Linux the clone lands but its config entry does not |
| `git branch --set-upstream-to`, `git checkout --track`, `git push -u` | Reports the refused write but exits 0; no upstream is recorded |

To add or initialise submodules, run it outside the sandbox, in a terminal; updating submodules that are
already cloned works inside it.

When a command's output names one of these paths, the bash result says so with
`sandbox.git_metadata_protected`, also when git exited 0.

A protected file that already has another hard link refuses every confined
command with `sandbox.git_metadata_linked`, because a write through the other
name would reach it; the user removes that link outside the sandbox.

Limits:

- On Linux, bubblewrap can only mount over paths that exist and cannot pin a
  symlink. Creating an absent `commondir`, `config.worktree` or hooks
  directory, or swapping a symlink on the `gitdir:` path, is not stopped there.
- Closing that on Linux is the host's side: its own git has to pin its git and
  common directories instead of rediscovering them.
- Existing worktree and submodule gitdirs get exact rules, up to 128 each on
  macOS and 512 on Linux.
- On macOS, patterns cover the rest and gitdirs created later. They skip
  `refs/` and `logs/`, so a branch or tag named `config` or `hooks` stays
  writable, but a new submodule named `hooks` is protected whole.
- On macOS with more than 128 worktrees, `git worktree add` fails; past 128
  submodules each command starts about 0.1 s slower.
- On Linux, past 512 gitdirs the whole `worktrees/` or `modules/` directory is
  mounted read-only, and a command can cause that by planting `HEAD` files.
  A command can likewise plant a submodule gitdir with a hard-linked config,
  after which every confined command is refused until the user removes it.
- A repository created by a sandboxed command is protected from the next
  command on.
- A `.git` made unrecognisable to git (for example a corrupted `HEAD`) sends
  git's discovery further up.
- A repository nested in the workspace that is not a submodule gitdir under
  `modules/` is not protected, including one a command creates and records as
  a gitlink.
- The host can run such a repository's configuration through its gitlink
  unless the host's own git excludes it.
- Hooks that `core.hooksPath` points outside `.git`, and files `include.path`
  names, are ordinary workspace files.
- Windows has no Bash sandbox, so none of this is enforced there.

**Session-private temporary directory.** Within one logical chat session, Bash
commands share a private temporary directory so consecutive calls can exchange
files through `$TMPDIR` (and, on Linux under bubblewrap, through literal
`/tmp`). No user setup is required: Reasonix automatically exports `TMPDIR`,
`TMP`, and `TEMP` for Bash and client-owned ACP terminals. The directory is
created lazily, is never the host public temporary root, and is rotated on
`/new`, `/clear`, resume of another session, and branch switches.
Model/settings hot rebuilds keep the same directory. Temporary files are not
durable storage: resume across process restarts does not restore them, and
scripts that need long-lived data should write into the workspace or a
user-specified path.

Reasonix-generated and project scripts should use the standard temporary
environment variables rather than hard-coding `/tmp`; users should not set
these variables themselves. For example:

```sh
tmp_file="${TMPDIR:?}/result.json"
```

```powershell
$tmpFile = Join-Path $env:TEMP "result.json"
```

| Platform | `$TMPDIR` / `$TMP` / `$TEMP` | Literal `/tmp` |
| --- | --- | --- |
| Linux + bubblewrap | Virtual `/tmp` (bound to the private dir) | Shared for the session (not a fresh empty tmpfs each call) |
| macOS Seatbelt | Host path of the private dir (allowed by policy) | Host macOS temporary directory; scripts should use `$TMPDIR` |
| Windows (no OS Bash sandbox) | Host path of the private dir | Not promised to match (e.g. Git Bash `/tmp`) |

Independent sandboxes such as MCP servers keep their own isolation and do not
inherit the chat session's temporary directory. An approved sandbox-escape
command still receives the private temp environment variables, but on Linux its
literal `/tmp` is no longer mapped by bubblewrap.

**Windows note:** the OS-level Bash sandbox on Windows is off unless the user
turns on `[sandbox] windows_integrity = true` (see "On Windows" below). While it
is off the effective mode is `off`; even an older config containing
`bash = "enforce"` resolves to `off`, `reasonix doctor` flags the ignored value,
and the desktop selector is read-only. Bash commands then run unconfined,
while the dedicated file tools still enforce `workspace_root`, `allow_write`,
and `forbid_read` in process. Saved credential variables are still removed from
the child environment, but an approved unconfined shell runs as the user and is
not a security boundary for other user-readable files.

When no OS sandbox backend is available, `bash = "enforce"` refuses bash
execution instead of running unconfined. Install the platform sandbox backend
(bubblewrap/`bwrap` on Linux, `sandbox-exec` on macOS) or set
`[sandbox] bash = "off"` to explicitly restore the pre-1.16 unconfined shell
behavior.

On Windows the sandbox is off unless you turn on
`[sandbox] windows_integrity = true` (Settings > Sandbox; user file only).

### On Windows

With `windows_integrity` on, a bash command runs under a write-restricted token.

- The first session grants a synthetic Windows identity write access on the
  workspace, your `allow_write` folders and the data caches of your toolchains.
- The caches are Go build and module, npm, pip, cargo registry and NuGet packages.
  A folder that holds programs you run (`~/go/bin`, `.cargo/bin`) is never granted.
- The grant is a permission entry on those folders, recorded in the Reasonix
  state folder. It gives nobody else access and is inert if the sandbox is turned off.
- `reasonix sandbox list` shows what is granted; `reasonix sandbox revoke` takes it back.

What it bounds, measured on Windows 11 and for every child process:

- Creating, appending to and overwriting files outside the granted folders.
- `mkdir`, registry writes under your user (HKCU), and global git configuration.
- Writes through a junction or symbolic link that points out of a granted folder.
- Backup-semantics writes (`robocopy /B`): the token carries no privileges.
- Creating a scheduled task, creating a service and writing HKLM from the command
  were refused when measured.

What it does not bound:

- **Deleting or renaming away** an existing file outside the granted folders.
  Windows does not apply the restriction to those two. The delete guard and the
  approval layer remain the control for deletes.
- **Reading** files, and **the network**.
- **Host authorities**: the Windows named pipes of ssh-agent and docker, and COM
  and RPC services such as the task scheduler, are reachable.
- Writes anywhere that Everyone or your logon session can already write: both are
  in the restricting set, so a folder open to them is open to the command.
- The toolchain caches that are granted are writable by the command, so a
  poisoned cache can reach your later unsandboxed builds. macOS and Linux have the
  same exposure for their caches.

Other facts:

- The command runs with every privilege except the traverse check removed, and in
  a job that cannot reach the other windows, clipboard or display settings of your session.
- Only the three standard handles cross into the command.
- A command cannot start its own launcher: the launcher refuses to run under a
  restricted token, and grants only session temp folders the host made.
- It needs an interactive desktop session. In an SSH or service session `enforce`
  refuses to run commands (`sandbox.win_no_logon_sid`) instead of running them unconfined.
- Not measured: standard (non-admin) accounts, other Windows versions, ReFS and
  network drives. Linux and macOS are unchanged and bound every kind of write.

### In a dev container or Docker

Bubblewrap has to create user and mount namespaces (and a network namespace for
a closed network), which a default container blocks. Measured on Docker Desktop
(Linux VM, Debian 12, bubblewrap 0.8.0), by launching a confined command under
`enforce` in each configuration:

| Container configuration | Sandbox | Reported cause |
|---|---|---|
| bubblewrap not installed | unavailable | `sandbox.no_bubblewrap` |
| default `docker run`; seccomp unconfined only; `SYS_ADMIN` only; seccomp and AppArmor unconfined without `SYS_ADMIN` | unavailable | `sandbox.bwrap_namespace_denied` |
| `SYS_ADMIN` with seccomp and AppArmor unconfined | works with the network open; a closed network is unavailable | `sandbox.bwrap_network_denied` for the closed network |
| the above plus `NET_ADMIN`, or `--privileged` | works, network open or closed | none |

- Both `CAP_SYS_ADMIN` and an unconfined seccomp profile are needed; either alone is not enough.
- Reasonix does not look for a container. The message comes from what bubblewrap
  could and could not do, so a container granted those privileges gets its
  sandbox, and one that is not is told which privilege is missing.
- Granting them widens the container itself. The other choice is
  `[sandbox] bash = "off"`; the container is then the only boundary, and what
  you mount writable (a credentials file in a bind-mounted workspace included)
  is writable by every command.
- Only the Linux sandbox was measured. A Dev Containers or Codespaces session,
  rootless Docker and SELinux hosts were not run.

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
`AGENTS.md` is missing, shadowed, disabled, or fails to start. Full flag
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

When a cached resource capability is available before its server connects,
`read_mcp_resource` waits within the normal startup budget. Cancelling that read
does not cancel the session's background connection attempt.

Reasonix is an MCP client. A `[[plugins]]` entry's `type` selects the transport:
`stdio` (default) launches a local subprocess (`command`/`args`/`env`); `http`
(Streamable HTTP) connects to a remote `url` with optional static `headers`
(`${VAR}` / `${VAR:-default}` expanded from the environment, so tokens stay out
of the file); `sse` connects to servers that still use the legacy persistent
GET + announced POST endpoint transport.

`${REASONIX_WORKSPACE_ROOT}` (or `${CLAUDE_PROJECT_DIR}`) expands to the current
workspace's absolute path, so one global entry can name the project:

```toml
headers = { IJ_MCP_SERVER_PROJECT_PATH = "${REASONIX_WORKSPACE_ROOT}" }
```

For a remote HTTP server without a static `Authorization` header, an
authentication challenge (HTTP 401 with a `Bearer` challenge) is reported as
needing sign-in. Run `reasonix mcp auth <name>` in the CLI. Reasonix performs OAuth metadata discovery, dynamic
client registration, PKCE S256 authorization, and refresh-token
rotation. Discovery and token requests use the same Reasonix network-proxy
settings as the MCP connection.

Before a browser opens, Reasonix names the authorization host the server chose and asks. "Always" is remembered for that server and host only, beside its OAuth state, and cleared with it.

When a signed-in server refuses a call (HTTP 403, `insufficient_scope`) because the access you granted is too narrow:

- The model is told to stop and say so, and you get a notice.
- Only you can grant more: click **Grant more access** on the server's row, or run `reasonix mcp auth <name> --upgrade`.
- The scope names are the server's own words, shown as written.
- You are asked every time, in every approval mode, and the answer is never remembered.
- A server that keeps asking after being granted is refused for good.

Tools whose definitions change after you approved them are held back until you accept them (`reasonix mcp trust <name>`). To take one server's changes without being asked, run it with `--follow` (`--unfollow` undoes it); each change is still announced.

OAuth client and token state is kept outside the workspace in the server's
private Reasonix state directory, written with mode `0600`, and bound to the
full configured resource URL. An explicit static `Authorization` header always
takes precedence. **Clear authentication** removes only Reasonix's local OAuth
state; it does not sign out the third-party browser session. Reasonix opens the
browser only after an explicit sign-in action, never automatically from a
background tool-call failure. Removing the MCP server also removes its local
OAuth state unless a lower-priority declaration for the same resource becomes
effective.

Browse the official MCP Registry from **Settings → MCP servers → Browse
registry**, or use `reasonix mcp browse [query]` and
`reasonix mcp install <registry-name>`. Registry access is explicit and never
runs during startup. Entries that need secrets or required arguments are shown
as manual setup instead of being installed with an incomplete configuration;
query-specific cached results remain available during a registry outage.

The normal setup path is intentionally one step. Use Desktop's **Add and
connect**, `n` in the terminal `/mcp` panel, or ask Reasonix to install a
package or URL. These explicit installs are saved to the user-global
`config.toml` (`p` in the panel: `reasonix.toml`) and are also
authorization: the server connects in the current session, and no second trust
step appears now or on the next startup. Servers declared by the current
project's `reasonix.toml` or `.mcp.json` remain in that project and are trusted
without a separate launch confirmation. Explicit deny rules still win. The
server's calls run
directly, including tools that declare `destructiveHint`. The dedicated Planner
still refuses destructive tools, and strict read-only sub-agents still expose
only hinted non-destructive readers.

MCP names are resolved once per workspace. Project declarations override
same-name global installs; inside a project, `reasonix.toml` overrides
`.mcp.json`. Editing updates the effective declaration in its original file,
and removing a higher-priority declaration reveals the next one instead of
deleting every same-name entry.

stdio servers keep one process for initialize, reads, and writes, so stateful
servers such as browsers retain sessions and open pages. Because an OS sandbox
is fixed when a process starts, this shared process uses the server's normal
process sandbox for every call; `readOnlyHint` and read-only sub-agent filtering
are dispatch policy, not a second per-call process sandbox.

Tools surface to the model as `mcp__<server>__<tool>`. A tool declaring MCP's
`readOnlyHint: true` joins parallel dispatch and the strict read-only tool
surfaces. Installing a server or declaring it in project configuration
authorizes the dedicated Planner to use all of its non-destructive
tools without another per-tool setting; strict read-only research sub-agents
receive only hinted non-destructive readers. Tools without the hint remain
write-capable for scheduling and mutation accounting. While planning, built-in
writers keep the ordinary permission posture. The dedicated Planner permits
authorized non-destructive MCP (including opaque writers) but hard-blocks
destructive or unauthorized targets; a single-model Plan without that dedicated
Planner keeps the older writer/destructive block until Plan exits.

Installing an MCP server is the authorization decision. After installation, all
of its tools run directly without a second server-level, per-tool, writer, or
destructive approval setting. Explicit global deny rules still win. The host
keeps `readOnlyHint` and `destructiveHint` internally for parallel scheduling,
Plan restrictions, strict read-only sub-agents, and cached-to-live safety
reclassification; these hints do not add user configuration.
Reasonix deliberately trusts an installed server to describe those hints
honestly. Planner/read-only filtering is therefore a workflow boundary for
trusted servers, not containment against a malicious MCP server; explicit deny
rules and the process sandbox remain host-controlled boundaries.

The retired `trusted_read_only_tools`, `default_tools_approval_mode`,
`tools.<raw>.approval_mode`, and `approvals_reviewer` fields are ignored when
loading older files and removed the next time Reasonix saves that MCP entry.

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
# startup_timeout_seconds = 60    # optional initialize + tools/list cap
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

An interactive caller waits only briefly for a cold server. If that wait ends,
the shared startup continues in the background rather than being killed and
restarted; retry the tool after it comes online. `mcp_startup_timeout_seconds`
(default `30`) bounds the full launch, authorization, initialize, and
`tools/list` sequence. `mcp_call_timeout_seconds` applies only after the server
is connected. Either value can be overridden per server.

Set `disabled_tools = ["write_file", "delete_file"]` in a server's `[[plugins]]` entry to hide selected tools. `.mcp.json` accepts the same extension.

- Names match `tools/list` exactly, before namespacing or prefix stripping. An empty list leaves every tool enabled.
- Hidden tools stay out of the registry, capability catalog, `/mcp` tool lists and schema token counts, including cached schemas. Config saves preserve the list.
- Restart the session or disconnect and reconnect the server after editing it. An active session keeps its existing schema prefix.
- This controls the tools Reasonix exposes; it does not sandbox the server process.

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

## Custom instructions / 提示词怎么注入

Choose the scope before adding a prompt.

### One task / 单次任务

Type the instructions in chat. For a reusable prompt you invoke explicitly,
use a [custom slash command](#slash-commands).

直接在聊天框输入要求。需要反复手动调用的提示词可写成[自定义斜杠命令](#slash-commands)。

### Repository rules / 仓库规则

- Create `AGENTS.md`, or use the repository's existing `REASONIX.md` / `CLAUDE.md`,
  at the workspace root. For example: `- Run the project tests before reporting completion.`
- Use `AGENTS.local.md` for personal rules in that directory, or an instruction
  file in your Reasonix home for user-global rules.
- See [standing-instruction resolution](./SESSION_MEMORY_RETRIEVAL.md#instruction-resolution)
  for hierarchy, imports, and `/memory instructions` to inspect what loaded.

- 在工作区根目录创建 `AGENTS.md`，或沿用仓库已有的 `REASONIX.md` / `CLAUDE.md`。
  例如添加 `- 完成前运行项目测试。`。
- 当前目录的个人规则可放在 `AGENTS.local.md`；跨项目的用户规则放在 Reasonix 主目录中的指令文件。
- 层级、导入及查看已加载内容的 `/memory instructions` 命令见
  [常驻指令文档](./SESSION_MEMORY_RETRIEVAL.md#instruction-resolution)。

### User-wide persona / 用户级人设

1. In Studio, open **Settings → Persona → System persona**.
2. Fill **Start of the system prompt** and/or **Additional instructions**, then **Save**.
3. Start a new session, or wait for the existing session's next rebuild
   (for example, a model switch). Saving does **not** change the running session's prompt.

These settings supplement built-in guidance; they do not replace it. They are
stored in your user configuration, not a project's `reasonix.toml`.
See [System persona](./PERSONA.md) for configuration fields and limits.

1. 在 Studio 打开**设置 → 人设 → 系统人设**。
2. 填写**系统提示词开头**和/或**补充要求**，点击**保存**。
3. 新建会话，或等现有会话下一次重建（例如切换模型）后生效。
   **保存不会改变正在运行的会话提示词**。

这些设置补充而非替换内置指令，写入用户配置，不由项目 `reasonix.toml` 设置。
配置字段及限制见[系统人设](./PERSONA.md)。

## Slash commands

In an interactive `reasonix` session, built-in commands (`/compact`, `/context`, `/new`, `/clear`, `/rewind`,
`/tree`, `/branch`, `/switch`, `/todo`, `/model`, `/work-mode`, `/mcp`, `/skills`, `/hooks`,
`/memory`, `/goal`, `/output-style`, `/sandbox`, `/language`,
`/reasoning-language`, `/help`, `/version`) run
locally — `/help` lists them all. Built-in **skills** such as `/init`,
`/explore`, `/test`, and `/reasonix-guide` also appear in the slash menu and via
`run_skill` (bodies load on demand; only the index line is cache-stable). Use
`/reasonix-guide` when you need config or capability troubleshooting; it points
at `reasonix doctor capabilities` (see
[Capability diagnostics](./CAPABILITY_DIAGNOSTICS.md)). `/new` starts a new
session while saving the previous transcript for history/resume; `/clear` asks
for confirmation, then discards the current context without saving it. `/tree`
shows saved conversation branches, `/branch [name]` branches from the current
conversation tip, and `/switch <id|name>` loads another branch. **Custom commands** are
Markdown files under `.reasonix/commands/` (project) or `~/.reasonix/commands/`
(user) — `review.md` becomes `/review`, a subdirectory namespaces it
(`git/commit.md` → `/git:commit`). The body is a prompt template; invoking the
command sends it as a turn.

In Studio, a finished reply has a "Branch into a new conversation" button. It copies the conversation up to that reply into a new session titled "<title> · branch" and leaves the original as it was. It needs a live turn boundary, so a session restored after a restart does not offer it.

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

Context Engine v2 separates two intentionally different layers:

- **Standing instructions** come from hierarchical `REASONIX.md`, `AGENTS.md`,
  and `CLAUDE.md` files. Put rules here when they must be present on every
  relevant turn. User-global files load first, then workspace and deeper target
  directories; within one directory, `.local.md` variants win.
- **Rules** are Markdown files in `.reasonix/rules/`, `.claude/rules/` (project)
  and `<Reasonix home>/rules/` (user), subdirectories included.
  - A rule with no `paths:` frontmatter is delivered like the standing instructions.
  - A rule with `paths: ["src/**/*.ts"]` (a list or a comma-separated string) is
    delivered on the next turn after the session reads or edits a matching file,
    and is retracted if it stops applying.
  - Rules load user first, then project, so a later rule is the more specific one.
  - A file is limited to 32 KB and each of the two layers to 64 KB. What exceeds a
    limit is skipped and shown in capability diagnostics. Symlinks are not followed.
  - Neither layer touches the cache-stable prefix.
- **Background memory** stores one durable fact per Markdown file. Each fact has
  an immutable ID, monotonic revision, timestamps, independent `type`
  (`user`, `feedback`, `project`, `reference`) and `scope` (`project`,
  `global`), plus freshness metadata. Facts may be stale, so they never outrank
  the current request or standing instructions.

Before a turn that changed files may finish, Reasonix asks whether a check ran
after the last write. It recognises the common runners on its own — `go test`,
`pytest`, `npm test`, `make check`, `cargo test`, `tsc --noEmit` and the like —
but it deliberately cannot tell `python deploy.py` from `python run_tests.py`.
A project that runs its checks through its own scripts needs no setup for this:
the agent names which command was the check when it signs the step off
(`complete_step` evidence with `kind: verification` and the command as run), and
the host proves from its own receipts that the command ran after the write and
passed. The agent decides only which of the commands it ran was the check;
whether it ran, exited zero, and did so after the write are never its to claim.

When a command ran that the host could not read as a check, and the host saw it
leave the tree as it found it, the turn ends with a warning: the miss is its own
blind spot, not evidence the work went unverified.

Two cases still fail. A turn that changed files and ran nothing at all, and one
whose unreadable command changed something — watched or merely unestablished.
The way out of the second is the citation above, a check the host recognises, or
`conclude_blocked`.

Declaring checks is the stronger, optional form. Name them in any
standing-instruction file, under exactly this heading:

```markdown
## Reasonix host checks

- verify: python -m pytest tests/
- verify: python scripts/screening.py --self-check
```

Every declared check must then have run since the latest write for the turn to
end, and the built-in classifier stops having a say — the project has defined
what verification means there. Entries are `- verify: <command>` (or
`* verify:`); ordinary instructions elsewhere in the file stay guidance and
never become gates.

The same section names the paths whose changes deserve the hardest review:

```markdown
## Reasonix host checks

- sensitive: src/auth/**
- sensitive: infra/network.tf
```

A change under a declared path requires `review` plus `security_review` before
the turn can end; everything else is scored by structure alone (an opaque write
the host cannot name a path for, or ten or more paths at once). Sensitivity is
declared rather than inferred, because a path's spelling cannot tell
`internal/auth` from `session_write_authority.go`.

Reasonix automatically recalls a small set of relevant facts before each real
user turn. It searches the raw user message, suppresses generic requests such as
"continue", prefers project facts over equivalent global fallbacks, down-ranks
stale facts, and appends at most four facts / 2,400 characters to the user turn.
This dynamic suffix does not rewrite the cache-stable system prompt or tool
schemas. Use `/memory recall` to see the selected IDs, scores, reasons,
freshness, budget, and suppression decision.

New, bounded, non-sensitive project/reference facts can be created
automatically with no setup or approval click. Global facts, user preferences,
feedback, updates, duplicates, sensitive/oversized content, and every `forget`
still require explicit confirmation. The storage layer makes the automatic
grant create-only, so it cannot overwrite a fact that appears concurrently.
A top-level headless controller may use the same one-shot low-risk create path;
sub-agents and headless surfaces without the owning scoped controller fail closed.

`forget` archives rather than permanently deletes. Every update snapshots the
previous revision; restore and archive recovery always create a higher revision
instead of overwriting history:

```text
/memory instructions
/memory recall
/memory revisions <id-or-name>
/memory restore <id-or-name> <revision>
/memory archived
/memory recover <archive-path>
```

The desktop Context Center shows the same provenance, conflicts, revision
history, recall trace, and recovery actions. Opening its Suggestions tab scans
recent local user turns automatically; candidates are deduplicated against both
memory scopes and instruction bodies, but nothing is saved until the user
accepts it. Remote workspaces never fall back to local desktop memory or
sessions.

Legacy facts are upgraded in place with deterministic IDs and revision 1;
missing scope is inferred from the containing directory. Migration is
idempotent, old clients retain safe routing, and legacy Memory v5 transcripts
remain readable. For the complete behavior and privacy/cache contract, see
[`Context Engine v2`](SESSION_MEMORY_RETRIEVAL.md).

```markdown
---
description: Review the staged diff
argument-hint: [focus-area]
---
Review the staged diff. Focus on $ARGUMENTS, list bugs with file:line.
```

`$ARGUMENTS` expands to all space-separated args, `$1`…`$N` to positional ones.
MCP prompts also appear here as `/mcp__<server>__<prompt>`.

## Embedded documentation retrieval

Reasonix bundles the Markdown files from `docs/` into each CLI and Desktop
build. The read-only `docs` tool searches that exact offline corpus with local
BM25 retrieval and can read a complete matching section with source provenance.
The agent should use the tool before web search or assumptions when a question
concerns Reasonix configuration, CLI/Desktop behavior, permissions, MCP, memory,
recovery, providers, or maintainer workflows.

No setup, network connection, vector database, or embedding service is needed.
Search results prefer the query language while retaining explicit `en`,
`zh-CN`, audience, and catalog filters. Balanced and Delivery profiles expose the
tool directly; Economy connects the `docs` source on demand. Every result reports
the product version, immutable source revision, and corpus SHA-256 digest. The
embedded manifest is bound to the candidate's `docs/*.md` and build identity, so
an online page cannot silently replace version-matched local guidance.

Use `/docs` to inspect the bundled corpus identity and usage examples without
calling a model. Use `/docs <question>` (for example,
`/docs how to configure an MCP server`) to make Reasonix search the corpus
locally first and then pass the matched sections to the currently configured AI
for a sourced answer. This command path does not depend on the model deciding to call
the `docs` tool, while ordinary natural-language questions may still use the
tool automatically. Existing custom commands and compatible plugin or skill
aliases keep ownership of `/docs`; when that happens, CLI and Desktop normally
expose the built-in corpus as `/reasonix:docs` instead. If that qualified name is
also already owned, Reasonix selects the next free `reasonix:`-qualified fallback
without displacing it. A remote Desktop uses the host's resolved command catalog,
so the displayed entry always matches what that host will execute.

Pull requests that change user-visible CLI, Desktop, configuration, provider,
permission, or tool behavior must declare whether embedded documentation was
updated. When no documentation change is needed, the declaration must explain
why the existing version-matched guidance remains correct.

## Goal

Goal is the unified runtime for long-running objectives. Reasonix keeps working
until the goal is complete, blocked, paused, or cleared. Ordinary chat never
changes collaboration mode implicitly; choose Goal in the composer or use
`/goal` to start a long-running objective.

The desktop plan fold and the terminal footer show a goal's state, turns, requests, tokens and active work time, and name the stop cause when it is not running.

Goal has no default model-round, cross-Run turn, wall-clock, or numeric
no-progress limit. It continues until completion, a genuine user/external
blocker, manual stop/pause, an unrecoverable external error, or an explicit
user-selected budget. To place an optional ceiling on an unattended loop, set:

```toml
[agent]
goal_token_budget = 20000000
```

The default is `0` (off). Reaching a positive token budget produces one summary
and a resumable `budget_spend` pause. `/goal resume` grants a fresh configured
slice while cumulative Goal statistics remain intact. Explicit positive
`max_steps`, task time, and task cost budgets remain available as well.
Progress is goal-scoped and novelty based:
new read/search results, mutations, verification, todo/signoff changes, and
reviews advance the goal; an exact tool/argument/result repeat does not.
Cumulative turns, tokens, real provider requests, and active work time are
tracked and shown as statistics; a token limit appears only when explicitly
configured. A paused goal keeps its todos, Delivery
checkpoint, and runtime history — use `/goal resume` to continue, or `/goal
pause` to pause a running goal manually. `/goal status` shows turns, requests,
tokens, and work time. Repeated host failures, zero-evidence rounds, and Todo
stall thresholds inject a strategy redirect and reset their intervention epoch;
they do not pause the Goal. At the end of every goal turn
the model reports its disposition through the structured `update_goal` tool
(continue/complete/blocked); when no report arrives, an independent bounded
evaluator judges the turn once, and any evaluator failure pauses the goal
instead of continuing silently.

For complex work, write the objective as a
[task contract](./TASK_CONTRACT.md): Context, Request, Output format,
Constraints, and Pause policy. Goal mode treats those sections as the boundary
for autonomous work. It keeps going with sensible defaults unless the next step
requires an irreversible or externally visible operation, a scope change, or
information only the user can provide.

Legacy simple/write/research classes are still inferred for sidecar and CLI
compatibility, but they no longer select an execution quota. There is no
separate research runtime to configure. Goal state stays in the normal session sidecar, progress
comes only from novel host receipts, canonical todos, `complete_step`, review
and the Delivery checkpoint, and completion is decided by Delivery readiness
plus the bounded Goal evaluator. Legacy `.reasonix/autoresearch/<task-id>/` archives are
read-only: an explicit old path can be recovered as an ordinary Goal, but new
runs never create or update those directories. Deprecated budget flags are
accepted for compatibility but are hidden from help and completion.

## Scheduled runs

A schedule makes Reasonix run a read-only task for you at a time you chose and leave the result waiting. Create
and manage them in Studio under Settings, Automation, Scheduled runs, or with the command line below.

```bash
reasonix schedule add --every 2h --prompt "Summarize what changed in the open TODOs" --workspace ~/src/app
reasonix schedule list
reasonix schedule pause <id>      # pause-all, resume, resume-all, renew and rm work the same way
reasonix schedule tick            # claim and run what is due now, then exit
```

- **A person confirms it.** `add`, `resume`, `renew`, `rm` and `resume-all` need a terminal. `add` shows the whole
  plan (prompt, folder, model, budget, expiry) and asks you to type back its short id; `list` shows who created
  each one. A command run by a tool, a pipe or a scheduler is refused.
  - The host marks every subprocess it starts for the agent with `REASONIX_AGENT_CHILD=1`, and these commands
    refuse when it is set, so an agent that opens its own pty is stopped too.
  - That is a guard against accidents and a trick, not a boundary: a child can clear its own environment, and
    only the sandbox confines what an agent can do.
- **When it runs.**
  - Studio ticks while it is open, with a window or a tray icon showing; a kernel nobody can see, or one with
    no parent that ends it, never starts a run. One kernel at a time ticks.
  - Otherwise call `reasonix schedule tick` from the operating system's scheduler (cron, launchd, Task
    Scheduler).
  - Every tick goes through the store's own budget and expiry checks, so neither route can bypass them.
- **Trusted folders only.** A schedule's folder must be a project you have trusted in Studio or the terminal UI.
  Anything else is refused by the kernel (`schedule.workspace_unknown`), and trust is checked again at every run:
  withdrawing it stops the schedule's runs.
- **Read-only.** A run reads files in its folder and writes nothing. Anything that needs your approval is parked
  for you and never answered on your behalf.
- **Ceilings.** `[schedule]` in your user config can change them; a repository's `reasonix.toml` can only
  tighten them.
  - One run at a time, and by default at least 60 minutes between runs (never under 10).
  - Per run: 250,000 tokens, 40 steps and 15 minutes.
  - 4 runs a day for one schedule and 6 for all of them, and a lifetime budget per schedule.
  - A 30 day expiry that only you renew.
- **Late and missed runs.** Only the latest slot counts. It still runs if it is at most 15 minutes late (never
  more than a quarter of the interval); otherwise it is recorded as skipped and not made up.
- **The model is pinned** when you create the schedule, from your own configuration. If it later becomes
  unavailable the run fails and says so.

## @ references

Embed `@` references in a message and Reasonix resolves them before sending, as
tagged context blocks: `@path/to/file` (or `@dir`) injects a local file's
contents (or a directory listing), and `@<server>:<uri>` injects an MCP
resource. A local path is only treated as a reference when it actually exists,
so ordinary `@mentions` stay literal. Typing `/` or `@` opens an autocomplete
menu — slash commands, or hierarchical file navigation (one directory level at a
time, descend into folders) plus MCP resources.

In Studio's file panel, right-click one file or folder:

- **Copy path** copies its absolute workspace path, including remote roots.
  It is available once the workspace reports an absolute root.
- **Copy reference** copies an `@` token to paste into the composer. Spaces and
  tabs are escaped; folder references end in `/`. Names the reference grammar
  cannot represent, or whose final whitespace the composer would trim, disable
  this action.

Paths containing `:` disable Copy reference because they can be interpreted as
MCP resources instead of workspace entries.

Copying does not open the entry or send a message.

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
executor. Writer and workflow tools remain executor-only.

Reasonix routes each turn deterministically without another classifier model:
questions, short follow-ups, clear atomic edits, and bounded read-only actions
go straight to the executor; bounded implementation work may receive a short
light plan. Ambiguous, cross-surface, structured, high-risk, active-Goal, or
Delivery work receives a full plan unless the request is clearly atomic or
read-only. Explicit Plan Mode
remains a separate host workflow and is never planned twice. An explicit
`plan first` / `先规划` request forces planning, while `just do it` / `直接改`
goes directly to the executor. Execution boundaries are recognized across the
request, not only at its beginning, while quoted examples are ignored. Bare
plan-first requests continue from the planner to the executor automatically.
Requests that explicitly say to wait for confirmation pause at the host
approval boundary and continue to the executor after approval. Only an
explicit `plan only` / `不要执行` request ends the
current turn with the plan persisted and no execution; a later user instruction
can continue in the same session. The phase detail records a privacy-safe route,
depth, and reason code for diagnosis without logging the user prompt.

Light plans contain a compact objective, at most four ordered steps, likely
touchpoints, and the main verification. Full plans distinguish verified from
candidate touchpoints and add relevant non-goals, risks, acceptance criteria,
command-level verification, and rollback guidance when the operation is hard to
reverse. These contracts are part of one stable planner system prompt; only the
small per-turn depth instruction is appended to the user turn, preserving the
planner's prefix cache after the one-time prompt upgrade. The host also gives
light and full research different per-turn round budgets. If a planner still
does not finalize after its bounded research and finalization round, ordinary
plan-and-execute work continues with the executor using the original task.
Plan-only and approval-gated requests remain fail-closed, and the incomplete
planner turn is rolled back instead of leaving an unusable continuation tail.

Reasonix manages normal execution automatically: if an active todo produces no
new completion, unique read, command, or mutation for 8 tool-call rounds, the
host asks the executor to reassess. In Goal mode, the later threshold forces a
smaller step, different tool/approach, focused delegation, or a real blocker
report, then execution continues. Exact repeats do not count as progress; new
host-observed work renews the lease. Two-level task lists keep
the same single-current contract: the active level-1 sub-step is the one
`in_progress` item while its level-0 phase stays `pending`; sub-steps are worked
and signed off in order, and once every sub-step has completed the phase itself
becomes `in_progress` for its own final sign-off.

Existing `[agent].max_steps` and `planner_max_steps` keys remain syntactically
accepted during upgrades, but their values are ignored and removed with a
one-time notice. This prevents a stale hidden limit from truncating automatic
progress or inherited subagent work. Use the one-off CLI `--max-steps` flag when
an explicit run budget is needed.

**An ordinary chat task has no limit of any kind by default** — not rounds, not
tokens, not time, not money. It runs until the model finishes, an adaptive
guard decides it stopped making progress, or you stop it.

One such guard is the **perseveration guard** (a model stuck echoing one block):
it detects when the model emits the same short block of text or reasoning
byte-for-byte.

Detection is **on by default**, but it only reports: a byte-identical block is
sometimes legitimate output — printing a value 300 times, a run of identical
table or log rows, a page of `=` or `A` — so nothing is cut and nothing reaches
the model unless you opt in.

A detected loop is surfaced through the progress-watch channel as a
`perseveration` notice, and the run goes on.

The notice is said once per loop episode: while the same loop keeps tripping the
guard it stays quiet, and it is said again only after a block that does not trip
the guard in between. The user's `progress_watch.pause` setting is what ends the
run on a detected loop.

Cutting the stream and nudging a retry is opt-in with
`[progress_watch].perseveration_retries` (a global default) or a per-provider
`perseveration_retries` on a `[[providers]]` entry, which overrides the global
value for that provider:

```toml
[progress_watch]
perseveration_retries = 0   # the default: report a detected loop, never cut

[[providers]]
name = "deepseek"
perseveration_retries = 1   # this provider also cuts and retries once
```

At `0` a detected loop is only reported, as above.

A **positive** value also cuts the stream, shows the user a
`[retrying (N) avoiding perseveration]` interjection, appends a host-authored
nudge through the mid-turn steer path, and retries up to that many times; once
the budget is spent the run stops resumably with a perseveration pause.

Before retrying, it trims the loop from the turn back to the phrase and its last
repetition, so the retry resubmits evidence of the loop rather than all of it.
The trim reaches both the transcript and the stored turn, so the two never
diverge.

Setting the key negative disables the guard entirely.

An optional spend gate is available when you want one. It bounds a whole task
(every "continue" included, until you start unrelated work), and on crossing it
the task produces one tool-free summary and pauses; the work is saved and the
next message continues it.

```toml
[agent]
task_cost_budget = 5.0            # in the model's pricing currency
task_time_budget_minutes = 60     # wall clock across the whole task
```

Both are off unless set. In particular, `task_time_budget_minutes = 0` (and
legacy negative values) disables the time gate; only a positive value enables
it. Neither has a default, because a stop is a judgement
only you can make: no amount of money is portable across models — a budget
loose enough for a cheap model would land a frontier model within a couple of
answers — and a long task is as often the job you asked for as it is a runaway.

Cost applies only to a priced model. Without a price table that axis stays
inactive rather than reading the task as free; use the time axis for a free or
local model.

Rounds are deliberately not an axis. A turn that reaches a high round count
without spending much is one whose rounds are individually cheap and fast,
which is the case least worth interrupting. Use the one-off `--max-steps` flag
when you specifically want a run bounded by rounds.

Subagent skills inherit the executor model by default. Set `subagent_model` to
run them on another configured model, or use `subagent_models` to override only
specific skills such as `review` or `security_review`.

Subagents delegate in layers: the root is depth 0, and `max_subagent_depth`
(default 8, a stop against a model delegating to itself without bound; 0 removes
it, 1 is the single-layer boundary) is the deepest layer with recursive tools.
`max_subagent_concurrency` and `max_parallel_writers` default to 0, no limit:
write claims, not a count, keep writers apart. A project's `reasonix.toml`
cannot set any of the three. A 429 under heavy parallelism is retried with
backoff and `Retry-After`.

Use `read_only_task` when planning needs isolated, deeper research without
granting write-capable delegation. Use `read_only_skill` when the same need is
best expressed through an existing skill. Both run ephemeral read-only
subagents with only read-only research tools plus safe foreground bash, return
only the final answer, and do not create resumable subagent transcripts.
Read-only nested delegation may be available until `max_subagent_depth` is
reached, but writer-capable `task` / `run_skill` remain unavailable inside these
read-only child registries. Execution settings share one tool surface: call
`use_capability` for `read_only_skill` and other optional tools. Subsequent
writer calls still pass through Permissions/Sandbox.

Every strict read-only child is built through one shared construction
pairing — `RunReadOnlySubAgentWithSession` / `NewReadOnlyAgent` — which marks
the child permanently read-only and applies a final registry filter. The filter
removes writers, destructive MCP targets, readers from unauthorized servers,
and every host-mutating tool. User-installed and project-configured servers are
authorized immediately. Eligible readers may still start on demand. These are
the strict read-only entrances:

| Entrance | Purpose |
| --- | --- |
| `read_only_task` | Isolated read-only research child from the main session |
| `parallel_tasks` (read-only) | Concurrent read-only research children |
| `fleet` with `read_only: true` | Parallel profile-aware batch (forced read-only per item) |
| `read_only_skill` | The same isolation driving an existing skill |
| `reasonix review` (CLI) | Read-only review of a diff or branch |
| Desktop preview/review subagents | Read-only desktop analysis surfaces |

`reasonix review` treats the checkout it reviews as untrusted input and runs
only your own configuration:

- The review skill is the built-in one, or one in your Reasonix home or
  home-directory skill folders. No project skill directory is read, so a
  `<root>/.reasonix/skills/review` never replaces it.
- Tools and their sandbox, including `[tools.search]` `engine` and `rg_path`,
  come from `<Reasonix home>/config.toml`, never from the checkout's
  `reasonix.toml`.
- Hooks are only the ones you configured under the Reasonix home: the global
  `settings.json` and installed plugins. Project hooks from the checkout
  (`<root>/.reasonix/settings.json`) never run, and the interpreter those hooks
  use comes from your own `[tools.shell]`.
- Review hooks start in the checkout root so they can inspect it, but a bare
  command such as `python` never resolves to an executable the checkout ships:
  the hook process runs with `NoDefaultCurrentDirectoryInExePath=1`, which stops
  `cmd.exe` on Windows from searching the current directory first.

The model and provider still resolve from the merged config, as in a session.

In persisted sessions, `parallel_tasks` and `fleet` return a bounded preview
plus one `Subagent reference` per completed child instead of concatenating every
full answer into a truncation-prone tool result. The parent can call
`read_subagent_result` with that reference and page by `offset_bytes`; results
are scoped to the current conversation lineage and workspace. Headless runs
without a persisted parent session remain ephemeral and receive fair bounded
previews, but cannot mint durable references.

A sub-agent that is still running can be sent guidance:

- The model calls `task` with `task_id` set to the sub-agent's `sa_...` reference
  and the guidance as `prompt`. You send it from Settings or the sub-agent's view.
- The sub-agent reads it at its next tool call, framed as coming from the
  delegating session (or from you). Its own approvals still apply.
- If the sub-agent has already finished, `task_id` continues its transcript
  exactly as `continue_from` does.
- `max_subagent_steer_bytes` and `max_subagent_steers_per_minute` bound one
  message and the rate (user config only; Settings > Tools > Guidance to running
  sub-agents; `0` lifts a limit).
- Guidance cannot be sent from a paired device.

The interactive two-model Planner uses a dedicated construction path
(`NewPlannerAgent`): it still blocks bash, file writers, and ordinary writers,
but may call authorized, non-destructive MCP through the fixed
`use_capability` proxy without requiring `readOnlyHint`. Direct `mcp__*`
schemas never enter the Planner tool list, so MCP install/connect churn does
not change the Planner cache prefix after the one-time schema upgrade. Missing
`readOnlyHint` no longer blocks the Planner; tools with `destructiveHint` are
zero-exec and should be written into the plan for the Executor.
In Balanced two-model sessions the Executor has its own frontend for the same
stable proxy, so an `auto_start=false` or destructive capability discovered by
the Planner remains callable by capability ID after handoff. Planner and
Executor ledgers/audits stay isolated and only the Host connection is shared.

Ordinary `task` / `fleet` sub-agents also get the same fixed proxy (session-
shared Host and connections, per-agent frontend/ledger) and may call installed
or project-configured MCP without `readOnlyHint`. Those calls use the trusted
MCP permission path (live authorization plus explicit deny only); writer and
destructive calls are still serialized, recorded as mutations, and subject to
Delivery evidence/lease guards rather than Planner handoff. Strict
`read_only_task` / `read_only_skill` / review sub-agents share the stable proxy
schema and connection reuse but keep the strict execution gate
(`authorized && readOnlyHint && !destructiveHint`). Profile `allowed-tools`
MCP names convert to capability-id allowlists on the proxy; children never
inherit dynamic `mcp__*` schemas.

Inside a strict child, `use_capability` re-checks the resolved target before
commit/permission/hooks/execution. An unconnected eligible MCP reader may start
on demand from the current schema cache. Before `tools/call`, cached
`readOnlyHint`/`destructiveHint` facts are checked against the live
initialize/tools-list result; a reader-to-writer change or destructive promotion
means zero executions and a normal retry through the current boundary. A
schema-only change refreshes the cache for the next session without interrupting
the authorized call. Runtime enablement, authorization, and the complete
connection identity are checked again immediately before dispatch, so a
same-name client from another project/tab cannot be reused accidentally. An
unauthorized server cannot raise privileges there. This strict-child boundary
is narrower than the dedicated Planner: the Planner accepts authorized opaque
non-destructive MCP, while a strict child requires an explicit reader hint and
never exposes writers at all.

Choose the startup execution setting with
`--preset balanced|delivery` (for example, `reasonix run --preset
delivery "fix and verify this bug"`). Legacy `--profile economy|balanced|delivery`
still works (`economy` maps to `balanced`). Both execution settings share the same
provider-visible core tool surface: direct read/bash/edit/write, background-shell
lifecycle tools, `ask`/`compress`/`recall` when registered, and the stable
`use_capability` proxy for optional tools (search, MCP, skills, subagents, docs,
web_fetch, and so on). Calling `use_capability` never expands the top-level
provider schema, so the prompt-cache tool prefix stays stable across execution
settings.

What differs by execution setting is host policy (planning route, verification
intensity, independent review floor), not the tool list:

- **Balanced** (default) — auto light/full planning by risk, risk-tiered
  verification, conditional independent review on medium-risk multi-file work.
- **Delivery** — full acceptance criteria, full verification, forced independent
  review on medium+ risk, and security review on high-risk work. Mutations and
  verification commands are blocked until a concrete acceptance list exists; a
  changed result cannot finalize until it has been reviewed, verified after the
  latest mutation, and signed off with `complete_step`.

Meta tools such as `task`, `run_skill`, and `review` are not counted as mutations
by themselves — only real child writes are. Read-only analysis remains available
without forcing a write.

Inside an interactive TUI session, use `/preset` to inspect the current choice or
`/preset balanced|delivery` to switch it. `/work-mode` and `/profile` are
compatibility aliases. The switch updates the execution setting in place without
rebuilding the controller, preserves history, the session path, leases, and the
Ask/Auto/YOLO posture, and is rejected while a turn, approval/question, background
job, or another runtime switch is active. This command changes only the current
session and does not persist a new global default. Because the provider-visible
tool surface is unified, switching execution settings does not create a new tool-schema
cache prefix.

Desktop tabs expose the same three choices (shown as Light / Balanced / Delivery)
and dual-write `agentPreset` with legacy `tokenMode` (`economy`/`full`/`delivery`)
for one compatibility version.

For interactive frontends, Plan Mode is always an explicit user choice. Select
Plan in the desktop collaboration-mode control or cycle to Plan with
`Shift+Tab` in the CLI. Reasonix first drafts a plan, then waits for approval
before the workflow switches to implementation. Tool calls made while drafting
still use the current Permissions and Sandbox. Legacy `agent.auto_plan` and
`agent.auto_plan_classifier` values are ignored and removed from the user config
during upgrade. The visible reasoning language can be changed with
`/reasoning-language auto|zh|en` in the
session, or `reasonix config reasoning-language auto|zh|en` in a shell/script.
Pass `--local`
to the reasoning-language shell command only when you intentionally want a
project-local override.

The why behind separate sessions (keeping each model's prefix cache-stable) is in
[`SPEC.md` §3.5](./SPEC.md#35-two-model-collaboration-coordinator).

## Free web search with SearXNG

`web_search` can use a [SearXNG](https://docs.searxng.org/) instance that you run yourself, so searches cost nothing. Reasonix does not start it.

```
mkdir -p ./searxng/config/ ./searxng/data/ && cd ./searxng/
docker run --name searxng -d -p 8888:8080 -v "./config/:/etc/searxng/" -v "./data/:/var/cache/searxng/" docker.io/searxng/searxng:latest
```

(the command is from the SearXNG documentation). SearXNG answers JSON only when its `settings.yml` allows it; put this in `./config/settings.yml` and restart the container:

```yaml
use_default_settings: true
search:
  formats: [html, json]
```

Then, in your user config file (a project file cannot set these):

```toml
[tools]
web_search_backends = ["searxng"]
searxng_url = "http://localhost:8888"
```

A backend counts as not answering when:

- the request fails, or the status is not 200;
- the body is not JSON, or no results came back.

Each time that happens Reasonix shows a notice naming the backend and the cause, then tries the next one. If none answers, the model receives `search.all_backends_failed`.

- Engines inside SearXNG that rate-limit you do not fail a search while one engine still answers.

In Settings, **Web access** has a **Test connection** button beside the address. It saves the field, then sends one search to the saved address, follows no redirect, uses no proxy, and says why the instance does not answer.

It never connects to a link-local, cloud-metadata or unspecified address; a loopback or local-network address is fine. The model is told the same code when a real search fails.

| Code | Meaning | What to do |
|---|---|---|
| `search.searxng_json_disabled` | The instance answered 403. SearXNG does that when `json` is not in `search.formats` of its `settings.yml`; a reverse proxy in front of it can also answer 403. | Add `json` to `search.formats` and restart it; if that is set, look at the proxy. |
| `search.searxng_unreachable` | Nothing answered at the address. | Check that it runs and that host and port are right. |
| `search.searxng_not_searxng` | It answered 200 but not with SearXNG's JSON results. | Point at SearXNG itself, not a login page or another service. |
| `search.searxng_redirected` | The address redirects. | Enter the final address, for example the `https` one. |
| `search.searxng_address_refused` | The test will not connect to this address (link-local, cloud metadata or unspecified). | Use a loopback or local-network address for a self-hosted instance. |
| `search.searxng_status` | Any other HTTP status, for example 429 or 500. | Retry later or read the instance's log. |
- Results are third-party content and are labelled as such to the model.

### Paid fallback: the DeepSeek Search API

Add `"deepseek_api"` after `"searxng"` to let `web_search` fall back to DeepSeek's [Search API](https://api-docs.deepseek.com/guides/search) when the free backend did not answer.

```toml
[tools]
web_search_backends = ["searxng", "deepseek_api"]
```

- It uses the key of a provider you configured for `api.deepseek.com`.
- DeepSeek lists the price as $0.75 per 1,000 successful requests (pricing page, read 2026-10-17); it bills only a 200 response with results, and announces the billing start date separately.
- The first time a search would reach it, a card shows the price and the query that would be sent to DeepSeek. Allow once, allow for the session, or always allow, which records `web_search_paid_allowed = ["deepseek_api"]` in your user file.
- After you choose always allow, the queries the model sends are no longer shown one by one before they go to DeepSeek.
- No approval mode answers that card for you, and a project file cannot record the allowance.
- Without an answer the model receives `search.free_failed_paid_needs_consent`. A 402 reaches it as `search.insufficient_balance`.
- Each successful paid request is counted in the usage view under its own source, `deepseek-search`.

### Paid fallback for fetching pages

`web_fetch` can fall back to DeepSeek's [Fetch API](https://api-docs.deepseek.com/guides/search) when this machine could not read a page.

```toml
[tools]
web_fetch_backends = ["local", "deepseek_api"]
```

- `local` is the fetch from this machine and is free. It counts as not answering when the request fails, the status is not 2xx, or no text came back.
- DeepSeek lists the same price as for search, $0.75 per 1,000 successful requests; a request is billed only when it returns page content.
- The first time it would be used, a card shows the price and the address that would be sent to DeepSeek. Agreeing to search is not agreeing to fetch: always allow records `web_fetch_paid_allowed`.
- An address on your own network is never sent. That covers loopback, private and link-local addresses, and names that resolve to them.
- So are names that cannot be public, even when this machine cannot resolve them: a single label such as `jira`, or a name under `localhost`, `local`, `internal`, `lan`, `home.arpa`, `invalid`, `test`, `example` or `onion`. Both are refused with `fetch.internal_address`.
- The whole address, including its query string, is sent to DeepSeek as written. The card shows it in full; after you choose always allow it is no longer shown each time.
- Pages are third-party content. Each successful paid fetch is counted in the usage view as `deepseek-fetch`.

## Letting a model watch a video file

`read_video` has two ways to look at a video.

- Frames: ffmpeg samples still images, which loses audio and the order of fast events.
- Natively: the MP4 itself is handed to a model that watches video, with no ffmpeg in between.

Native needs two declarations in the provider entry of your user config: the models, and how a file reaches them.

```toml
[[providers]]
name = "dashscope"
video_models = ["qwen3-vl-plus"]
video_protocol = "inline_data_url"   # or "moonshot_file"
```

- `inline_data_url`: the file travels inside the request as a `video_url` data URL. DashScope's documentation (read 2026-10-17) limits the encoded string to under 10 MB, so Reasonix caps the file at 7.5 MB.
- `moonshot_file`: the file is uploaded once (`POST /files`, `purpose = "video"`) and the request names it as `ms://<id>`.
- Moonshot's file API (read 2026-10-17) allows 100 MiB per file and 10 GiB per organization, and does not say how long a file is kept.
- A file the vendor no longer holds is uploaded again once. Files stay in your organization's storage until you delete them there.
- A model that is not listed in `video_models`, or an entry with no `video_protocol`, keeps getting frames. Nothing is guessed from a model or host name; an explicit empty list means "no model here watches video".

`read_video` takes `mode`: `auto` (default) sends natively when the selected model offers it and the file fits, otherwise samples frames and says why; `frames` always samples; `native` refuses with a code if the file cannot be handed over.

- Only MP4 is sent natively; any other container is sampled into frames (`video.native_container_unsupported`).
- To lower the size limit, set `[tools] video_native_max_bytes` in your user file; a project file cannot set it, and it can only be lower than the protocol's own limit. A refusal names which limit stopped it (`video.native_too_large`).
- The model sees the video on the one request that follows the read. After it answers, later requests carry a one-line note instead, so a long file is not resent every turn; call `read_video` again to look again. The session file keeps a reference to the file, not its bytes.
- The file is read from disk when the request is built. A file that changed since it was read is not sent (`video.file_changed`), and one that is gone is reported as `video.file_missing`.
- Only a video this process's `read_video` handed out is ever opened; a reference from a restored or imported session is not, and is reported as `video.file_unavailable`.
- Sub-agents do not get native video; they use frames.
- Where there is no OS sandbox, `read_video` still asks before its first use, as before; choose always allow on that card or add a rule for `read_video` to stop being asked.

Not built, and why:

- Gemini: its video input is in the native Gemini API (inline data, the Files API, YouTube URLs); the OpenAI-compatible page does not list a video content part, and Reasonix reaches Gemini through that endpoint.
- Zhipu GLM: the documentation shows `video_url` with a public HTTPS address only and says nothing about base64 or limits, so a local file cannot be sent without Reasonix uploading it somewhere.

## Letting the model change a setting

The model has a `setting` tool. When something it needs is switched off, such as handing a task to another project, it can ask you to turn the switch on instead of sending you to Settings.

A refusal that a setting would lift ends with `(fix: setting ...)`. The tool can also list settings by the words of their id.

- Nothing changes until you answer a card. It names the setting, the old and the new value, and when the change takes effect. It is worded by Reasonix; the model's own reason is only quoted under it.
- Every setting has a class in the code. A preference (a font, a retry count) can be agreed once for the rest of the session. An elevated setting (messaging between sessions, dispatching to another project, a protection the model could otherwise loosen) is asked every time, in every approval mode including YOLO.
- Boundary settings (approval mode, sandbox, network, rules, hooks, provider keys and addresses, safety limits, and the switch for this tool) are never changed by the model. It can only point you at them (`op=open`).
- The card shows the whole new value; a value too long to show is refused instead of cut. The change is written to your user file, a project file cannot grant it, and values are checked with the same rules as the Settings screen.
- At most three proposals are put to you per turn, and none while plan mode is on (the model can still look at settings and point you at one).
- To remove the tool, set `[tools] setting_tool = false`, or leave `setting` out of an explicit `[tools] enabled` list.
## Google Gemini, native (experimental)

The `gemini` provider kind speaks Google's Interactions API. It is experimental: it was written from Google's documentation (read 2026-10-17) and has not been run against the real endpoint, so expect rough edges and report them.

Install the **Google Gemini, native (experimental)** preset, or write the entry yourself:

```toml
[[providers]]
name = "gemini-native"
kind = "gemini"
base_url = "https://generativelanguage.googleapis.com/v1beta"
api_key_env = "GEMINI_API_KEY"
models = ["gemini-3.8-flash"]
```

- Every request carries the whole conversation and `store` is false, so the transcript stays yours; Google keeps nothing of it.
- Thinking runs at the level the entry sets (`/effort`); the preset lists the levels each model documents. Thought summaries are streamed as reasoning, and the thought steps are sent back exactly as received, in order, on the next request.
- `temperature` and `top_p` are never sent: Google's reference page does not list them for this API.
- The preset lists only models whose own pages were read: `gemini-3.8-flash`, `gemini-4-argon`, `gemini-3.6-flash`, `gemini-3.5-flash-lite` and `gemini-3.1-pro-preview`, all with a 1,048,576-token window, image and video input. Others work if you list them and say what they read.
- The older **Google Gemini** preset uses Google's OpenAI-compatible endpoint. That endpoint documents no video input.

### Watching video

Declare the models and how a file reaches them, as for any provider (see Letting a model watch a video file):

```toml
video_models = ["gemini-3.8-flash"]
video_protocol = "gemini_files"   # or "gemini_inline"
```

- `gemini_inline` puts the file in the request as base64. Google's page says "under 20MB total request size" while its table says 100 MB; the narrower reading is used, so the file is held to 15,000,000 bytes (the encoded request is a third larger). You can only lower it with `[tools] video_native_max_bytes`.
- `gemini_files` uploads the file with the Files API resumable protocol, waits until Google reports it `ACTIVE`, refers to it by its uri, and deletes it when the request ends. Google allows 2 GB per file and 20 GB per project and removes files after 48 hours on its own.
- MP4, WebM and MOV are sent natively on both protocols; any other container is sampled into frames. A file Google reports as `FAILED`, or in a state this build does not know, is not sent and the model is told `video.upload_failed`.
- A public YouTube address cannot be given to the model through `read_video`: the host does not fetch addresses, and handing one to the vendor is a separate setting.

### Cost

Reasonix prices Google's list rates per 1M tokens (https://ai.google.dev/gemini-api/docs/pricing, read 2026-10-17):

| Model | Input | Cached | Output (incl. thinking) |
|---|---|---|---|
| `gemini-3.8-flash`, `gemini-3.6-flash` | $0.75 | $0.075 | $3.75 |
| the same two from 2027-01-01 | $1.50 | $0.15 | $7.50 |
| `gemini-3.5-flash-lite` | $0.30 | $0.03 | $2.50 |
| `gemini-3.1-pro-preview` (up to 200k prompt tokens) | $2.00 | $0.20 | $12.00 |

`gemini-3.1-pro-preview` costs more above 200k prompt tokens ($4.00 / $0.40 / $18.00), which one rate card cannot say, so a long prompt reads low. `gemini-4-argon` shows no price on the page and is not priced. Storage for explicit context caching is not counted.

### Handing a video address to the model vendor

Some vendors read a video from a public address instead of a file. `read_video` can take `url` in place of `path`, but only when you turn this on in Settings (**Web access → Video addresses**, or `[tools] video_url_passthrough = true` in your user file; a project file cannot set it).

Turning it on is a confirmation of its own: the model can propose it, and you answer a card every time.

- The vendor visits the address itself; nothing is downloaded on your machine. Turn it on only if you accept that the vendor's servers open the address.
- Only an address you wrote in the conversation is handed over. An address the model found in a page, a file or a tool result, or made up, is refused (`video.url_not_from_user`), so a page's author cannot make the vendor visit an address of their choosing.
- The address must be `https`, with a public host name: no credentials, no IP address, no `localhost`, single-label or local-network name, and at most 2,048 characters (`video.url_invalid`).
- Zhipu GLM takes a public https address in a `video_url` part and no file ([docs.bigmodel.cn](https://docs.bigmodel.cn/cn/guide/models/vlm/glm-4.6v), read 2026-10-17). Declare it with `video_models = ["glm-4.6v"]` and `video_protocol = "zhipu_url"` on the entry; a local file is then read as frames.
- Gemini takes a public YouTube address as a video `uri` on `gemini_inline` and `gemini_files`; no other address is handed to it (`video.url_unsupported_model`).
- A reference restored from a stored or imported transcript is never sent: only an address checked in this process is.
