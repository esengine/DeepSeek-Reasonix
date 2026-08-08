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
    ├── remote/              # SSH transport for the Remote-SSH module
    │   ├── forward/         # -L / -R port-forward lifecycle
    │   ├── sftpfs/          # SFTP file layer (quarantines pkg/sftp)
    │   └── bootstrap/       # detached `reasonix serve` bootstrap over SSH
    └── agent/               # Session + harness loop
```

Dependency direction (acyclic): `cli → {agent, plugin, config} → {tool, provider}`.
Built-in subpackages (`provider/openai`, `tool/builtin`) import their parent to
self-register; parents never import children. The Remote-SSH module layers
`cli → remote/bootstrap → remote → {remote/forward, remote/sftpfs, config,
netclient}`; `remote` and its subpackages never import `cli`, `agent`, or
`serve`, and all interactivity flows through callbacks (host-key / secret
prompts) so the desktop module consumes the same surface. See §Remote below.

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
- **OpenAI-compatible vendors are config instances** of `kind = "openai"`,
  differing only in `base_url` / `model` / `api_key_env`. Adding another OpenAI-
  compatible model is a config edit, not a code change.
- **A provider is a vendor endpoint** (one `base_url` + `api_key_env`) that offers
  one or more models. OpenAI-compatible chat normally posts to
  `base_url + "/chat/completions"`; set `chat_url` only for gateways that require a
  full request URL. An entry declares either a single `model = "..."` or a
  `models = ["...", "..."]` list (with an optional `default`); the list form lets
  one vendor expose several models without re-declaring the endpoint/key. A
  **model reference** (`default_model`, the `--model` flag, the desktop switcher)
  resolves via `Config.ResolveModel`, which accepts a provider name (→ its default
  model), a bare model name, or an explicit `provider/model`. `context_window` is
  the provider-wide fallback; `model_overrides.<model>.context_window` can replace
  it for one model. Per-model `prices` use model IDs as keys.
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
- Tool schemas are canonicalized on registry insertion. The built-in contract is
  documented in [`TOOL_CONTRACT.md`](TOOL_CONTRACT.md) and backed by tests that
  compare the documented surface against the same canonical schema path.
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
    sent on every request. When no static `Authorization` header is configured,
    user-initiated OAuth uses Protected Resource Metadata and Authorization
    Server Metadata discovery, dynamic client registration, PKCE S256, a
    loopback callback, resource indicators, and refresh-token rotation. Client
    credentials and tokens are stored with mode `0600` in the server's private
    Reasonix MCP state directory, outside the workspace; tokens are bound to the
    configured resource URL and are never reused after that URL changes. OAuth
    discovery, registration, and token requests honor Reasonix's resolved
    network-proxy settings. Removing a declaration clears this state unless the
    effective fallback uses the same OAuth resource.
  - `sse` — the legacy 2024-11-05 HTTP+SSE transport. A persistent GET stream
    receives an announced relative POST endpoint, JSON-RPC responses, and server
    messages. Cross-origin announced endpoints are rejected so static headers
    cannot leak.
- `${VAR}` / `${VAR:-default}` are expanded in `command`, `args`, `env`, `url`,
  and `headers` so secrets come from the environment, not the config file.
- Lifecycle: `initialize` → `notifications/initialized` → `tools/list`;
  invocation via `tools/call {name, arguments}`.
- When a workspace root exists, initialize advertises `roots` and transports
  answer `roots/list` with its file URI. `tools/call` includes a per-call
  `_meta.progressToken`; matching `notifications/progress` messages stream into
  the existing tool-progress event path.
- A stdio server uses one persistent transport for initialize, reads, and
  writes, preserving state such as browser sessions across tool calls. The
  process uses the server's process sandbox because process confinement cannot
  change per RPC; read-only eligibility and destructive filtering remain local
  workflow gates rather than separate process sandboxes.
- Configuration provenance is runtime metadata and determines persistence scope.
  Desktop and CLI installs write the user-global `config.toml`; project
  `reasonix.toml` and `.mcp.json` entries remain in their owning project file.
  Every configured source is trusted without a separate launch-confirmation
  step. Project entries override same-name global entries, and project
  `reasonix.toml` overrides `.mcp.json`. Editing writes to the effective entry's
  source; removing it reveals the next lower-priority declaration.
- Each remote tool is adapted to the `Tool` interface and injected into the run
  registry, namespaced `mcp__<server>__<tool>` (spaces normalised to `_`) to
  match Claude Code and avoid clashes.
- A tool's MCP `annotations.readOnlyHint` maps to `Tool.ReadOnly()`. It defaults
  to false (a remote tool is opaque — we can't see its side effects), so a
  plugin opts a tool into parallel-batch dispatch and the permission layer's
  reader-default by declaring `readOnlyHint: true` in `tools/list`.
- Installation is the trust decision for tool metadata. Reasonix assumes an
  installed server reports `readOnlyHint` and `destructiveHint` honestly;
  planner/read-only filtering is a workflow boundary for trusted servers, not
  containment against a malicious MCP server. Explicit deny rules and the
  process sandbox remain host-controlled boundaries.
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
  concise plan. A deterministic host policy chooses executor-only, light
  planning, full planning, plan-for-approval, or explicit plan-only from
  pristine user text plus trusted turn metadata. It does not call a classifier
  model and does not infer host state from controller-authored prompt blocks.
  Explicit Plan Mode, synthetic turns, short contextual replies, atomic edits,
  and bounded read-only actions avoid a second planner; cross-surface,
  structured, ambiguous, and high-risk work uses the full contract. Active Goal
  and Delivery turns upgrade non-atomic mutation work, while bounded read-only
  actions remain executor-only. The privacy-safe
  route/depth/reason decision is emitted in phase detail.
- Light plans use a small per-turn research-round budget and return a compact
  objective, 1-4 ordered steps, likely touchpoints, and primary verification.
  Full plans use a larger bounded budget and distinguish verified from candidate
  touchpoints, with risks, acceptance criteria, command-level verification, and
  rollback when relevant. The depth contract stays in one stable system prompt;
  only a small host-authored `<planner-turn>` block changes per user turn. If
  the planner still does not finalize after the bounded research and grace
  round, plan-and-execute falls back to the executor with the pristine task;
  plan-only and plan-for-approval remain fail-closed. The incomplete planner
  turn is rolled back rather than exposed as a broken manual continuation.
- A bare plan-first route hands the completed plan directly to the executor.
  Plan-for-approval is reserved for an explicit request to wait for
  confirmation; the host enforces that boundary even if the planner omits its
  marker, then hands the approved plan to the executor. A headless host persists
  the plan so a later turn can continue. Explicit plan-only requests persist the
  plan and end the current turn without execution. A planner failure on either
  execution boundary cannot fall back to the executor. These directives may
  appear after the task clause; quoted examples do not change the route.
- The plan is handed off as structured text to the **executor** — a full
  tool-using `Agent` in its own session — which validates candidate assumptions
  and carries it out.
- The sessions never mix, so neither model's prefix is disturbed by the other's
  turns; both grow prepend-only and stay cache-friendly. This reconciles
  "cache-first" with "two-model collaboration": switching models *inside one
  shared conversation* would break the prefix and tank cache hits, so we don't.

### 3.6 Context management (compaction)

Long tasks eventually fill the model's context window. Reasonix manages this with
**low-frequency compaction** that respects the cache-first design:

- Each provider declares its `context_window` (tokens). Context maintenance is
  tiered: below `agent.tool_result_snip_ratio` (default `0.6`) the session is
  left untouched apart from the soft notice; at the snip ratio, stale tool
  results before the recent tail are archived and shortened with deterministic
  head/tail markers; at `agent.compact_ratio` (default `0.8`) stale tool results
  are archived and pruned to short placeholders before any summary call; only if
  pruning still leaves the prompt above the threshold does summary compaction
  run. At `agent.compact_force_ratio` (default `0.9`), the existing forced fold
  may proceed even when the fold economics would normally skip it.
- Users can inspect or change the 65–85% automatic threshold with
  `reasonix config compact-ratio [--local] [VALUE]`. The default is 80%; the
  project-local value overrides the shared user config used by desktop and new
  CLI sessions.
- A positive `model_overrides.<model>.context_window` replaces the provider-wide
  value after model resolution. Missing or zero model overrides inherit the
  provider value; provider-level `context_window = 0` disables compaction.
- `max_output_tokens` is a separate total-output budget, not a conversion from
  the client reasoning byte guard. Zero selects the provider's safe default,
  positive values set an explicit cap, and negative values omit optional wire
  limits. `model_overrides.<model>.max_output_tokens` can specialize mixed
  gateways; Anthropic still supplies a mandatory `max_tokens` fallback.
- Tool-result snip/prune never removes messages, so assistant `tool_calls` and
  tool results stay paired. `KeepErrors` preserves error/blocked tool outputs,
  and the recent tail is not rewritten. Snipped results can later be upgraded to
  pruned placeholders; already-pruned results are left alone.
- When summary compaction runs, it folds only the assistant/tool work. Every
  **user turn** small enough to be a brief and every **prior digest** is kept
  verbatim; the foldable remainder is summarized — using the executor's own
  provider, no tools — in place. The boundary is aligned backward off any tool
  result so the recent tail never begins with an orphan tool message whose
  `tool_calls` were summarized away.
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
- Before each real user turn, bounded BM25 recall selects relevant active facts
  from the raw user message and appends them as a low-authority user-turn suffix.
  Generic turns are suppressed, project facts override equivalent global
  fallbacks, stale facts are down-ranked, and recall is bounded by result/character
  budgets. This never mutates the stable system prompt or tool schemas.
- The owning controller may auto-allow only a bounded, non-sensitive,
  create-only project/reference `remember`, including in a top-level headless
  run. Global facts, preferences, feedback,
  updates, duplicates, sensitive/oversized content, and every `forget` require a
  fresh human approval even under Auto or YOLO. Guardian/safety review cannot
  answer these prompts on the user's behalf. Sub-agents and headless surfaces
  without the owning scoped controller fail closed. The approval request includes a compact preview, while
  external notification hooks only receive the tool name.
- Facts carry immutable IDs, monotonic revisions, timestamps, type, and scope.
  Updates snapshot the previous revision; restore and archive recovery create a
  higher revision and reject path escapes, symlinks, collisions, and overwrites.
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
  and `move_file` renames/moves. Legacy lowercase tool IDs still load for
  compatibility. `Bash=<literal>` is the exact-command form: metacharacters in
  the literal are ordinary characters and only the identical complete command
  matches. The
  `:*` suffix marks a Bash command-prefix approval; generated prefix rules also
  reject later commands that introduce shell operators, so `Bash(go test:*)`
  does not cover `go test ./... && rm -rf tmp`.
  Legacy `Bash(go test *)` prefix rules still load, but new rules are saved as
  `Bash(go test:*)`. The subject is extracted generically from the call's JSON
  args by a small set of
  known keys — `command` (bash), `path` / `file_path` (file tools), `pattern`
  (grep/glob) — so tools need not change. A rule whose subject the args don't
  expose only matches in its bare `Tool` form.
- **Dynamic Bash.** Parameter/arithmetic expansions, assignments, heredocs, unproved
  redirects, and shell globs cannot reuse bare Bash, prefix, or glob allows;
  remembered approvals are exact `Bash=<literal>` rules. They still follow the
  normal posture fallback, so Auto and an approved-plan window may execute them
  without prompting. Nested or indirect execution is stricter: command and
  process substitution, a dynamic command name, parse failures, `eval`,
  `source`, shell `-c`, PowerShell/cmd command strings, and runtime inline-code
  flags require a human in interactive Ask/Auto. Guardian, allowing hooks, and
  the approved-plan window cannot answer that decision; only an identical exact
  grant or YOLO can bypass it by default. The advanced
  `[permissions] allow_dynamic_bash = true` opt-in lets an Allow fallback,
  including Auto, cover this class; explicit `ask` and `deny` rules retain
  precedence.
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
  (`reasonix run`, a sub-agent, anything with no TTY / no approver) cannot prompt.
  Its explicit posture therefore resolves without blocking: Ask/manual fails
  closed, Auto allows only ordinary writer fallback, and YOLO may bypass ordinary
  Ask decisions. Nested or indirect Bash remains stricter: headless
  Ask/Auto/DontAsk reject it unless an identical literal grant exists; YOLO or
  `allow_dynamic_bash = true` with an Allow fallback may opt out. A `Deny` is a
  hard block in *every* mode: the tool never executes and the model receives a
  "blocked" result it can adapt to (the same shape as a plan-mode refusal).
- **MCP authorization.** Installing an MCP server authorizes all of its tools;
  there is no second server, raw-tool, writer, or destructive approval policy.
  Project configuration is trusted the same way and requires no separate launch
  confirmation. Explicit global deny rules still win. `readOnlyHint` and
  `destructiveHint` remain internal
  facts for scheduling, Plan/read-only restrictions, and cached-to-live safety
  reclassification. Strict read-only sub-agent registries expose only
  authorized tools with `readOnlyHint: true` and no `destructiveHint`. The
  two-model Planner uses the fixed `use_capability` proxy (never direct
  `mcp__*` schemas) for authorized, non-destructive MCP without requiring
  `readOnlyHint`; destructive tools are left for the Executor. In Balanced
  two-model sessions the Executor owns an isolated frontend for the same proxy,
  so Planner-discovered capability IDs remain executable after handoff. Schema-only
  changes refresh the next-session cache without adding an execution approval
  or retry. Immediately before dispatch, the proxy re-checks the current
  controller's enablement, authorization, and complete runtime connection
  identity; a same-name client on a shared Host is never sufficient authority.
- **Relationship to plan mode.** Plan mode (§3.4) is a plan-first collaboration
  workflow, not an all-tools read-only mode. Before Permissions/Sandbox, the
  host enforces explicit phase opt-outs (`complete_step` is read-only but
  belongs to the post-approval execution phase, so it self-reports plan-unsafe
  and is refused). The dedicated two-model Planner may call authorized,
  non-destructive MCP even when `readOnlyHint` is absent; it hard-blocks
  destructive targets and readers from unauthorized servers for the entire
  planning phase. A single-model Plan without the dedicated Planner continues
  to block MCP writer/destructive targets while Plan is active.
  Ordinary built-in and Bash calls then use the same Ask/Auto/YOLO, explicit
  `ask`/`deny`, and Sandbox path as Standard mode. A third-party MCP
  `readOnlyHint` affects dispatch classification and strict-child eligibility,
  but not the dedicated Planner's non-destructive trust path. Once the server is
  installed or declared in project configuration, all non-destructive
  capabilities enter the dedicated Planner proxy; only hinted readers enter
  strict read-only sub-agent execution. `plan_mode_read_only_commands` is
  retained for config/session round trips and does not grant or revoke calls in
  the main Plan workflow. `read_only_task` and `read_only_skill` remain strict
  read-only capabilities with their own tool registry and safe foreground Bash;
  writer-capable `task` and skill execution remain permission-gated instead of
  Plan-blocked, and their child turns inherit the Plan workflow marker and
  explicit phase opt-outs.
- **User decisions are separate from tool approvals.** Runtime tool approval has
  three user-facing postures: `ask` ("需要批准"), `auto` ("自动批准"), and
  `yolo` ("Yolo批准"). `auto` lets the permission policy auto-approve the writer
  fallback while preserving explicit ask/deny rules; `yolo` skips ordinary tool
  permission prompts for approval-gated tools such as writers and Bash. Explicit
  deny rules and forced fresh reviews still apply. Nested or indirect Bash
  commands require a human in interactive Ask/Auto even during the approved-plan
  window; ordinary expansions, assignments, redirects, and globs continue under
  Auto fallback but cannot inherit reusable Bash rules. YOLO is the sole mode
  bypass for the human-required class, while an identical exact literal remains
  an ordinary explicit authorization.
  Neither posture answers `ask` questions or approves `exit_plan_mode` plans.
  Plan Mode is entered only through an explicit user choice and remains
  independent of the active tool-approval posture. After a user approves a
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
  does not reset the audit; restarting a goal begins a fresh blocked audit. A
  goal is treated as a task contract: if the objective includes Context,
  Request, Output format, Constraints, or Pause policy sections, those sections
  define the autonomous work boundary. When they are absent, the model infers a
  lightweight contract from the conversation and workspace. The injected goal
  block tells the model to pause only for irreversible or externally visible
  operations, scope changes, or information only the user can provide; ordinary
  uncertainty should be handled with sensible defaults and reported as an
  assumption. Completion requires the concrete request, output format,
  constraints, and relevant verification expectations to be satisfied or
  explicitly reported as unverified.
  Goals that look like long-horizon research, debugging, optimization, or
  implementation work automatically add an AutoResearch protocol to the same
  transient active-goal user block. AutoResearch is a Goal strategy, not a
  standalone global skill: it writes project-local state under
  `.reasonix/autoresearch/YYYYMMDD-HHMMSS-slug/` and keeps dynamic run state out
  of `REASONIX.md`, `AGENTS.md`, project memory, tool schemas, and the
  cache-stable system prompt. `/goal --research <objective>` forces that
  strategy; `/goal --simple <objective>` forces lightweight Goal. Outside goal
  mode, ordinary prompts never change collaboration mode or create durable
  AutoResearch state; the user must choose Goal or use `/goal` explicitly.
  `/goal clear` removes the active goal. Switching into plan/normal mode clears
  the active goal in the desktop UI so the collaboration mode remains one of
  the three choices, while the underlying tool approval posture is preserved.

| Tool approval posture | Tool approvals | Plan approval | `ask` questions |
| --- | --- | --- | --- |
| Need approval / `ask` | Follow permission policy (`Ask` prompts interactively) | Waits for user | Waits for user |
| Auto approve / `auto` | Writer fallback auto-allowed; explicit ask/deny rules still apply | Waits for user | Waits for user |
| YOLO approval / `yolo` | Ordinary prompts auto-allowed; deny rules and fresh reviews remain | Waits for user | Waits for user |
| Approved-plan execution window | Approved plan's writer fallback is auto-allowed; explicit `ask` / `deny` rules remain | Future plans still wait | Waits for user |

Out of the box (`mode = "ask"`, no rules), interactive `reasonix` prompts before
each writer/bash call and `reasonix run` fails closed on those calls because it
has no approver. Use `reasonix run --auto ...` / `-y` to allow ordinary writer
fallback in unattended automation; `--permission-mode auto` is equivalent.
Explicit `ask` rules still fail closed under Auto, and `deny` rules harden every
posture.

### 3.8 Slash commands (`internal/command`)

The chat TUI accepts `/command` input. Three kinds share one dispatch:

- **Built-in actions** (`/compact`, `/new`, `/clear`, `/effort`, `/mcp`, `/help`) manipulate session
  state locally and never reach the model. `/new` starts a new session while
  saving the previous transcript for resume/history. `/clear` requires
  confirmation, then discards the current context without saving it; it does not
  delete project memory.
- **Custom commands** are Markdown files under `.reasonix/commands/` (project) and
  the user config dir, e.g. `~/.reasonix/commands/` on macOS/Linux; the project dir overrides the user dir on a
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

### 3.10 Subagent profiles and explicit CLI execution

A subagent profile is a Skill with `runAs: subagent` and, for profiles managed
by the desktop or CLI editors, `invocation: manual`. Profiles reuse the existing
project/global Skill files; they do not introduce another state format or
database. Manual invocation excludes a profile from the pinned Skill index so
the model cannot discover it implicitly, while explicit `/<name> <task>`
invocation remains available.

Interactive slash invocation and `Controller.RunSubagentProfile` both execute
the profile with the Boot-wired Skill runners. Each run gets an isolated child
session and returns only its final answer to the caller. The headless contract is
explicit:

- `reasonix subagent try <name> ... <task>` uses the read-only Skill runner;
- `reasonix subagent run <name> ... <task>` uses the normal permission and
  sandbox path; and
- ordinary `Controller.Run` / `reasonix run` remains unchanged and does not
  reinterpret slash-prefixed input as a subagent command.

Desktop and CLI profile mutations share
`skill.ValidateEditableSubagentProfile`. Only simple manual project/global
profiles can be rewritten or deleted. Custom-scope Skills, unmanaged
frontmatter, and Skill directories containing `references/` or `scripts/` are
refused so an editor cannot silently flatten or discard rich Skill content.
Built-in profiles support configuration overrides but have no writable file.

Effective model and effort precedence is: per-profile
`agent.subagent_models` / `agent.subagent_efforts`, this call's `model` /
`effort` on `task`/`fleet`, profile frontmatter, `agent.subagent_model` /
`agent.subagent_effort`, then executor/default model configuration.

`task` accepts optional `profile` and `write_paths`. `fleet` dispatches 2–64
profile-aware tasks under a session scheduler
(`agent.max_subagent_concurrency`, default 6; `agent.max_parallel_writers`,
default 3). Profile names are resolved at runtime from the Skill store and
must never enter tool schemas or the parent system prompt. Custom and named
built-in profile bodies are the full child system prompt (no implicit
concise default). `parallel_tasks` remains the compatible read-only batch
API on the same scheduler. In a persisted parent session, parallel/fleet
children save independent transcripts; the aggregate carries bounded previews
and stable refs, and `read_subagent_result` pages a referenced final answer by
UTF-8 byte offset under the current conversation-lineage/workspace boundary.
Headless runs remain ephemeral and return fair bounded previews without refs.
See [Subagent profiles](./SUBAGENT_PROFILES.md)
for the user-facing command and file-format contract.

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
> built-in defaults**. Starting with **Reasonix v1.8.1**, the user config lives
at `~/.reasonix/config.toml` on macOS/Linux and
`%AppData%\reasonix\config.toml` on Windows. See
[Configuration paths](./CONFIG_PATHS.md) for migration and related data paths.
Fields marked user/global only are not overridden by project `reasonix.toml`.
Provider entries name secrets with `api_key_env`; saved key values live in
Reasonix's global `<Reasonix home>/.env`, shared by CLI and desktop. Project
`.env`, home `.env`, inherited shell environment variables, legacy credentials,
and the OS keyring are not provider-key runtime fallbacks. Project `.env` still
feeds workspace-scoped, non-provider `${VAR}` expansion for MCP/plugin settings
without importing provider keys or Reasonix control variables.

```toml
default_model = "deepseek"   # provider name (→ its default model) or "provider/model"
# language    = "zh"                # ui language tag; empty = auto-detect from $LANG / $REASONIX_LANG

