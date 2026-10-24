---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-09-29
---

# Tool Contract

This document records the provider-visible contract for Reasonix compile-time built-in tools. It is generated from the same canonical schema path used by the runtime registry.

`present_files` declares up to four existing files in the workspace or authorized folders.
Its versioned result remains in the canonical tool history; live and restored cards use
the host-confirmed capability identity and the complete result, even when model context
is shortened.

Opening a card reads the current source file, not an immutable snapshot.
It does not certify task completion and is not admitted by the Observe posture.

`present_files` 每次声明最多四个工作区或已授权目录中的现有文件。有版本的结果保存在原始工具历史中；
实时与恢复后的卡片使用宿主确认的能力身份和完整结果，不依赖模型上下文是否被压缩。
点击卡片读取源文件当前内容，不保存版本快照，不构成任务完成证明，也不进入 Observe 模式。

This tool is enabled by default. User-only `tools.present_files = false` disables
it on session open, removing it from the registry and provider request. Existing
explicit choices are preserved. The unconditional builtin budget remains 7,185
tokens; the conditional presenter schema is additional when enabled.

Boot effect tests
verify absence while off, the exact enabled schema, denied reads, direct and
capability-proxy calls, and stable schemas within a turn. Viewers remain available.

该工具默认开启；用户配置 `tools.present_files = false` 在打开会话时禁用，
使其不进入注册表或模型请求。已有的显式选择会保留。
无条件内置工具预算保持 7,185 tokens；启用时另外加入文件交付工具定义。
Boot 效果测试覆盖关闭时缺席、启用时精确的 schema、读取拒绝、直接调用、能力代理及轮内稳定性。
文件预览不受此开关影响。

