# Reasonix Engineering Spec

> Reasonix is a coding agent: a thin harness driving multiple models, with **all
> capabilities supplied by configuration and plugins**. This document is the
> contract — code follows it. Change the contract first, then the code.

## 1. Design Principles

1. **Config- and plugin-driven core.** The core knows only interfaces. Concrete
   models and tools are resolved by name from registries, declared in config, or
   injected by plugins. No hardcoded `switch model`.
2. **Single static binary.** `CGO_ENABLED=0`; cross-compile with one command;
   CLI works out of the box.
3. **Lean dependencies.** Standard library by default. A third-party dependency
   must be pure-Go, lightweight, and must not compromise the single-binary /
   cross-platform / distribution story. TOML parsing is the one accepted dependency.
4. **Two extension tiers.** Compile-time built-ins (self-register via `init()`),
   and runtime external plugins (stdio JSON-RPC subprocesses, MCP-compatible).
5. **Interface-first & registry-based.** `Provider` and `Tool` are interfaces.
6. **Evolve, don't over-engineer.**

Language: **English is the primary language for all code** — comments,
user-facing strings, tool descriptions, system prompts, and this spec. The
README is bilingual (`README.md` English + `README.zh-CN.md`).

## 2. Layout

```
reasonix/
├── go.mod / go.sum          # module reasonix; require BurntSushi/toml
├── Makefile                 # build / cross / vet / fmt / test
├── README.md / README.zh-CN.md
├── reasonix.example.toml         # sample config
├── docs/SPEC.md             # this file
├── cmd/reasonix/main.go          # entry; blank-imports built-in providers/tools
├── cmd/reasonix-plugin-example/  # reference MCP stdio plugin (a runnable example)
└── internal/
    ├── cli/                 # subcommand routing, flags, assembly, exit codes
    ├── config/              # TOML loading (flag > project > user > defaults)
    ├── provider/            # Provider interface + types + kind→factory registry
    │   └── openai/          # OpenAI-compatible impl; init() registers "openai"
    ├── tool/                # Tool interface + Registry
    │   └── builtin/         # read_file/write_file/edit_file/move_file/bash/ls/glob/grep
    ├── permission/          # per-call Policy: allow/ask/deny rules → Decision
    ├── command/             # custom slash commands loaded from .reasonix/commands/*.md
    ├── plugin/              # stdio JSON-RPC (MCP) client; adapts remote tools
    └── agent/               # Session + harness loop
```

Dependency direction (acyclic): `cli → {agent, plugin, config} → {tool, provider}`.
Built-in subpackages (`provider/openai`, `tool/builtin`) import their parent to
self-register; parents never import children.

## 3. Core Abstractions

### 3.1 Provider + registry (`internal/provider`)

```go
type Provider interface {
    Name() string
    Stream(ctx context.Context, req Request) (<-chan Chunk, error)
}

// Factory builds a Provider from a resolved config instance.
type Factory func(cfg Config) (Provider, error)

// Register adds a factory under a kind (e.g. "openai"). Called from init().
func Register(kind string, f Factory)

// New instantiates the provider of the given kind.
func New(kind string, cfg Config) (Provider, error)

type Config struct {
    Name    string         // instance name, e.g. "deepseek"
    BaseURL string
    Model   string
    APIKey  string
    Extra   map[string]any // kind-specific options
}
```

- The `openai` kind is an OpenAI-compatible `/chat/completions` implementation.
- **DeepSeek and MiMo are not code — they are config instances** of `kind = "openai"`,
  differing only in `base_url` / `model` / `api_key_env`. Adding another OpenAI-
  compatible model is a config edit, not a code change.
- **A provider is a vendor endpoint** (one `base_url` + `api_key_env`) that offers
  one or more models. An entry declares either a single `model = "..."` or a
  `models = ["...", "..."]` list (with an optional `default`); the list form lets
  one vendor expose several models without re-declaring the endpoint/key — picking
  a model reuses the same connection. A **model reference** (`default_model`, the
  `--model` flag, the desktop switcher) resolves via `Config.ResolveModel`, which
  accepts a provider name (→ its default model), a bare model name, or an explicit
  `provider/model`. `context_window` / `price` are per-provider, so models that
  need distinct values stay separate single-`model` entries.
- Streaming tool-call deltas are accumulated by index inside the provider; only
  complete `ToolCall`s are emitted.