[ui]
# shortcut_layout = "desktop"       # classic|desktop; compatibility setting
# cursor_shape = "bar"              # CLI/TUI textarea cursor: underline|block|bar
show_turn_usage = false              # hide per-request token/cost receipts in the TUI; default true

[agent]
system_prompt = "You are Reasonix, a coding agent..."  # or system_prompt_file = "..."
temperature       = 0.0
reasoning_language = "auto"       # visible reasoning text: auto|zh|en
# plan_mode_read_only_commands = ["gh issue view"]   # legacy compatibility only; Plan bash uses Permissions
# planner_model = "deepseek-pro"   # optional: two-model collaboration (low-frequency planner)
# subagent_model = "deepseek-pro"   # optional default for runAs=subagent skills
# subagent_effort = "high"           # optional default reasoning effort for subagents
# subagent_models = { review = "deepseek-pro", security_review = "deepseek-pro" }
# subagent_efforts = { review = "max", security_review = "high" }

# A vendor endpoint exposing several models under one base_url/key.
[[providers]]
name           = "deepseek"
kind           = "openai"
base_url       = "https://api.deepseek.com"
# chat_url     = "https://proxy.example.com/v1/chat/completions"   # optional full chat request URL
# models_url   = "https://proxy.example.com/v1/models"             # optional model discovery URL
models         = ["deepseek-v4-flash", "deepseek-v4-pro"]
default        = "deepseek-v4-flash"   # optional; defaults to models[0]
api_key_env    = "DEEPSEEK_API_KEY"
context_window = 1000000   # tokens; harness compacts older history near this limit (0 disables)
max_output_tokens = 32768  # total visible + reasoning + tool-call output; 0 = provider default
# model_overrides = { "deepseek-v4-flash" = { context_window = 1000000, max_output_tokens = 32768 } }