| Tool | Read-only | Description |
| --- | --- | --- |
| `bash` | false | Execute a command in the shell and return combined stdout/stderr. Use for builds, tests, git, package managers, etc. To read or modify a file use read_file / edit_file / write_file — never Get-Content, cat, sed, head/tail or a shell redirect. read_file returns up to 2000 lines per call, each prefixed with its line number, and those numbers are what edit_file targets; paging a file through the shell instead costs one round trip per chunk and yields no line anchors. |
| `bash_output` | true | Read new output from a background job started with bash(run_in_background=true) or task(run_in_background=true). Returns the output produced since the last bash_output call for that job, plus its status (running/done/failed/killed). Does not block. Omit job_id to list the jobs still running, with their kind, label and elapsed time. |
| `browser_act` | false | Operate a page with real input. Ordered steps stop at the first failure; target by ref, or screenshot x/y. Returns results, page errors and changes. fill replaces, type inserts; press takes keys like Enter or Control+a; select sets a <select>; scroll takes delta_y or delta_x; drag ends at to_ref or to_x/to_y; upload gives a file input workspace files; dialog answers prompts; eval executes arbitrary page JavaScript from script and awaits promises; resize sets the viewport to width×height; back, forward and reload move this tab. |
| `browser_open` | false | Load a page in your browser and return its snapshot: one element per line, with [eN] refs for browser_act. url is http(s), about:blank or a workspace file. Without url, tab switches to that tab; close closes it. |
| `browser_read` | true | Read a browser page: snapshot (default; page with offset/limit, or ref for one subtree), find (lines carrying text, with their refs), screenshot (browser_act x/y use its pixels, and the person is shown it too), logs (errors since you last read them), network (what it requested and what came back; text narrows by URL), tabs. |
| `code_index` | true | Lightweight built-in code symbol index. Prefer lsp_* for language semantics and installed code graph MCP tools for call graph, impact, and architecture relationships; use this as the local fallback for file outlines and symbol definition candidates, then verify with read_file or grep. |
| `complete_step` | true | Record the evidence-backed completion of ONE step of an approved plan. Call it as you finish each step instead of silently moving on: a completion with no evidence is REJECTED, so don't claim a step is done until you can show why. The host advances the list for you — it marks this step completed and moves the next to in_progress, so a todo_write that marks such an item completed on its own is REJECTED too. Sign off one step per tool-call ROUND, not one per call: the sign-off promotes the next item, and a second sign-off sent beside it signs against the list as it stood before. |
| `compress` | true | Compress a selected part of the current model-visible conversation without deleting visible history. Use only when the user explicitly asks for context compression. Choose `before` to summarize everything before the uniquely matched user turn while keeping that turn and later context, or `after` to summarize from that turn through the last completed turn while keeping the active turn. The anchor must be an exact, unique excerpt from a real user message; use a longer excerpt if the tool reports multiple matches. |
| `computer_act` | false | Operate an approved application through CUA Driver. launch must be one separate step using an exact installed identity from computer_read what=apps; no window selector or extra arguments. Select window when the app has multiple windows. Use refs from its latest snapshot or coordinates from its latest screenshot. Background clicks, set_value, type/paste, key and scroll never silently escalate. pointer_click explicitly takes foreground control and requires separate approval. wait is bounded. Inspect fresh state after an unverified effect; never assume an action landed. Returns a snapshot afterwards except launch, which returns the verified running identity for a separately approved read. |
| `computer_read` | false | See an approved application listed by what=apps. Installed entries without a targetable window provide an exact launch identity for a separate computer_act launch step. Select window by id when it has multiple windows. snapshot returns session-bound element refs in brackets for computer_act. screenshot captures that exact window; computer_act x/y use pixels of this latest displayed image. Old tokens and captures expire on refresh or reconnect. |
| `context_budget` | true | Report how much context room is left before this conversation is automatically compacted. Nothing reports it unprompted: check it at the start of a long task, before a large change, and before output you may not finish reading (a broad search, a large file, a long build log), so you can narrow the command. `tokens_remaining` counts down to the compaction trigger, not the physical window. |
| `delete_range` | false | Delete a contiguous text range from a file using exact start/end text anchors. Each anchor must match exactly one line. Returns unified diff on success. Use for large deletions — smaller changes should use edit_file. |
| `delete_symbol` | false | Delete a named symbol (function, method, type, interface, const, var) from a Go source file using AST parsing. For non-Go files, use delete_range with manual anchors. |
| `edit_file` | false | Replace an exact string in a file with another. old_string must occur exactly once; add surrounding context to disambiguate. Use for targeted edits instead of rewriting the whole file. |
| `glob` | true | Find files matching a glob pattern (e.g. "*.go", "internal/*/*.go", "**/*.test.ts"). Supports shell metacharacters * ? [] and the recursive ** pattern. |
| `grep` | true | Search for a regular expression in a file, or recursively under a directory (skips hidden files and files matched by .gitignore). Returns matching lines as path:line:text, capped at 200 matches. Ignored paths are searched only when nothing else matched; such matches say so, so an empty answer means absent rather than filtered. |
| `import_web_image` | false | Import web_fetch image_url from source url into new path. Validates PNG/JPEG/GIF/WebP. Returns image {path,alt,source} for render_surface. Match extension. |
| `kill_shell` | false | Terminate a running background job (bash or task) started with run_in_background. A no-op if the job has already finished or the id is unknown. |
| `ls` | true | List the entries of a directory. Directories are shown with a trailing slash; files show their byte size. Set recursive=true to list all nested files depth-first (skips .git/node_modules). |
| `move_file` | false | Move or rename a file from source_path to destination_path. Creates the destination parent directory as needed. Use instead of shell mv, Move-Item, or ren for file moves so workspace confinement and file-edit permissions apply. |
| `multi_edit` | false | Apply a list of edits to a single file atomically: each edit runs against the result of the previous one, all in memory; the file is rewritten only if every edit succeeds. Cheaper and safer than chaining edit_file calls — a failure in step 3 leaves the file untouched instead of half-edited. |
| `notebook_edit` | false | Edit one cell of a Jupyter notebook (.ipynb). Target a cell by 0-based cell_number (or cell_id). edit_mode: "replace" (default) swaps the cell's source; "insert" adds a new cell after cell_number (use -1 to prepend at the top), taking cell_type and new_source; "delete" removes the cell. cell_type is "code" or "markdown" (required for insert). Editing a code cell clears its outputs. Prefer this over edit_file for notebooks — it keeps the JSON valid. |
| `read_file` | true | Read a text file with optional line offset/limit. Output prefixes each line with its 1-based number (e.g. `   42→...`) so subsequent edit_file calls can target exact lines. Use `offset` and `limit` to page through large files; the tool reports total length and pagination hints in a trailer. |
| `read_video` | true | Look at a video: native if the model watches video, else ffmpeg frames. |
| `recall` | true | Bring folded conversation back: read the #n positions listed under "Folded work index" in a compaction summary, or search the whole folded region by query when no index line names what you need. Prefer re-reading a file over recalling it — a current copy beats a folded one. One budget per compaction covers both, and a request past it is refused whole, not truncated. |
| `render_chart` | true | Show the user a chart inline. Rows hold only numbers, strings and nulls; no URLs, markup or colours. Returns a chart_id and a text summary, not the rows. |
| `todo_write` | true | Record and update a structured task list for the current work. Send the COMPLETE list every call — it replaces the previous one. Use it to plan multi-step work and show progress: keep exactly one item in_progress at a time, and flip an item to completed the moment it's done (don't batch completions). Skip it for trivial single-step tasks. The list is two-level: a `level` 0 item is a PHASE (a milestone) and the `level` 1 items after it are its concrete sub-steps. COPY `step_id` VERBATIM for every item that already has one: it is how a completion stays attached to its step when you retitle it, insert a step above it, or reorder the list. Give a new item a fresh unique id (e.g. "plan_step_07"); never reuse or renumber an existing one. |
| `update_goal` | true | Report this turn's disposition for the active goal. Call it at the end of every goal turn instead of using prose markers: `continue` (work is ongoing — give a concrete next_action), `complete` (the request is fully done, output format and constraints satisfied, and verification was attempted or reported unavailable), or `blocked` (only the user can unblock: missing user-only information, an irreversible/externally visible operation, or changed scope). The host validates your claim against Delivery acceptance criteria and decides whether to continue automatically. |
| `wait` | true | Block until background jobs finish, then return each job's status and final output/answer. Use to collect the result of a task(run_in_background) or bash(run_in_background) before continuing. Omit job_ids to wait for every running job. |
| `web_fetch` | true | Read HTTP(S) text. Reduces HTML to text and image candidates for import_web_image; returns JSON/plain text verbatim. |
| `write_file` | false | Write full file content, overwriting and creating parent directories. Prefer edit_file/multi_edit for partial changes. |

