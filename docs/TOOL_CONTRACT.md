# Tool Contract

<a href="./TOOL_CONTRACT.zh-CN.md">简体中文</a>

This document records the provider-visible contract for Reasonix compile-time built-in tools. It is generated from the same canonical schema path used by the runtime registry.

| Tool | Read-only | Description |
| --- | --- | --- |
| `bash` | false | Execute a command in the shell and return combined stdout/stderr. Use for builds, tests, git, package managers, etc. To search/read/list/edit/move files, prefer the dedicated tools (grep, read_file, ls, glob, edit_file, move_file) over shell grep/cat/ls/find/sed/mv/Move-Item - they behave identically on every OS. For symbol search or architecture questions, prefer LSP/read tools and targeted grep before shell commands. |
| `browser_attach_page` | false | Attach a browser tab by its tabId so it becomes readable and operable. The tab becomes the active CDP target. Use browser_list_pages first to find the tabId. Attaching grants read/operate access to that tab, so only attach tabs the user has approved. |
| `browser_attached_pages` | true | List the browser tabs the user has explicitly attached to this session. Returns an array with tabId, URL, title, and which one is the active CDP target. Use this to know which pages you may operate on before calling browser_navigate/browser_click/etc. |
| `browser_click` | false | Click an element in the current page by CSS selector. The element is scrolled into view before clicking. Use browser_read or browser_read_dom first to discover page content and identify the right selector. Prefer this over browser_eval for simple clicks. |
| `browser_close_page` | false | Close a browser tab by its tabId. Use browser_list_pages first to discover the tab ID. The last open tab cannot be closed. If the closed tab was the active debugger target, another tab will be auto-attached. |
| `browser_drag` | false | Drag an element onto another element. Uses CDP Input.dispatchMouseEvent to simulate mouse down, move, and up. Provide the source CSS selector (what to drag) and the target CSS selector (where to drop). |
| `browser_emulate` | false | Emulate a device viewport on the current page. Use this to test responsive layouts by setting viewport width, height, device scale factor, and mobile mode. Call browser_resize to reset to default. |
| `browser_eval` | false | Execute arbitrary JavaScript in the current page context. Returns the result as a JSON string. Use this ONLY when other tools cannot achieve what you need - prefer browser_click, browser_fill_form, browser_read, etc. for standard operations. The expression must return a JSON-serializable value. |
| `browser_fill_form` | false | Fill multiple form fields at once by providing a map of CSS selectors to values. This is the PREFERRED tool for filling forms - it's faster and more reliable than calling browser_fill_form repeatedly per field. Handles inputs, textareas, select dropdowns, checkboxes, and radio buttons in one call. |
| `browser_go_back` | false | Navigate back to the previous page in browser history. Equivalent to clicking the browser back button. Use browser_read after navigating to confirm the page changed. |
| `browser_go_forward` | false | Navigate forward to the next page in browser history. Equivalent to clicking the browser forward button. Only works after browser_go_back. Use browser_read to confirm the page changed. |
| `browser_handle_dialog` | false | Accept or dismiss a JavaScript dialog (alert, confirm, or prompt). By default accepts the dialog. For prompt dialogs, provide the text to enter. Use this when a dialog is blocking page interaction - browser_click and other tools will fail until the dialog is handled. |
| `browser_hover` | false | Hover the mouse over an element identified by a CSS selector. Use this to trigger hover effects, tooltips, or dropdown menus that appear on hover. After hovering, use browser_read_dom to discover newly visible elements. The element is scrolled into view before hovering. |
| `browser_list_console_messages` | true | List console messages logged by the current page since the last navigation. Returns an array of messages with level, text, source, and line number. Use this to debug JavaScript errors, warnings, and logs. Pass clear=true to clear the cache after reading. |
| `browser_list_network_requests` | true | List network requests made by the current page. Returns an array of requests with URL, method, status code, type, and headers. Use this to see what API calls the page is making, debug failed requests, or inspect request/response details. Pass clear=true to clear the cache after reading. |
| `browser_list_pages` | true | List all open browser tabs. Returns an array of pages with their index, tabId, title, URL, and active status. Use this first to discover what pages are open, then use browser_select_page to switch to a specific tab. PREFERRED tool for discovering open tabs. |
| `browser_navigate` | false | Navigate the current browser tab to the specified URL. Waits for the page to finish loading before returning. After navigation, use browser_read or browser_read_dom to confirm the page loaded correctly. If the page doesn't load, check the URL format. |
| `browser_new_page` | false | Open a new browser tab with an optional URL. The new tab becomes the active debugger target. Use this to open a new page without affecting the current tab. If no URL is given, opens about:blank. |
| `browser_press_key` | false | Press a key or key combination in the currently focused element. Use this AFTER typing text with browser_fill_form or browser_type to submit forms (Enter), navigate (Tab, Escape), or trigger keyboard shortcuts (Control+A, Control+C, Shift+Tab). Supports: Enter, Tab, Escape, ArrowUp/Down/Left/Right, Backspace, Delete, Home, End, F1-F12, and modifier combinations. |
| `browser_read` | true | Read the visible text content of the current page. Returns the page title, URL, and body text. This is the PREFERRED tool for reading page content. Use a CSS selector to read only a specific element. For interactive elements (buttons, links, inputs), use browser_read_dom instead. |
| `browser_read_dom` | true | Read all interactive elements on the current page (buttons, links, inputs, selects, textareas) with their CSS selectors, text content, and bounding rectangles. This is the PREFERRED tool for discovering what clickable elements exist on a page before using browser_click, browser_fill_form, or browser_type. Use this instead of browser_read when you need to interact with the page. |
| `browser_resize` | false | Resize the page viewport to the specified width and height in pixels. Use this to test responsive layouts or to ensure the page is rendered at a specific size before taking a screenshot. Default device scale factor is 1. |
| `browser_scroll` | false | Scroll the page by a specified number of pixels, or scroll an element into view using a CSS selector. Use this when content is below the fold. PREFER using the selector parameter to scroll to a specific element rather than guessing pixel values. |
| `browser_screenshot` | true | Take a screenshot of the current page. Returns a base64-encoded image. PREFERRED format is JPEG for smaller size (use format='jpeg'). Use this to visually inspect the page layout, but prefer browser_read or browser_read_dom for text content since the AI can read text directly. |
| `browser_select_page` | false | Switch the browser debugger focus to a specific attached tab by its tabId. Call browser_attached_pages first to see which tabs are attached and their IDs. Only attached tabs can be selected. |
| `browser_status` | true | Check if the browser relay extension is connected and authorized. Always call this first before using any other browser tool to verify the connection state. Returns the server address, connection state, and extension info. |
| `browser_take_snapshot` | true | Take a text snapshot of the current page based on the accessibility tree. Returns structured elements with roles, names, and unique IDs. Use this to discover page structure and interactive elements when browser_read_dom is not detailed enough. |
| `browser_type` | false | Type text into an input field by CSS selector. Clears existing content before typing by default. Use browser_fill_form for a simpler alternative that also handles select dropdowns. Use browser_press_key for special keys like Enter or Tab after typing. |
| `browser_upload_file` | false | Upload a file to a file input element on the page. The file path must be an absolute path on the local machine. Use this for file upload forms. Requires the page to have a file input element. |
| `browser_wait` | true | Wait for an element to appear on the page, identified by a CSS selector. Use this after browser_navigate or browser_click to wait for dynamic content to load. By default waits up to 5 seconds and checks if the element is visible. Returns true if found, false if timeout. PREFER this over fixed delays. |
| `bash_output` | true | Read new output from a background job started with bash(run_in_background=true) or task(run_in_background=true). Returns the output produced since the last bash_output call for that job, plus its status (running/done/failed/killed). Does not block. |
| `code_index` | true | Lightweight built-in code symbol index. Prefer lsp_* for language semantics and installed code graph MCP tools for call graph, impact, and architecture relationships; use this as the local fallback for file outlines and symbol definition candidates, then verify with read_file or grep. |
| `complete_step` | true | Record the evidence-backed completion of ONE step of an approved plan. Call it as you finish each step instead of silently moving on: it signs the step off with PROOF it is done - the verification you ran (command + result), the diff/files you changed, or a manual check. A completion with no evidence is REJECTED, so don't claim a step is done until you can show why. The host advances the task list for you when you sign off - it marks this step completed and moves the next to in_progress, so you don't need a separate todo_write to mark completions. Fields: `step` (which step - its title or number, matching the task list), `result` (what is now true/changed), `evidence` (>=1 item, each with `kind` = verification\|diff\|files\|manual and a `summary`, plus optional `command`/`paths`), and optional `notes`. |
| `compress` | true | Compress a selected part of the current model-visible conversation without deleting visible history. Use only when the user explicitly asks for context compression. Choose `before` to summarize everything before the uniquely matched user turn while keeping that turn and later context, or `after` to summarize from that turn through the last completed turn while keeping the active turn. The anchor must be an exact, unique excerpt from a real user message; use a longer excerpt if the tool reports multiple matches. |
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
| `update_goal` | true | Report this turn's disposition for the active goal: `continue` (work is ongoing - give a concrete next_action), `complete` (the request is done and verification was attempted or reported unavailable), or `blocked` (only the user can unblock). An optional `completion` account may accompany `complete`: `verified` commands are reconciled against the session's real receipts, while `unverified` and `risks` are declarations the host cannot infer and do not block Light/Balanced completion. The host validates the claim against Delivery acceptance criteria and budget and decides whether to continue automatically. Outside an active goal turn the call fails closed without changing any state. |
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

