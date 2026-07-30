# Tool Contract

<a href="./TOOL_CONTRACT.zh-CN.md">简体中文</a>

This document records the provider-visible contract for Reasonix compile-time built-in tools. It is generated from the same canonical schema path used by the runtime registry.

| Tool | Read-only | Description |
| --- | --- | --- |
| `bash` | false | Execute a command in the shell and return combined stdout/stderr. Use for builds, tests, git, package managers, etc. To search/read/list/edit/move files, prefer the dedicated tools (grep, read_file, ls, glob, edit_file, move_file) over shell grep/cat/ls/find/sed/mv/Move-Item - they behave identically on every OS. For symbol search or architecture questions, prefer LSP/read tools and targeted grep before shell commands. |
| `bash_output` | true | Read new output from a background job started with bash(run_in_background=true) or task(run_in_background=true). Returns the output produced since the last bash_output call for that job, plus its status (running/done/failed/killed). Does not block. |
| `code_index` | true | Lightweight built-in code symbol index. Prefer lsp_* for language semantics and installed code graph MCP tools for call graph, impact, and architecture relationships; use this as the local fallback for file outlines and symbol definition candidates, then verify with read_file or grep. |
| `complete_step` | true | Record the evidence-backed completion of ONE step of an approved plan. Call it as you finish each step instead of silently moving on: it signs the step off with PROOF it is done - the verification you ran (command + result), the diff/files you changed, or a manual check. A completion with no evidence is REJECTED, so don't claim a step is done until you can show why. The host advances the task list for you when you sign off - it marks this step completed and moves the next to in_progress, so you don't need a separate todo_write to mark completions. Fields: `step` (which step - its title or number, matching the task list), `result` (what is now true/changed), `evidence` (>=1 item, each with `kind` = verification\|diff\|files\|manual and a `summary`, plus optional `command`/`paths`), and optional `notes`. |
| `delete_range` | false | Delete a contiguous text range from a file using exact start/end text anchors. Each anchor must match exactly one line. Returns unified diff on success. Use for large deletions - smaller changes should use edit_file. |
| `delete_symbol` | false | Delete a named symbol (function, method, type, interface, const, var) from a Go source file using AST parsing. For non-Go files, use delete_range with manual anchors. |
| `edit_file` | false | Replace an exact string in a file with another. old_string must occur exactly once; add surrounding context to disambiguate. Use for targeted edits instead of rewriting the whole file. |
| `glob` | true | Find files matching a glob pattern (e.g. "*.go", "internal/*/*.go", "**/*.test.ts"). Supports shell metacharacters * ? [] and the recursive ** pattern. |
| `grep` | true | Search for a regular expression in a file, or recursively under a directory (skips hidden files and files matched by .gitignore). Returns matching lines as path:line:text, capped at 200 matches. |
| `kill_shell` | false | Terminate a running background job (bash or task) started with run_in_background. A no-op if the job has already finished or the id is unknown. |
| `ls` | true | List the entries of a directory. Directories are shown with a trailing slash; files show their byte size. Set recursive=true to list all nested files depth-first (skips .git/node_modules). |
| `move_file` | false | Move or rename a file from source_path to destination_path. Creates the destination parent directory as needed. Use instead of shell mv, Move-Item, or ren for file moves so workspace confinement and file-edit permissions apply. |
| `multi_edit` | false | Apply a list of edits to a single file atomically: each edit runs against the result of the previous one, all in memory; the file is rewritten only if every edit succeeds. Cheaper and safer than chaining edit_file calls - a failure in step 3 leaves the file untouched instead of half-edited. |
| `notebook_edit` | false | Edit one cell of a Jupyter notebook (.ipynb). Target a cell by 0-based cell_number (or cell_id). edit_mode: "replace" (default) swaps the cell's source; "insert" adds a new cell after cell_number (use -1 to prepend at the top), taking cell_type and new_source; "delete" removes the cell. cell_type is "code" or "markdown" (required for insert). Editing a code cell clears its outputs. Prefer this over edit_file for notebooks - it keeps the JSON valid. |
| `read_file` | true | Read a text file with optional line offset/limit. Output prefixes each line with its 1-based number so subsequent edit_file calls can target exact lines. Use `offset` and `limit` to page through large files; the tool reports total length and pagination hints in a trailer. |
| `todo_write` | true | Record and update a structured task list for the current work. Send the COMPLETE list every call - it replaces the previous one. Use it to plan multi-step work and show progress: keep exactly one item in_progress at a time, and flip an item to completed the moment it's done (don't batch completions). Skip it for trivial single-step tasks. |
| `wait` | true | Block until background jobs finish, then return each job's status and final output/answer. Use to collect the result of a task(run_in_background) or bash(run_in_background) before continuing. Omit job_ids to wait for every running job. |
| `web_fetch` | true | Fetch a URL over HTTPS/HTTP and return its text content. HTML pages are reduced to readable text; JSON / plain text / markdown bodies come back verbatim. Use to read documentation pages, API responses, or source files hosted somewhere the local filesystem can't reach. |
| `write_file` | false | Write content to a file at the given path (overwriting existing content). Creates parent directories as needed. |

## Schema Snapshot

The exact canonical schemas are intentionally tested in code rather than copied by hand here. Run:

```bash
go test ./internal/tool -run TestBuiltinToolContractDocumentation
```