### 3.2 Tool + registry (`internal/tool`)

```go
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage // JSON Schema for parameters
    Execute(ctx context.Context, args json.RawMessage) (string, error)
}
```

- Built-in tools self-register into a process-global builtin set via `init()`
  (`tool.RegisterBuiltin(t)`); `tool.Builtins()` lists them.
- A runtime `*Registry` is assembled per run: enabled built-ins (filtered by
  config) **plus** plugin-provided tools. The agent only sees the `*Registry`.
- `Execute` parses raw JSON args itself. Errors are returned, not fatal — the
  agent feeds them back so the model can self-correct.

### 3.3 Plugins (`internal/plugin`) — MCP client

An external plugin is an MCP server declared in config. The wire protocol is
**JSON-RPC 2.0** in every case; only the transport differs. A `transport`
interface (`call` / `notify` / `close`) abstracts that, so the MCP-level logic
(handshake, `tools/list`, `tools/call`, …) is written once.

- **Transports** (config `type`):
  - `stdio` (default) — a local subprocess; one JSON message per line over the
    child's stdin/stdout (the MCP stdio convention). Declared with
    `command` / `args` / `env`; terminated on ctx cancel / shutdown.
  - `http` (a.k.a. `streamable-http`) — a remote server at `url`. Each request
    is an HTTP POST; the server replies with either `application/json` (one
    response) or `text/event-stream` (an SSE stream carrying the response plus
    any server notifications). The `Mcp-Session-Id` response header, once seen,
    is echoed on subsequent requests. Static `headers` (e.g. a bearer token) are
    sent on every request. OAuth is out of scope for now (see §9).
  - `sse` — the legacy 2024-11-05 HTTP+SSE transport; recognised but deferred
    (deprecated upstream — use `http`). Configuring it returns a clear error.
- `${VAR}` / `${VAR:-default}` are expanded in `command`, `args`, `env`, `url`,
  and `headers` so secrets come from the environment, not the config file.
- Lifecycle: `initialize` → `notifications/initialized` → `tools/list`;
  invocation via `tools/call {name, arguments}`.
- Each remote tool is adapted to the `Tool` interface and injected into the run
  registry, namespaced `mcp__<server>__<tool>` (spaces normalised to `_`) to
  match Claude Code and avoid clashes.