Every session uses this exact executor tool surface plus one stable
proxy, `use_capability`, so optional MCP servers (including `auto_start=false`)
can be inspected and called without changing provider-visible schemas
mid-session. The host also
enforces a risk-adaptive execution contract: state-changing and
verification commands need acceptance criteria when the turn is closed-loop;
changed work cannot finalize
without post-change review, verification, and an evidence-backed
`complete_step` sign-off; Skill/MCP `require`/`prefer` routes are gated with
host-proven evidence (including read-only answers — ordinary reads never skip
a required capability); and medium/high-risk mutations force structured
`review` / `security_review` results via the review-only `review_report` tool,
whose `reviewed_paths` must be backed by host-observed read/diff receipts.

## Unified Boot Surface

Every session uses the same provider-visible core tools and the same
`use_capability` proxy.

The two-model Planner and all task/fleet sub-agents also use `use_capability`
(and never direct `mcp__*` schemas). Planner and ordinary writer-capable
sub-agents may call installed or project-configured MCP without
`readOnlyHint`; Planner leaves `destructiveHint` tools for the Executor, while
ordinary sub-agents use the trusted MCP path (live authorization plus explicit
deny only). Writer/destructive calls are still serialized and recorded as
mutations for evidence, workspace leases, and closed-loop guards. Strict read-only sub-agents
share the same proxy schema and Host connections but still require
`readOnlyHint` and non-destructive at execution time. Dual-model
attaches independent proxy frontends to both Planner and Executor so a
capability discovered during planning remains directly callable after handoff;
their ledgers/audits are isolated while Host connections are shared. A
single-model session has no independent Planner.

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
not change when MCP inventory changes.

