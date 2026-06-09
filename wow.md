# WOW.md — How We Fix Bugs

> The reproducible bug-fix workflow for Reasonix Code. Follow this for every issue.

## Language rule

**All code comments must be in English.** The codebase is maintained by an international community — Chinese comments create a barrier for non-Chinese contributors reading the diff, reviewing PRs, or debugging regressions. This applies to every file (Go, TypeScript, tests, configs). Project documentation (`priority.md`, issue templates) can use Chinese where appropriate, but source code comments are always English.

---

## Phase 0 — IS THIS A BUG? (Think before you act)

Before touching any code, classify the issue into one of three categories.
Each demands a different response.

### 0a. The Three-Way Classification

| Category | Signal | Examples | Response |
|:---------|:-------|:---------|:---------|
| **🔴 Genuine bug** | Behavior contradicts a documented invariant, a type contract, or a package comment. Reproduces on clean default config. | Todo progress stuck at 0/3 when items are completed (#3633). Model auto-refresh overwrites user edits (#3632). | Fix the code. |
| **🟡 Design tension** | Behavior is *intentional* in the current design, but the design choice conflicts with a reasonable user expectation. | Yolo mode auto-approves `ask_choice` by design — the bypass flag was meant to skip everything (#3624). Desktop auto-refreshes models on key save by design — but reporter wants manual control (#3632). | Propose a design refinement. May need `submit_plan`. |
| **🟢 Environment / platform limitation** | Only reproduces with specific third-party software, OS boot timing, or cross-platform shell incompatibility. | Desktop freeze with Tencent ACE + Everything on fast startup (#3631). msys2 bash can't capture Windows .exe output (#3620). | Close as `not planned`. Document in `REASONIX.md`. |

### 0b. How to Classify — The Diagnostics Checklist

```
☐ Does the behavior match what the types/interfaces promise?

   Read the relevant interface or struct. Does the observed behavior
   violate its contract? E.g., `todo_write` is `ReadOnly() = true`
   (internal/tool/builtin/todo.go:82) — if a bug is about the tool
   corrupting state, that's a genuine bug. If it's about the TUI
   not rendering the tool's output correctly, that's a *different*
   bug in a different layer.

☐ Can you reproduce with default config on a clean system?

   Strip all plugins, custom skills, third-party MCP servers.
   Fresh `~/.reasonix/` state. If the bug disappears, it's
   environment or config interaction. If it persists, it's code.

☐ Is the behavior "by design" in the bypass/yolo/plan-mode code?

   Read `control.Controller.SetBypass()` and `requestApproval()`
   (internal/control/controller.go ~790). Yolo mode is INTENDED
   to auto-approve — exempting ask_choice from it is a design
   decision you're proposing, not a bug you're fixing.

☐ Does the reproduction depend on OS boot timing, third-party
   software, or cross-platform runtime differences?

   #3631 freeze only happens "after boot with Tencent ACE running."
   #3620 only happens in msys2, not in a native Windows cmd.
   These are not fixable in Reasonix code.

☐ Does the same issue affect all frontends, or only one?

   "One controller behind every frontend" (REASONIX.md). If a TUI
   bug doesn't reproduce via `reasonix serve`, the issue is in
   `internal/cli/`, not the controller. If it affects all three
   (TUI, serve, desktop), fix in `internal/control/`.
```

### 0c. Check for Feature Conflict — Read the Invariants

Every bug fix must stand on the shoulders of the codebase's design.
**Check these before proposing any change:**

```
☐ Cache-first: never mutate the system-prompt prefix mid-session

   The prefix (base prompt + tools + memory) must stay byte-stable
   across turns so DeepSeek's prefix cache stays warm. Any fix that
   appends to the cached prefix is WRONG — ride the turn tail via
   `control.Compose()` (internal/control/input.go:36). See agent.go
   comment at internal/agent/agent.go:118-119: plan mode uses a
   marker in the USER MESSAGE, never the system prompt.

☐ Controller owns behavior; frontends own rendering

   "Add behavior to the controller, not a frontend." (REASONIX.md)
   If the same logic would need to be duplicated in TUI + serve +
   desktop, the fix belongs in internal/control/. If the logic is
   purely visual (CSS overflow, scrollbar, color), it belongs in
   the frontend layer.

☐ The event.Kind enum is a contract between controller + frontends

   Defining a new event type (internal/event/event.go:20-88) means
   every frontend must handle it (or explicitly ignore it). Before
   adding one, ask: "Can I use an existing event kind with a new
   field?" Adding fields is cheaper than adding kinds.

☐ Tool ReadOnly() governs plan-mode gating

   `planMode atomic.Bool` (agent.go:118) gates write tools.
   Marking a tool as NOT read-only means it's blocked in plan mode.
   Changing a tool's ReadOnly() return value changes plan-mode
   behavior — this touches the prompt contract. Verify against
   internal/tool/builtin/todo.go:82 before proposing.

☐ The four surfaces have different update cadences

   User-facing (slash commands) → updates to internal/cli/
   Model-facing (tools)        → updates to internal/tool/ + prompt
   UI (desktop frontend)       → updates to desktop/frontend/
   Library exports (src/index.ts) → embedder API, semver-matters

   A fix that changes a model-facing tool's behavior also changes
   what the model can do. A fix that only changes the desktop
   frontend has no effect on TUI or serve users.
```

### 0d. The "Design Intention" Test

Read the comment block above the code you're about to change.

```go
// If the comment says "yolo mode auto-approves everything,"
// then #3624 is a DESIGN REFINEMENT, not a bug fix.
// The fix requires updating the comment AND the behavior.
```

```go
// If the comment says "todo_write is read-only — context side-effect only,"
// then the progress rendering bug is in the TUI, not the tool.
// The tool is working as designed.
```

**Rule:** A behavior that matches its documented contract is not a bug.
It may be a bad design — that's a feature request, not a fix.
Label it `enhancement` and route through the planning process.

---

## The Four-Phase Bugfix Loop

```
┌─────────────────────────────────────────────────┐
│  1. PREPARE   →   2. REPRODUCE   →             │
│                   3. CODE CHANGE  →             │
│                   4. VERIFY                      │
└─────────────────────────────────────────────────┘
         ↑_____________ loop until green _____________↓
```

---

## Phase 1 — PREPARE

### 1a. Know what you're working with

```bash
# Read the project conventions — every package has a comment
git log --oneline -5          # Recent activity
git branch -a                 # main-v2 is the active dev branch
make help                     # Available build targets
```

Project layout:
- `cmd/` — CLI entry points (`reasonix`, `reasonix serve`, etc.)
- `internal/` — Go kernel, one concern per package
- `desktop/` — Wails desktop app (Go backend + frontend/)
- One transport-agnostic `control.Controller` behind every frontend

### 1b. Read the issue fully

```
☐ Version / environment details (OS, version line, exact version)
☐ What happened — the symptom
☐ Steps to reproduce — these are GOLD
☐ Relevant logs or output
☐ Labels — agent? tui? desktop? config?
```

**Map the tags to code:**
| Label | Code Area |
|-------|-----------|
| `agent` | `internal/agent/`, `internal/control/` |
| `tui` | `internal/cli/` |
| `desktop` | `desktop/` (Go) + `desktop/frontend/` (TS/HTML) |
| `config` | `internal/config/` |
| `mcp` | `internal/plugin/`, `codegraph/` |

### 1c. Read the relevant code first

```bash
# Find the files
search_content "AskChoice\|ask_choice" internal/agent/
search_content "TodoWrite\|todo_write" internal/control/

# Read the package comment + key functions
read_file internal/agent/agent.go
read_file internal/control/compose.go
```

**Never guess the code** — the SEARCH/REPLACE edit gate requires a read this session.

---

## Phase 2 — REPRODUCE

### 2a. Build from source

```bash
make build              # Full build
go build ./cmd/reasonix # Minimal build — faster
```

### 2b. Run the exact scenario from the issue

```bash
# TUI bugs
./reasonix              # Launch TUI to test todo progress / ask_choice

# Server bugs
./reasonix serve        # Test Web UI at http://127.0.0.1:8787

# Desktop bugs
cd desktop && go run .  # Launch Wails desktop app
```

### 2c. Confirm the bug

> **Rule:** If you can't reproduce it within 3 attempts, tag the issue with `needs-reproduction` and move on.
>
> Environment issues (#3631, #3620) are not worth chasing — they won't reproduce in a clean dev env.

**Reproduction checklist:**
```
☐ Bare minimum steps (strip config, remove plugins)
☐ Default config only
☐ Fresh state (delete ~/.reasonix/ state)
```

### 2d. Write a test that captures the bug

```go
// internal/agent/agent_test.go
func TestAskChoiceNotAutoSelectedInYolo(t *testing.T) {
    // Arrange — yolo mode enabled
    ctrl := control.New(control.WithYolo(true))
    
    // Act — ask_choice is called
    // Assert — tool execution paused, not auto-approved
    
    t.Errorf("yolo mode should NOT auto-select ask_choice")
}
```

> **Not every bug needs a test** — but if you can write one quickly, DO IT. Tests are the best verification for the next phase.

---

## Phase 3 — CODE CHANGE

### 3a. Study the surrounding code style

```go
// Reasonix Go conventions — match these:
//   - Package comment at top (one concern)
//   - err != nil returns early
//   - Descriptive variable names, no abbreviations
//   - Comment density matches surrounding code
//   - Control flow: early return, defer cleanup
```

### 3b. Plan the minimal change

> **"The best fix is the one that touches the fewest layers."**

Given the architecture (one controller, three frontends), the question is
not just "what's the smallest diff" but "which LAYER does the fix belong to?"

```
Fix in CONTROLLER (internal/control/) if:
  - The bug affects the agent loop or session lifecycle
  - All three frontends would need the same fix
  - The fix changes tool execution / approval behavior
  - You need to emit a new or modified event type

Fix in TUI (internal/cli/) if:
  - The bug is purely about terminal rendering or keybindings
  - The `/` slash command parsing or display is wrong
  - The todo panel / status bar / progress display is wrong

Fix in DESKTOP (desktop/ Go + frontend/) if:
  - The bug only affects the Wails GUI (model picker, settings, tray)
  - It's a CSS/HTML/layout issue in desktop/frontend/
  - It's a Windows-specific event handling issue (tray icon, window state)

NO FIX needed (document or close) if:
  - The root cause is third-party software or OS boot timing
  - The behavior is "by design" per the current invariants
  - The fix would break the cache-stable prefix invariant
```

**The four-surface check:** Before writing code, ask:

```
This symptom appears in which surface?
  ☐ User-facing (slash commands)  → internal/cli/
  ☐ Model-facing (tools)          → internal/tool/ + prompt text
  ☐ UI (desktop frontend)         → desktop/frontend/ (TS/React)
  ☐ Library exports (src/index.ts)→ semver-sensitive, for embedders

Does the fix change tool behavior?
  → If yes, it changes what the model can do — test the prompt contract.
  → If no, it's a rendering/config fix — test the UI only.

Does the fix touch the cached prefix?
  → If yes, STOP. Ride the turn tail instead (control.Compose).
```

**Trace the data flow before editing:**

```
Issue symptom → which component produces the bad data?
              → which component consumes it?
              → where should the correction happen?

Example — #3633 (todo progress stuck at 0/3):
  Producer: tool/todo.go returns correct completed count
  Consumer: cli/chat_tui.go renderTodoPanel() displays wrong number
  → Bug is in the CONSUMER. Don't touch the tool.
  → Fix: renderTodoPanel() should track in_progress→completed
    transitions from the todo_write args, not re-derive from scratch.

Example — #3624 (yolo skips ask_choice):
  Producer: control/controller.go requestApproval() in bypass mode
  Consumer: the user (who never sees the ask prompt)
  → The bug IS in the producer. Bypass exempts no tool types.
  → Fix: exempt ActionAskChoice from bypass auto-approval.
```

### 3c. Make the change — SEARCH/REPLACE

```go
// OLD — buggy
<<<<<<< SEARCH
func (a *Agent) approve(action Action) bool {
    if a.yolo {
        return true // <-- approves EVERYTHING, including ask_choice
    }
    ...
=======
// NEW — fixed
func (a *Agent) approve(action Action) bool {
    if a.yolo {
        // ask_choice requires user interaction even in yolo mode
        if action.Type == ActionAskChoice {
            return false
        }
        return true
    }
    ...
>>>>>>> REPLACE
```

**Style rules:**
- Match the **exact** indentation and brace style of the file
- Comments in the same language as surrounding comments
- Error handling in the same pattern (`err != nil` / `errors.Wrap`)
- No trailing whitespace

### 3d. Run the existing test suite

```bash
go test ./internal/agent/   # Package-level
go test ./...               # Full suite — don't ship if this fails
make test                   # Project conventions
```

---

## Phase 4 — VERIFY

### 4a. Rebuild and re-run the reproduction scenario

```bash
make build && ./reasonix
# Now step through the exact same steps from Phase 2
# The bug should no longer appear
```

### 4b. Run the test you wrote (Phase 2d)

```bash
go test -run TestAskChoiceNotAutoSelectedInYolo ./internal/agent/
# ✓ PASS
```

### 4c. Check for regressions

```bash
# Run the FULL test suite — not just one package
go test ./...

# If the fix was in the controller, run all frontend tests too
cd desktop && go test ./...
```

### 4d. Human review pass

```
☐ Did I fix the RIGHT bug? (not just a symptom)

   "Auto-refresh overwrites edits" → did you fix the auto-refresh,
   or did you just add a "manual refresh" button? The latter
   papers over the data-loss bug.

☐ Did I choose the right layer?

   If the fix is in the controller, does it also fix the TUI?
   If it only needed a desktop frontend change, did I accidentally
   touch internal/control/? (Don't — the controller is shared.)

☐ Did I introduce a cache-stability violation?

   Does my change add anything to the system-prompt prefix?
   If yes: move it to the turn tail (control.Compose).

☐ Did I break any edge cases? (empty state, error state, multi-turn)

   - Yolo mode + no ask_choice call → still fast-paths correctly?
   - Todo list with 0 items → renderTodoPanel hides?
   - Model picker with 1 model → still renders?
   - Desktop tray on macOS/Linux → unchanged? (tray_icon_unix.go)

☐ Is there a test I should update?

   - If I changed bypass behavior → check yolo_test.go
   - If I changed todo tool → check todo_test.go
   - If I changed tray logic → check tray_test.go, tray_icon_windows_test.go
   - If no test exists → write the minimal one that fails before the fix

☐ Are there any related code paths that need the same fix?

   #3628 + #3629 + #3632(2) are the SAME model-picker truncation bug
   reported by three people. Fix once, link all three.

☐ Does the commit message reference the issue AND explain WHY?

   Not just "Fixes #3624" — explain the root cause and the approach.
   The "why" is more important than the "what" for future readers.
```

---

## When to skip / escalate

| Situation | Action |
|-----------|--------|
| Bug doesn't reproduce | Tag `needs-reproduction`, move to next issue |
| Root cause is OS/environment | Close as `not planned`, document in `REASONIX.md` |
| Fix would touch 6+ files | Write a plan first (`submit_plan`) |
| Fix requires API or contract change | Write a plan first (`submit_plan`) |
| Same root cause as another issue | Fix once, link both issues in commit message |
| Unsure about correctness | Tag the fix with `// REVIEW: <uncertainty>` and `review` the diff |

---

## Commit message format

```
fix(agent): don't auto-approve ask_choice in yolo mode

- ask_choice requires user interaction even when yolo is active
- yolo mode was unconditionally approving all action types
- add ActionAskChoice type check to the yolo approval path

Fixes #3624
```

---

## TL;DR — The 5-minute checklist

```
[ ] Read the issue + the relevant code
[ ] Build from source
[ ] Reproduce the bug
[ ] Write a failing test
[ ] Make the minimal code change (match style!)
[ ] Full test suite passes
[ ] Re-run reproduction — bug gone
[ ] Commit with "Fixes #XXXX"
```