# A single-model entry still works for custom OpenAI-compatible endpoints.

[environment]
enabled = true   # inject a stable startup summary of OS, shell, and common tool versions
offline = false  # set true when outbound network access is unavailable; prevents futile retries

# Optional trusted executable paths shown to the model when PATH probing is not enough.
# Workspace-local paths are listed but not auto-executed during startup probing.
# [environment.tools]
# go = "/opt/homebrew/bin/go"

[tools]
enabled = []   # omit/empty = all built-ins
bash_timeout_seconds = 120   # foreground safety cap; set 0 for no tool-local cap
mcp_startup_timeout_seconds = 30   # background initialize + tools/list safety cap
mcp_call_timeout_seconds = 300   # default MCP call safety cap; plugin/tool overrides may raise it

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
# workspace_root = ""          # file-writers confined here; empty = cwd
# allow_write    = ["/tmp"]    # extra dirs write_file/edit_file/multi_edit/move_file may modify
# forbid_read    = ["${HOME}/.ssh"]   # paths read/list/search tools and sandboxed bash may not inspect

[serve]
auth_mode = "none"             # none|token|password; use auth before binding beyond localhost
# token = ""                   # optional fixed token; empty token mode generates one at startup
# password_hash = ""           # bcrypt hash generated with reasonix serve --hash-password --password '...'
# behind_proxy = false         # trust X-Forwarded-* only behind a trusted reverse proxy