## Schema Snapshot

The exact canonical schemas are intentionally tested in code rather than copied by hand here. Run:

```bash
go test ./internal/contract/tool -run TestBuiltinToolContractDocumentation
```

The test checks that every registered built-in tool has a documented name, read-only flag, description row, and canonical schema generated by `tool.BuiltinContractEntries`.

## Default Full Boot Surface

In a default full-token boot, Reasonix sends the built-in tools above plus the
session, memory, skill, subagent, LSP, install, and slash-command tools below:

Single-model Balanced uses this exact executor tool surface.

Balanced with a distinct Planner and every Delivery session additionally expose one stable proxy, `use_capability`, so optional MCP servers (including `auto_start=false`) can be inspected and called without changing provider-visible schemas mid-session.

Delivery also adds a stable execution contract enforced by the host:

- State-changing and verification commands need acceptance criteria.
- Changed work cannot finalize without post-change review, verification, and an evidence-backed `complete_step` sign-off.
- Skill/MCP `require`/`prefer` routes are gated with host-proven evidence (including read-only answers — ordinary reads never skip a required capability).
- Medium/high-risk mutations force structured `review` / `security_review` results via the review-only `review_report` tool, whose `reviewed_paths` must be backed by host-observed read/diff receipts.

The two-model Planner and all task/fleet sub-agents also use `use_capability` (and never direct `mcp__*` schemas).

Planner and ordinary writer-capable sub-agents may call installed or project-configured MCP without `readOnlyHint`; Planner leaves `destructiveHint` tools for the Executor, while ordinary sub-agents use the trusted MCP path (live authorization plus explicit deny only).

Writer/destructive calls are still serialized and recorded as mutations for evidence, workspace leases, and Delivery guards. Strict read-only sub-agents share the same proxy schema and Host connections but still require `readOnlyHint` and non-destructive at execution time.