- A tool's MCP `annotations.readOnlyHint` maps to `Tool.ReadOnly()`. It defaults
  to false (a remote tool is opaque — we can't see its side effects), so a
  plugin opts a tool into parallel-batch dispatch and the permission layer's
  reader-default by declaring `readOnlyHint: true` in `tools/list`.
- `prompts/list` + `prompts/get` surface as `/mcp__<server>__<prompt>` slash
  commands; `resources/list` + `resources/read` are referenced as
  `@<server>:<uri>` in chat. `/mcp` shows connected servers and their counts.
- `cmd/reasonix-plugin-example` is a runnable reference stdio server (`echo`,
  `wordcount`), driven by an end-to-end test that builds the real binary.

### 3.4 Agent (`internal/agent`)

- `Session` holds `[]Message`.
- `Run(ctx, input)` loop: build `Request` (with tool schemas) → `provider.Stream`
  → print text deltas live, collect complete tool calls → if none, done; else
  execute each tool (built-in or plugin) and append results → repeat, bounded by
  `maxSteps`. `ctx` threads throughout (Ctrl-C aborts in-flight requests).
- A `Runner` is anything with `Run(ctx, input) error`; both `Agent` and
  `Coordinator` satisfy it, so the CLI is agnostic to single- vs two-model mode.

### 3.5 Two-model collaboration (`Coordinator`)

When `agent.planner_model` names a provider different from the executor, a
`Coordinator` runs two models in **separate sessions** to keep each one's prompt
prefix cache-stable:

- The **planner** (low-frequency) runs in its own session with the same standing
  memory context plus a filtered read-only research tool set, then produces a
  concise plan. It can inspect files/docs before planning, but writer and
  workflow tools are not exposed to it. `agent.planner_max_steps` bounds this
  read-only exploration independently from the executor's `agent.max_steps`.
- The plan is handed off as structured text to the **executor** — a full
  tool-using `Agent` in its own session — which carries it out.
- The sessions never mix, so neither model's prefix is disturbed by the other's
  turns; both grow prepend-only and stay cache-friendly. This reconciles
  "cache-first" with "two-model collaboration": switching models *inside one
  shared conversation* would break the prefix and tank cache hits, so we don't.

### 3.6 Context management (compaction)

Long tasks eventually fill the model's context window. Reasonix manages this with
**low-frequency compaction** that respects the cache-first design:

- Each provider declares its `context_window` (tokens). When a turn's reported
  `prompt_tokens` reach `compactRatio` (default `0.8`) of that window, the
  executor compacts **once** before the next turn.
- Compaction folds only the assistant/tool work. Every **user turn** small
  enough to be a brief and every **prior digest** is kept verbatim; the foldable
  remainder is summarized — using the executor's own provider, no tools — in
  place. The boundary is aligned backward off any tool result so the recent tail
  never begins with an orphan tool message whose `tool_calls` were summarized away.
- The dropped originals are archived under the user config dir
  (`reasonix/archive/<timestamp>.jsonl`; see §5 for its per-OS location), one
  message per line, so the full history stays traceable.
- The read-only `history` tool gives the agent on-demand BM25 retrieval over
  saved session JSONL files. `scope="project"` searches the current controller's
  session directory; `scope="global"` also searches the user-global session
  directory and compacted-history archives. `operation="around"` can then read a
  bounded transcript window around a returned hit. Search keeps the best hit and
  trims trailing common-word-only noise with a relative score floor; a 0-result
  response tells the agent how to retry with rarer terms or widen scope.
- The read-only `memory` tool gives the agent on-demand search/list/read access
  to saved auto-memory files. It complements the writer tools: `memory` checks
  what already exists, `remember` saves or updates a fact, and `forget` removes
  a stale one from the active index while archiving the file for traceability.
  Archived memory files are visible in local management surfaces (`/memory`,
  TUI, desktop panel) but are excluded from active-memory retrieval. Memory
  search uses the same relative BM25 floor and guides the agent to fall back to
  history when exact original wording or tool output matters.
- Agent-initiated `remember` and `forget` calls require a fresh human approval
  each time, even when tool auto-approval or YOLO/full-access mode is enabled.
  The approval request includes a compact preview of the memory being saved or
  archived, while external notification hooks only receive the tool name.
  User-initiated memory edits in the local UI are already explicit user actions.
  See [`SESSION_MEMORY_RETRIEVAL.md`](SESSION_MEMORY_RETRIEVAL.md) for the
  detailed implementation contract.

**What survives a fold.** A fact the user states in a normal-sized turn is kept
verbatim and is never summarized away — at any point in the session, across any
number of compactions. A digest, once written, is likewise kept verbatim rather
than re-summarized, so facts it captured are not lost to drift. The one
**best-effort** boundary: a fact buried inside a single oversized message (a
large paste, over the per-turn pin budget) folds with the rest, so its survival
depends on the summarizer catching it while compressing bulk. There is no
reliable way to auto-detect an arbitrary fact in bulk, so durable facts belong in
their own turn rather than buried in a large paste; the raw oversized content is
still archived and recoverable either way.

This is the **only** point where the prompt prefix changes — a deliberate, rare
"cache-reset point". Between compactions the session grows prepend-only and
stays cache-friendly, so cache hit rate (the key observability signal) stays
high. `context_window = 0` disables compaction for an instance.

### 3.7 Permissions (`internal/permission`) — per-call gating

A coding agent runs shell commands and edits files autonomously. The permission
layer decides, **per tool call**, whether to allow it, deny it, or ask the user
first. It is independent of the model and of the CLI — the agent consults a
`Gate` interface at execute time; the gate is built from a static `Policy` plus
an optional interactive `Approver`.

```go
type Decision int            // permission package
const (Allow Decision = iota; Ask; Deny)

// Policy evaluates static rules against a tool call. Pure, no I/O.
type Policy struct { Mode Decision; Allow, Ask, Deny []Rule }
func (p Policy) Decide(toolName string, readOnly bool, args json.RawMessage) Decision
```

- **Rule syntax.** A rule is `Tool` (matches any call in that tool family) or
  `Tool(specifier)` (matches when the call's *subject* matches the specifier).
  Bash and file mutation approvals use Claude Code-style families such as
  `Bash(npm run build)`, `Bash(npm run test:*)`, and `Edit(docs/**)`. Built-in
  file mutations include writes, edits, notebook edits, symbol/range deletes,
  and `move_file` renames/moves. Legacy
  lowercase tool IDs and `tool=literal` rules still load for compatibility. The
  `:*` suffix marks a Bash command-prefix approval; generated prefix rules also
  reject later commands that introduce shell operators, so `Bash(go test:*)`
  does not cover `go test ./... && rm -rf tmp`.
  Legacy `Bash(go test *)` prefix rules still load, but new rules are saved as
  `Bash(go test:*)`. The subject is extracted generically from the call's JSON
  args by a small set of
  known keys — `command` (bash), `path` / `file_path` (file tools), `pattern`
  (grep/glob) — so tools need not change. A rule whose subject the args don't
  expose only matches in its bare `Tool` form.
- **Precedence.** `deny` > `ask` > `allow` > fallback. Fallback is `Allow` for
  read-only tools and `Mode` (default `Ask`) for writers. `deny` always wins, so
  a broad `allow = ["Bash"]` can still be carved by `deny = ["Bash(rm -rf*)"]`;
  conversely `ask` overrides a broad `allow` to force a prompt on a risky subset.
- **Resolving `Ask`.** The interactive front-end (the chat TUI) prompts the user
  — allow once / allow this approval scope for the session / always allow this
  approval scope / deny — via an `Approver`. For Bash, the default scope is the
  concrete command subject, and the user may choose a conservative command-prefix
  scope when available (for example `Bash(go test:*)`) so similar invocations in
  the same session or saved config do not prompt again. For file-mutation tools,
  a session grant covers editing for the rest of the session while a persisted
  grant is path-scoped when a path is available, stored as `Edit(<path>)` so all
  built-in file-mutating tools share it. A
  non-interactive run
  (`reasonix run`, a sub-agent, anything with no TTY / no approver) cannot prompt, so
  it resolves `Ask` to **allow** — preserving autonomous behaviour. A `Deny` is a
  hard block in *every* mode: the tool never executes and the model receives a
  "blocked" result it can adapt to (the same shape as a plan-mode refusal).
- **Relationship to plan mode.** Plan mode (§3.4) is an orthogonal, coarser gate
  that refuses *all* writers regardless of policy; it is checked first. The
  permission layer is the fine-grained, always-on gate underneath it.
- **User decisions are separate from tool approvals.** Runtime tool approval has
  three user-facing postures: `ask` ("需要批准"), `auto` ("自动批准"), and
  `yolo` ("Yolo批准"). `auto` lets the permission policy auto-approve the writer
  fallback while preserving explicit ask/deny rules; `yolo` skips all tool
  permission approvals for approval-gated tools such as writers and Bash.
  Neither posture answers `ask` questions or approves `exit_plan_mode` plans for
  the user.
  Auto-plan is also a separate feature flag: when enabled, a complex task may
  still enter plan mode in any tool approval posture. After a user approves a
  plan, the controller opens a short `approvedPlanAutoApproveTools` execution
  window so the model can perform the approved writes without re-prompting; that
  transient window still does not auto-approve future plans. In headless `ask`
  execution, any fallback answer is labelled as a model assumption, not as a
  user decision.

- **Collaboration mode is separate from tool approval.** The desktop composer
  presents collaboration as `normal` ("正常模式"), `plan` ("计划模式"), and
  `goal` ("目标模式"). `/goal <objective>` starts an autonomous, session-scoped
  active goal: the controller prepends goal context to user turns outside the
  cache-stable system prompt and keeps issuing continuation turns until the
  model reports completion, repeats the same blocked state three times, the user
  stops it, or the safety continuation limit is reached. Blocked-state matching
  is normalized for casing, whitespace, and punctuation so minor wording drift
  does not reset the audit; restarting a goal begins a fresh blocked audit.
  `/goal clear` removes it. Switching into plan/normal mode clears the active
  goal in the desktop UI so the collaboration mode remains one of the three
  choices, while the underlying tool approval posture is preserved.

| Tool approval posture | Tool approvals | Plan approval | `ask` questions |
| --- | --- | --- | --- |
| Need approval / `ask` | Follow permission policy (`Ask` prompts interactively) | Waits for user | Waits for user |
| Auto approve / `auto` | Writer fallback auto-allowed; explicit ask/deny rules still apply | Waits for user | Waits for user |
| YOLO approval / `yolo` | Approval prompts auto-allowed unless denied | Waits for user | Waits for user |
| Approved-plan execution window | Approved plan's tool calls auto-allowed unless denied | Future plans still wait | Waits for user |

Out of the box (`mode = "ask"`, no rules) `reasonix run` behaves exactly as before
(writers resolve `Ask`→allow with no TTY), while `reasonix chat` now prompts before
each writer/bash call. `deny` rules harden both modes.

### 3.8 Slash commands (`internal/command`)

The chat TUI accepts `/command` input. Three kinds share one dispatch:

- **Built-in actions** (`/compact`, `/new`, `/clear`, `/effort`, `/mcp`, `/help`) manipulate session
  state locally and never reach the model. `/new` starts a new session while
  saving the previous transcript for resume/history. `/clear` requires
  confirmation, then discards the current context without saving it; it does not
  delete project memory.
- **Custom commands** are Markdown files under `.reasonix/commands/` (project) and
  `reasonix/commands/` in your OS config dir (user; see §5); the project dir overrides the user dir on a
  name clash. A file `review.md` becomes `/review`; a subdirectory namespaces it
  (`git/commit.md` → `/git:commit`). Invoking one renders its body and sends the
  result as the next user turn.
- **MCP prompts** (§3.3) appear as `/mcp__<server>__<prompt>`.

```markdown
---
description: Review the staged diff
argument-hint: [focus-area]
---
Review the staged diff. Focus on $ARGUMENTS, list bugs with file:line.
```

- Frontmatter is an optional `---`-fenced block of simple `key: value` lines;
  `description` and `argument-hint` are recognised (no YAML dependency — Reasonix
  stays lean). The remainder is the body template.
- Substitution in the body: `$ARGUMENTS` (all args, space-joined), `$1`…`$N`
  (positional, empty when absent), `$$` (a literal `$`). Arguments are the
  space-separated tokens after the command.
- Loading is pure (`command.Load(dirs...)`) and tested; a malformed file is
  skipped, not fatal. Custom and MCP-prompt commands both resolve to text and
  reuse the same "start a turn" path as a typed message.

#### CLI modal/composer ownership

The Bubble Tea chat TUI has one bottom composer. A slash-command overlay must
declare whether it owns keyboard input:

- **Modal overlays** own navigation/confirm/cancel keys and must hide the
  composer while open. Examples: `/mcp`, `/resume`, `/rewind`, approval prompts,
  and non-typing `ask` choice cards.
- **Input-owned overlays** are attached to the textarea and must keep the
  composer visible. Examples: slash/@ autocomplete and `ask` free-text mode.

New CLI overlays must update `chat_tui.hideComposer()` and add/extend layout
tests so `bottomRows()` accounts for either `panel + status` or
`panel + composer + status`. This prevents inactive chat input boxes from being
rendered under modal panels.

### 3.9 Chat references (`@`)

A chat message may embed `@` references; before the turn is sent, each is
resolved and prepended to the message as a tagged block the model can read.

- `@<server>:<uri>` where `<server>` is a connected MCP server → an MCP
  resource (`resources/read`), wrapped `<resource ref="…">…</resource>`.
- `@<path>` otherwise → a **local file or directory**, but only when the path
  actually exists on disk. This existence gate is the disambiguator: an ordinary
  `@mention` or an email address resolves to no file and stays literal text. A
  file is wrapped `<file path="…">…</file>` (size-capped, binary files noted not
  dumped); a directory becomes a recursive listing (depth-first, skipping common
  noise like `.git` and `node_modules`).
- Resolution is asynchronous (off the TUI event loop); a fetch failure surfaces
  as a notice but doesn't block the turn. Reads are user-initiated and read-only
  — they do **not** pass the permission gate (§3.7).
- Typing `/` or `@` opens an autocomplete menu above the input. The `@` menu
  navigates **one directory level at a time** (`os.ReadDir`, never a recursive
  walk — bounded for huge directories): a directory entry descends, a file
  completes, and MCP resources appear alongside top-level entries. The
  bottom-region menu changes height only on these discrete actions, never per
  streamed token, so scrollback stays clean (§ rendering).

## 4. Data Types (`internal/provider`)

```go
type Role string
const (RoleSystem Role = "system"; RoleUser Role = "user"
       RoleAssistant Role = "assistant"; RoleTool Role = "tool")

type Message struct {
    Role       Role       `json:"role"`
    Content    string     `json:"content,omitempty"`
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
    ToolCallID string     `json:"tool_call_id,omitempty"`
    Name       string     `json:"name,omitempty"`
}

type ToolCall   struct { ID, Name, Arguments string }              // Arguments: raw JSON
type ToolSchema struct { Name, Description string; Parameters json.RawMessage }
type Request    struct { Messages []Message; Tools []ToolSchema; Temperature float64; MaxTokens int }

type ChunkType int
const (ChunkText ChunkType = iota; ChunkToolCall; ChunkDone; ChunkError)

type Chunk struct {
    Type     ChunkType
    Text     string    // ChunkText
    ToolCall *ToolCall // ChunkToolCall
    Err      error     // ChunkError
}
```

## 5. Configuration (TOML)

Resolution order: **flag > project `./reasonix.toml` > the user config file
> built-in defaults**. The user config lives in your OS config dir — `~/.config/reasonix/`
on Linux, `~/Library/Application Support/reasonix/` on macOS, `%AppData%\reasonix\` on
Windows. Secrets come from the environment via `api_key_env` and
are never stored in config files. A `.env` in the working directory is loaded if
present. Step-limit preferences usually belong in the user config; project
`reasonix.toml` should override them only when the repository needs shared
runtime bounds.

```toml
default_model = "deepseek"   # provider name (→ its default model) or "provider/model"
# language    = "zh"                # ui language tag; empty = auto-detect from $LANG / $REASONIX_LANG

[agent]
system_prompt = "You are Reasonix, a coding agent..."  # or system_prompt_file = "..."
max_steps         = 0    # executor tool-call rounds; 0 = no limit
planner_max_steps = 12   # planner read-only tool-call rounds; 0 = no limit
temperature       = 0.0
reasoning_language = "auto"       # visible reasoning text: auto|zh|en
# planner_model = "mimo"   # optional: two-model collaboration (low-frequency planner)
# subagent_model = "deepseek-pro"   # optional default for runAs=subagent skills
# subagent_models = { review = "deepseek-pro", security_review = "deepseek-pro" }

# A vendor endpoint exposing several models under one base_url/key.
[[providers]]
name           = "deepseek"
kind           = "openai"
base_url       = "https://api.deepseek.com"
models         = ["deepseek-v4-flash", "deepseek-v4-pro"]
default        = "deepseek-v4-flash"   # optional; defaults to models[0]
api_key_env    = "DEEPSEEK_API_KEY"
context_window = 1000000   # tokens; harness compacts older history near this limit (0 disables)

# A single-model entry (use when a model needs its own base_url/context_window/price).
[[providers]]
name        = "mimo-pro"
kind        = "openai"
base_url    = "https://token-plan-cn.xiaomimimo.com/v1"
model       = "mimo-v2.5-pro"
api_key_env = "MIMO_API_KEY"

[[providers]]
name        = "mimo-flash"
kind        = "openai"
base_url    = "https://token-plan-cn.xiaomimimo.com/v1"
model       = "mimo-v2.5"
api_key_env = "MIMO_API_KEY"

[tools]
enabled = []   # omit/empty = all built-ins
bash_timeout_seconds = 120   # foreground safety cap; set 0 for no tool-local cap

[tools.shell]
prefer = "auto"   # auto (default) | bash | powershell | pwsh — force the shell tool's interpreter
# path = "C:\\Program Files\\PowerShell\\7\\pwsh.exe"   # explicit executable for the chosen shell

[skills]
# paths = ["~/my-skills", "../shared/skills"]   # extra custom skill roots
# excluded_paths = ["~/.agents/skills"]         # hide convention roots without deleting folders
# disabled_skills = ["review"]                  # hidden from prompt, slash invocation, and skill tools

[permissions]
mode  = "ask"                              # writer fallback when no rule matches: ask|allow|deny
deny  = ["Bash(rm -rf*)", "Bash(git push*)"]   # hard-blocked in every mode
allow = ["Bash(go test:*)", "Bash(git status:*)"]  # never prompted
ask   = []                                 # force a prompt even if otherwise allowed

[sandbox]
# workspace_root = ""          # file-writers confined here; empty = cwd (writes stay in-project)
# allow_write    = ["/tmp"]    # extra dirs write_file/edit_file/multi_edit/move_file may modify

[[plugins]]
name    = "example"            # type defaults to "stdio"
command = "reasonix-plugin-example"
args    = []
# env   = { FOO = "bar" }

# [[plugins]]                   # a remote MCP server over Streamable HTTP
# name    = "stripe"
# type    = "http"             # "stdio" (default) | "http" | "sse"
# url     = "https://mcp.stripe.com"
# headers = { Authorization = "Bearer ${STRIPE_KEY}" }   # ${VAR} / ${VAR:-default} expanded
```

`reasonix setup` writes this default config so the CLI is usable out of the box.

MCP servers may also be declared in a project-root `.mcp.json` using Claude
Code's exact `mcpServers` schema (`command`/`args`/`env`, `type`/`url`/`headers`,
`${VAR}` expansion). It is read after the TOML files and merged into
`[[plugins]]`; on a name collision `reasonix.toml` wins (it is the more explicit,
Reasonix-specific source). This lets a server already configured for Claude work in
Reasonix unchanged.

```json
{ "mcpServers": {
  "stripe": { "type": "http", "url": "https://mcp.stripe.com",
              "headers": { "Authorization": "Bearer ${STRIPE_KEY}" } }
} }
```

`[sandbox]` is the *enforcement* layer beneath permissions (which are *policy*).
Phase 0 confines the file-writing built-ins (`write_file`, `edit_file`,
`multi_edit`, `move_file`) to `workspace_root` (default cwd) plus `allow_write`: a write whose
target — resolved to an absolute, symlink-free path so a symlinked dir or `..`
cannot tunnel out — falls outside every root is refused, and the error is fed
back to the model. Confinement is on by default (root = cwd), so edits stay in
the project; reads are unrestricted. `bash` is itself jailed on macOS by default
(`[sandbox] bash = "enforce"`, Seatbelt): each command runs under sandbox-exec
allowed to write only the same roots (+ temp and toolchain caches) and to reach
the network only when `network = true`. Unsupported platforms fall back to
running unconfined. The escape-prompt and Linux support are Phase 1's remainder (§9).

## 6. Error Handling

- Library code wraps with `fmt.Errorf("...: %w", err)` and returns; it never
  prints or calls `os.Exit`.
- Only `cli` / `main` decide exit codes and user-facing messages.
- Tool execution errors are fed back to the model, not fatal.
- Network layer should apply bounded exponential backoff on 429 / 5xx
  (interface reserved; implementation may follow).

## 7. Code Style

- `gofmt` + `go vet` must be clean; package names lowercase; exported
  identifiers documented; comments explain *why*, not *what*.
- No premature generalization. Prefer clear and direct.

## 8. Distribution

- Build: `CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=$(VERSION)" -o reasonix ./cmd/reasonix`
- Cross matrix: `darwin|linux|windows` × `amd64|arm64`.
- Version injected via ldflags (`git describe --tags --always`).
- Install: prebuilt binary / `go install` / future `brew tap`.

## 9. Roadmap (not in current scope)

- Sandbox Phase 1: an OS-level jail for `bash` so commands — not just the
  file-writer built-ins (Phase 0) — are confined to the workspace. **macOS
  (Seatbelt via `sandbox-exec`) ships, on by default** (see §5). Remaining: (a)
  the escape-prompt — detect a sandbox-denied failure and offer to re-run the
  command unconfined via the permission gate (in `reasonix run`, the command just
  fails and the model adapts), which completes the "allow inside the box, prompt
  at its edge" model; (b) Linux (bubblewrap / landlock). Shells out to OS tooling
  so the binary stays dependency-free; Windows is out of scope. With this in
  place, "always allow" rule persistence becomes optional rather than load-bearing.
- MCP long tail (deferred deliberately — no consumer / no foundation yet): OAuth
  2.0 + `headersHelper` auth for remote servers; the remaining `.mcp.json` scopes
  (local / user — project scope shipped, see §5); tool-search deferral;
  `list_changed` live updates; channels / elicitation / roots; plugins that
  provide *providers*, not just tools.
- An Anthropic-native provider `kind` (native prompt-cache control), proving the
  registry generalises beyond one wire format.
- "Always allow" persistence writing learned rules back to project config; a
  per-session permission override flag for `reasonix run`.