[[plugins]]
name    = "example"            # type defaults to "stdio"
command = "reasonix-plugin-example"
args    = []
# env   = { FOO = "bar" }
# startup_timeout_seconds = 60         # initialize + tools/list cap; 0 = global/default cap
# call_timeout_seconds = 600            # per-server MCP call timeout; 0 = global/default cap
# tool_timeout_seconds = { "generate_video" = 1800 }   # raw MCP tool names
# [[plugins]]                   # a remote MCP server over Streamable HTTP
# name    = "stripe"
# type    = "http"             # "stdio" (default) | "http" | "sse"
# url     = "https://mcp.stripe.com"
# headers = { Authorization = "Bearer ${STRIPE_KEY}" }   # ${VAR} / ${VAR:-default} expanded
```

The native CLI updater always installs the latest strict `vX.Y.Z` official
release. Legacy channel configuration and arguments remain parseable during
1.x, resolve to the official release, and are omitted on subsequent writes.

The executor tracks an adaptive progress lease while a todo is active. A new
completion, unique successful read, command, or mutation renews the lease;
exact repeats do not. After 8 no-progress tool-call rounds the host appends a
one-shot reassessment nudge. After 16 it pauses and preserves work for a later
user turn. The serial contract is level-aware while preserving the
single-in_progress rule: in a two-level list the active level-1 sub-step is
the only `in_progress` item and its level-0 phase stays `pending`; sub-steps
complete in order, and the phase becomes `in_progress` — and signs off — only
after all of its sub-steps have completed. A level-1 item with no phase above
it is rejected. Retired `[agent].max_steps` and `planner_max_steps` keys remain
parseable for upgrade compatibility, but are ignored and removed by a one-time
migration. The CLI `--max-steps` flag and `[bot].max_steps` remain separate,
explicit controls for one-off and unattended execution.

`reasonix setup` writes this default config so the CLI is usable out of the box.

`[ui].cursor_shape` is normalized to `underline`, `block`, or `bar`; empty or
unknown values fall back to `bar`. It applies to the Bubble Tea CLI/TUI
textarea only, while desktop and browser inputs keep their platform-native
cursor behavior.

`[serve]` controls the HTTP browser frontend used by `reasonix serve`. The
default `auth_mode = "none"` is intended for the loopback default
`127.0.0.1:8787`; deployments reachable from another machine must use `token` or
`password`. Password mode requires either a startup `--password` or a stored
bcrypt `password_hash`. `behind_proxy` must stay false unless the server is
behind a trusted proxy that owns the `X-Forwarded-For` and `X-Forwarded-Proto`
headers.

MCP servers may also be declared in a project-root `.mcp.json` using Claude
Code's exact `mcpServers` schema (`command`/`args`/`env`, `type`/`url`/`headers`,
`${VAR}` expansion). It is read after the TOML files and merged into
`[[plugins]]`; on a name collision `reasonix.toml` wins (it is the more explicit,
Reasonix-specific source). This lets a server already configured for Claude work in
Reasonix unchanged.

MCP startup has a separate lifecycle from an individual tool call. A caller
waits briefly for cold startup, while the shared launch/authorization/
`initialize`/`tools/list` sequence may continue in the background up to
`mcp_startup_timeout_seconds` (default `30`). A per-server
`startup_timeout_seconds` overrides that cap. MCP call timeouts begin only after
the connection is ready.

```json
{ "mcpServers": {
  "stripe": { "type": "http", "url": "https://mcp.stripe.com",
              "headers": { "Authorization": "Bearer ${STRIPE_KEY}" } }
} }
```

`[sandbox]` is the *enforcement* layer beneath permissions (which are *policy*).
Phase 0 confines the file-writing built-ins (`write_file`, `edit_file`,
`multi_edit`, `move_file`) to `workspace_root` (default cwd), the Reasonix user
config dir, plus `allow_write`: a write whose target — resolved to an absolute,
symlink-free path so a symlinked dir or `..` cannot tunnel out — falls outside
every root is refused, and the error is fed back to the model. Confinement is on
by default (root = cwd), so edits stay in the project while the agent can still
update its own global config. `forbid_read` lists files or directories the agent should
not read, list, or search; entries support `${VAR}` / `${VAR:-default}` expansion
and should be absolute, or use `${HOME}` for home-relative secrets such as
`${HOME}/.ssh`. `bash` is itself jailed by default when an OS sandbox is
available (`[sandbox] bash = "enforce"`: Seatbelt on macOS and bubblewrap on
Linux): each command is allowed to write only
the same roots plus platform-specific command temp/cache roots, denied reads
under `forbid_read`, and allowed to reach the network only when
`network = true`.
**Windows status:** Reasonix does not ship an OS-level Bash sandbox on Windows.
The effective mode is fixed to `off`; an older config containing
`bash = "enforce"` remains readable but resolves to `off`, `reasonix doctor`
reports the ignored value, and the desktop control is read-only. Bash therefore
runs unconfined on Windows. The in-process file tools continue to enforce
`workspace_root`, `allow_write`, and `forbid_read`.
When no OS sandbox is available, `bash = "enforce"` refuses bash execution
instead of running unconfined. Install the platform sandbox backend
(bubblewrap/`bwrap` on Linux, `sandbox-exec` on macOS) or set
`[sandbox] bash = "off"` to explicitly restore the pre-1.16 unconfined shell
behavior. The escape-prompt and broader OS support are Phase 1's remainder (§9).

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
  file-writer built-ins (Phase 0) — are confined to the workspace. **Seatbelt on
  macOS and bubblewrap on Linux ship, on by default when available** (see §5).
  Remaining: the escape-prompt — detect sandbox-unavailable or sandbox-denied failures and
  offer an explicit, permission-gated unconfined rerun (in `reasonix run`, the
  command just fails and the model adapts), which completes the "allow inside the
  box, prompt at its edge" model. With this in place, "always allow" rule
  persistence becomes optional rather than load-bearing.
- MCP long tail (deferred deliberately): `headersHelper` auth for remote
  servers; the remaining `.mcp.json` scopes
  (local / user — project scope shipped, see §5); tool-search deferral;
  `list_changed` live updates; channels / elicitation / roots; plugins that
  provide *providers*, not just tools.
- An Anthropic-native provider `kind` (native prompt-cache control), proving the
  registry generalises beyond one wire format.
- "Always allow" persistence writing learned rules back to project config; a
  per-session permission override flag for `reasonix run`.