Balanced dual-model attaches independent proxy frontends to both Planner and Executor so a capability discovered during planning remains directly callable after handoff; their ledgers/audits are isolated while Host connections are shared. Economy remains single-model without an independent Planner.

`use_capability` resolution is side-effect free:

- `action=list` returns sorted configured MCP servers without starting them.
- `action=call` on a not-yet-connected server resolves to a deferred target, and Plan re-checks only an explicit phase opt-out on the real target.
- The server process starts only after the permission gate and PreToolUse hooks approve the call.

On-demand children share the session lifetime (they outlive the starting call and exit with the session); `action=inspect` lists live tools for connected servers and cached schemas otherwise, never starting a process.

First discovery of a server with no schema cache goes through `action=call` on the `mcp-server:` id itself.

It resolves to a gated connect (permission name = the server's dedicated `mcp_connect__<server>` identity, so an exact rule such as `deny = ["mcp_connect__github"]` blocks process startup) that connects after approval and returns the live tool directory.

MCP tool rules remain exact; `mcp__github__*` is not a tool-name glob. Installing an MCP authorizes the Planner to use its non-destructive tools; third-party servers that omit `destructiveHint` are treated as user-install trust.

Before every connect or `tools/call`, the frontend re-checks the current runtime enablement, authorization, and exact Host connection identity; another project/tab's same-name shared client is rejected without process, network, or tool dispatch.

The fixed proxy's provider-visible name, description, schema, and ordering do
not change when MCP inventory changes. Balanced Executor deliberately retains
its direct `mcp__*` tools, so its overall provider prefix may still change when
those direct tools are installed, connected, or refreshed.

```text
ask, await_user, docs, explore, fleet, forget, history, install_skill, install_source, list_sessions, list_subagents, lsp_definition, lsp_diagnostics, lsp_hover, lsp_references, memory, parallel_tasks, read_only_skill, read_only_task, read_session, read_skill, read_subagent_result, remember, research, review, run_skill, security_review, slash_command, task.
```

`parallel_tasks` and `fleet` keep their combined result below the single-tool output limit by returning a fair preview and a stable `Subagent reference` for every persisted child.

`read_subagent_result` pages through one referenced final answer by UTF-8 byte offset, so long parallel research remains lossless without injecting every report into the parent context at once.

`list_subagents` recovers the references themselves when the aggregate that carried them never arrived, which is what an interrupted background fleet leaves behind. References are restricted to the current conversation lineage and workspace.

`use_capability` (`action` = `list` | `inspect` | `call` | `decline`) is on the provider-visible surface for every execution setting (`balanced` | `delivery`).

Optional tools stay registered for host dispatch but are not expanded into the top-level provider schema; the model reaches them through `use_capability` without cache-breaking schema churn.

`internal/assembly/boot.TestBootToolContractMatchesProviderVisibleSurface` verifies the
actual boot registry contract against the provider request, including read-only
flags and canonical schemas.

## Unified Boot Surface (all execution settings)

Every execution setting starts with the same lean provider-visible core: direct
coding tools, background-shell lifecycle tools, and the stable capability proxy:

`bash`, `bash_output`, `edit_file`, `kill_shell`, `read_file`, `wait`,
`write_file`, `compress`, `recall` and `context_budget` (when registered), and
`use_capability`.

The host-control tools ride the same surface at every setting: `todo_write` and `complete_step` for the task list, and `ask` and `await_user` for the turns that end on the model's terms.

`setting` finds the user's settings and asks the user to confirm a change. A refusal that a setting would lift ends with `(fix: setting ...)`.

`conclude_blocked` is registered but not yet on this surface, so it is reachable only through `use_capability` even where the host names it as a way out of an unfinished turn.

Optional tools (`glob`, `grep`, `ls`, `web_fetch`, MCP, skills, subagents, docs, session history, memory mutation, workflow, and so on) remain in the host registry for dispatch. The model lists, inspects, calls, or declines them via `use_capability` without changing the provider tool list.

Execution settings change host planning / verification / review policy, not which tools appear on the provider-visible surface. The retired `connect_tool_source` path is no longer registered.