The test checks that every registered built-in tool has a documented name, read-only flag, description row, and canonical schema generated by `tool.BuiltinContractEntries`.

## Default Full Boot Surface

In a default full-token boot, Reasonix sends the built-in tools above plus the
session, memory, skill, subagent, LSP, install, and slash-command tools below:

Single-model Balanced uses this exact executor tool surface. Balanced with a
distinct Planner and every Delivery session additionally expose one stable
proxy, `use_capability`, so optional MCP servers (including `auto_start=false`)
can be inspected and called without changing provider-visible schemas
mid-session. Delivery also
adds a stable execution contract enforced by the host: state-changing and
verification commands need acceptance criteria; changed work cannot finalize
without post-change review, verification, and an evidence-backed
`complete_step` sign-off; Skill/MCP `require`/`prefer` routes are gated with
host-proven evidence (including read-only answers — ordinary reads never skip
a required capability); and medium/high-risk mutations force structured
`review` / `security_review` results via the review-only `review_report` tool,
whose `reviewed_paths` must be backed by host-observed read/diff receipts.

The two-model Planner and all task/fleet sub-agents also use `use_capability`
(and never direct `mcp__*` schemas). Planner and ordinary writer-capable
sub-agents may call installed or project-configured MCP without
`readOnlyHint`; Planner leaves `destructiveHint` tools for the Executor, while
ordinary sub-agents use the trusted MCP path (live authorization plus explicit
deny only). Writer/destructive calls are still serialized and recorded as
mutations for evidence, workspace leases, and Delivery guards. Strict read-only sub-agents
share the same proxy schema and Host connections but still require
`readOnlyHint` and non-destructive at execution time. Balanced dual-model
attaches independent proxy frontends to both Planner and Executor so a
capability discovered during planning remains directly callable after handoff;
their ledgers/audits are isolated while Host connections are shared. Economy
remains single-model without an independent Planner.

`use_capability` resolution is side-effect free: `action=list` returns sorted
configured MCP servers without starting them; `action=call` on a
not-yet-connected server resolves to a deferred target, Plan re-checks only an
explicit phase opt-out on the real target, and the server process starts only
after the permission gate and PreToolUse hooks approve the call. On-demand children
share the session lifetime (they outlive the starting call and exit with the
session); `action=inspect` lists live tools for connected servers and cached
schemas otherwise, never starting a process. First discovery of a server with
no schema cache goes through `action=call` on the `mcp-server:` id itself: it
resolves to a gated connect (permission name = the server's dedicated
`mcp_connect__<server>` identity, so an exact rule such as
`deny = ["mcp_connect__github"]` blocks process startup) that connects after
approval and returns the live tool directory. MCP tool rules remain exact;
`mcp__github__*` is not a tool-name glob. Installing an MCP authorizes the
Planner to use its non-destructive tools; third-party servers that omit
`destructiveHint` are treated as user-install trust. Before every connect or
`tools/call`, the frontend re-checks the current runtime enablement,
authorization, and exact Host connection identity; another project/tab's
same-name shared client is rejected without process, network, or tool dispatch.

The fixed proxy's provider-visible name, description, schema, and ordering do
not change when MCP inventory changes. Balanced Executor deliberately retains
its direct `mcp__*` tools, so its overall provider prefix may still change when
those direct tools are installed, connected, or refreshed.

`ask`, `explore`, `fleet`, `forget`, `history`, `install_skill`, `install_source`,
`list_sessions`, `lsp_definition`, `lsp_diagnostics`, `lsp_hover`,
`lsp_references`, `memory`, `parallel_tasks`, `read_only_skill`,
`read_only_task`, `read_session`, `read_skill`, `remember`, `research`,
`review`, `run_skill`, `security_review`, `slash_command`, `task`.

`use_capability` (`action` = `list` | `inspect` | `call` | `decline`): Delivery
Executor, plus both Planner and Executor in Balanced dual-model sessions; not
enabled in Economy.

`internal/boot.TestBootToolContractMatchesProviderVisibleSurface` verifies the
actual boot registry contract against the provider request, including read-only
flags and canonical schemas.

## Token Economy Boot Surface

In token economy mode, Reasonix starts with nine tools: four direct coding tools,
the three background-shell lifecycle tools, `ask`, and the connector used to
enable optional sources on demand:

`ask`, `bash`, `bash_output`, `connect_tool_source`, `edit_file`, `kill_shell`,
`read_file`, `wait`, `write_file`.

Everything else is explicit and on demand. `connect_tool_source` supports
`search` (`code_index`, `glob`, `grep`, `ls`), `files` (specialized move,
multi-edit, delete, and notebook tools), `workflow` (`todo_write`,
`complete_step`), `sessions` (`history`, `list_sessions`, `read_session`),
`memory` (`memory`, `remember`, `forget`), `commands` (`slash_command`),
`skills`, `read_only_skill`, `mcp`, `lsp`, `web_fetch`, `install_source`,
`task`, and `read_only_task`. Every source may be connected in Plan; subsequent
reader and writer calls use the same Permissions/Sandbox path as Standard mode.
`workflow` is the phase-specific exception: while planning it installs only
`todo_write`; `complete_step` joins on a fresh `workflow` connect after plan
approval. Use `bash` for listing and search until the dedicated `search` source
is needed.