When the current frontend has a session reader, the same fixed proxy also lists
the read-only `session:tool_result` capability. It pages the complete local copy
of one tool result by UTF-8 byte offset without adding a top-level schema. Calls
require `tool_call_id`; new truncation markers also provide a stable
`result_ref`, which is required to disambiguate repeated call IDs. `offset`
defaults to 0, `limit` defaults to 16KiB and is capped at 24KiB. Each response
starts with `result_ref`, actual offset, `next_offset`, `total_bytes`, full
SHA-256, and `complete`, followed by the raw page. The reader is bound to the
current Agent session and is not inherited from a parent when a capability
frontend is cloned. A restricted child that already has `use_capability` may
read only its own results; an allowed-tools profile without the proxy is not
widened.

`ask`, `docs`, `explore`, `fleet`, `forget`, `history`, `install_skill`, `install_source`,
`list_sessions`, `lsp_definition`, `lsp_diagnostics`, `lsp_hover`,
`lsp_references`, `memory`, `parallel_tasks`, `read_only_skill`,
`read_only_task`, `read_session`, `read_skill`, `read_subagent_result`, `remember`, `research`,
`review`, `run_skill`, `security_review`, `slash_command`, `task`.

`parallel_tasks` and `fleet` keep their combined result below the single-tool
output limit by returning a fair preview and a stable `Subagent reference` for
every persisted child. `read_subagent_result` pages through one referenced
final answer by UTF-8 byte offset, so long parallel research remains lossless
without injecting every report into the parent context at once. References are
restricted to the current conversation lineage and workspace.

`use_capability` (`action` = `list` | `inspect` | `call` | `decline`) is on the
provider-visible surface for every task. Host verification obligations come
from real tool actions, not from preclassifying the prompt. Optional tools stay registered for host dispatch but are not
expanded into the top-level provider schema; the model reaches them through
`use_capability` without cache-breaking schema churn.

`internal/boot.TestBootToolContractMatchesProviderVisibleSurface` verifies the
actual boot registry contract against the provider request, including read-only
flags and canonical schemas.

## Unified Boot Surface (every task)

Every task starts with the same lean provider-visible core: direct
coding tools, background-shell lifecycle tools, and the stable capability proxy:

`bash`, `bash_output`, `edit_file`, `kill_shell`, `read_file`, `wait`,
`write_file`, `compress` (when registered), and `use_capability`.

Optional tools (`glob`, `grep`, `ls`, `web_fetch`, MCP, skills, subagents, docs,
session history, memory mutation, workflow, and so on) remain in the host
registry for dispatch. The model lists, inspects, calls, or declines them via
`use_capability` without changing the provider tool list. Task risk changes host
planning, verification, and review policy, not which tools appear on the
provider-visible surface. The retired `connect_tool_source` path is no longer registered.
