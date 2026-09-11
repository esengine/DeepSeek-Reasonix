import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { performance } from "node:perf_hooks";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const sourcePath = path.join(root, "src", "lib", "todoVisibility.ts");
const source = readFileSync(sourcePath, "utf8");
const transpiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ES2022,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText;

const moduleUrl = `data:text/javascript;base64,${Buffer.from(transpiled).toString("base64")}`;
const {
  dismissedTodoKeyForScope,
  resolveTodoPanelTodos,
  sameStringList,
  sameTodoList,
  scopedTodoBatchKey,
  scopedTodoDismissalKey,
  shouldOpenTodoPanelByDefault,
  shouldShowTodoPanel,
  todoBatchKey,
  todoContinueTarget,
  todoDismissalKey,
  todoPanelScope,
  todoPresentationStatus,
} = await import(moduleUrl);

const completedTodos = [
  { content: "Inspect the report", status: "completed" },
  { content: "Ship the fix", status: "completed" },
];
const activeTodos = [
  { content: "Inspect the report", status: "in_progress" },
  { content: "Ship the fix", status: "pending" },
];

assert.equal(todoPresentationStatus("in_progress", { running: true, pendingPrompt: false }), "in_progress", "an active turn renders its current todo as in progress");
assert.equal(todoPresentationStatus("in_progress", { running: false, pendingPrompt: false }), "paused", "an idle or restored turn renders a stale current todo as ready to continue");
assert.equal(todoPresentationStatus("in_progress", { running: true, pendingPrompt: true }), "waiting", "a prompt-blocked turn renders its current todo as waiting for the user");
assert.equal(todoPresentationStatus("completed", { running: false, pendingPrompt: false }), "completed", "completed todos remain completed");
assert.equal(todoContinueTarget("tab-a", "tab-a", { ready: true, running: false, pendingPrompt: false }), "tab-a", "continue targets the exact idle visible tab");
assert.equal(todoContinueTarget("tab-a", "tab-b", { ready: true, running: false, pendingPrompt: false }), null, "a rapid tab switch prevents stale todo routing");
assert.equal(todoContinueTarget("tab-a", "tab-a", { ready: true, running: true, pendingPrompt: false }), null, "running turns cannot receive a duplicate continue submission");
assert.equal(todoContinueTarget("tab-a", "tab-a", { ready: true, running: false, pendingPrompt: true }), null, "pending prompts must be answered instead of bypassed by continue");

assert.deepEqual(
  resolveTodoPanelTodos([], undefined),
  [],
  "an authoritative empty canonical list with no live tool clears the panel",
);
assert.deepEqual(
  resolveTodoPanelTodos(
    [{ content: "Inspect the report", status: "in_progress" }, { content: "Ship the fix", status: "pending" }],
    [{ content: "Inspect the report", status: "completed" }, { content: "Ship the fix", status: "in_progress" }],
  ),
  [{ content: "Inspect the report", status: "completed" }, { content: "Ship the fix", status: "in_progress" }],
  "a live todo_write snapshot advances mid-turn status past a stale meta snapshot",
);
assert.deepEqual(
  resolveTodoPanelTodos(undefined, activeTodos),
  activeTodos,
  "an unavailable canonical list falls back to the transcript snapshot",
);
assert.deepEqual(
  resolveTodoPanelTodos(activeTodos, undefined),
  activeTodos,
  "without a live tool the panel keeps the meta snapshot",
);
assert.equal(
  sameTodoList(activeTodos, activeTodos.map((todo) => ({ ...todo }))),
  true,
  "equivalent canonical lists do not churn meta state",
);
assert.equal(
  sameTodoList(activeTodos, [{ ...activeTodos[0], status: "completed" }, activeTodos[1]]),
  false,
  "canonical status changes invalidate meta state",
);

assert.equal(
  shouldShowTodoPanel("todo-final", null, completedTodos),
  true,
  "a completed batch remains mounted so TodoPanel can distinguish restore from a live transition",
);
assert.equal(
  shouldShowTodoPanel("todo-active", null, [{ content: "Run tests", status: "in_progress" }]),
  true,
  "an active todo_write remains visible",
);
assert.equal(
  shouldShowTodoPanel("todo-final", "todo-final", completedTodos),
  false,
  "a user dismissal still hides that exact todo list",
);
const completedKey = todoDismissalKey(completedTodos);
const dismissedBySession = new Set([scopedTodoDismissalKey("session:a", completedKey)]);
assert.equal(
  shouldShowTodoPanel(completedKey, dismissedTodoKeyForScope("session:a", dismissedBySession, completedKey), completedTodos),
  false,
  "a completed todo dismissal hides the list in the session where it was closed",
);
assert.equal(
  shouldShowTodoPanel(completedKey, dismissedTodoKeyForScope("session:b", dismissedBySession, completedKey), completedTodos),
  true,
  "a completed todo dismissal in one session does not hide another session's list",
);
dismissedBySession.add(scopedTodoDismissalKey("session:b", completedKey));
assert.equal(
  shouldShowTodoPanel(completedKey, dismissedTodoKeyForScope("session:a", dismissedBySession, completedKey), completedTodos),
  false,
  "closing another session's todo does not forget the first session dismissal",
);
assert.equal(
  todoPanelScope({
    activeTabId: "tab-a",
    activeTab: {
      id: "tab-a",
      scope: "project",
      workspaceRoot: "/repo",
      topicId: "topic-a",
      sessionPath: "/sessions/a.jsonl",
    },
  }),
  "session:/sessions/a.jsonl",
  "a restored history session uses its session path as the todo panel scope",
);
assert.equal(
  todoPanelScope({
    activeTabId: "tab-b",
    activeTab: {
      id: "tab-a",
      scope: "project",
      workspaceRoot: "/repo",
      topicId: "topic-a",
      sessionPath: "/sessions/a.jsonl",
    },
    eventChannel: "desktop-events",
  }),
  "tab:tab-b",
  "a stale active-tab fallback must not scope dismissal to the previous session",
);
assert.equal(
  todoPanelScope({
    activeTabId: "tab-c",
    activeTab: {
      id: "tab-c",
      scope: "project",
      workspaceRoot: "/repo",
      topicId: "topic-a",
    },
  }),
  "tab:tab-c",
  "an unsaved topic session uses its tab id so sibling sessions do not share todo state",
);
assert.equal(shouldShowTodoPanel(null, null, completedTodos), false, "no canonical todo item means no panel");
assert.equal(shouldShowTodoPanel("todo-empty", null, []), false, "empty todo lists do not render a panel");

const activeKey = todoDismissalKey(activeTodos);
assert.equal(
  activeKey,
  todoDismissalKey(activeTodos.map((todo) => ({ ...todo }))),
  "the same task list keeps a stable dismissal key across restored event ids",
);
assert.equal(
  shouldShowTodoPanel(activeKey, activeKey, activeTodos),
  true,
  "an incomplete restored todo list must reappear even after a stale local dismissal",
);
assert.equal(
  shouldShowTodoPanel(activeKey, null, activeTodos, { batchKey: todoBatchKey(activeTodos), batches: [todoBatchKey(activeTodos)] }),
  true,
  "a persisted completed-batch dismissal cannot hide unfinished work",
);
const completedBatch = todoBatchKey(completedTodos);
assert.equal(
  shouldShowTodoPanel(completedKey, null, completedTodos, { batchKey: completedBatch, batches: [completedBatch] }),
  false,
  "a session-sidecar batch dismissal hides the completed shelf after upgrade remount",
);
assert.equal(sameStringList(["a"], ["a"]), true, "identical dismissed batch lists compare equal");
assert.equal(sameStringList(["a"], ["b"]), false, "changed dismissed batch lists compare unequal");
assert.notEqual(
  activeKey,
  todoDismissalKey([{ ...activeTodos[0], status: "completed" }, { ...activeTodos[1], status: "in_progress" }]),
  "real progress produces a fresh dismissal key",
);
assert.equal(
  todoBatchKey(activeTodos),
  todoBatchKey([{ ...activeTodos[0], status: "completed" }, { ...activeTodos[1], status: "in_progress" }]),
  "status progress stays in the same todo batch",
);
assert.equal(
  scopedTodoBatchKey("session:a", todoBatchKey(activeTodos)),
  scopedTodoBatchKey("session:a", todoBatchKey([{ ...activeTodos[0], status: "completed" }, { ...activeTodos[1], status: "in_progress" }])),
  "status progress keeps the same scoped open-state key",
);
assert.notEqual(
  scopedTodoBatchKey("session:a", todoBatchKey(activeTodos)),
  scopedTodoBatchKey("session:b", todoBatchKey(activeTodos)),
  "the same task list in a different session gets isolated open state",
);
assert.notEqual(
  todoBatchKey(activeTodos),
  todoBatchKey([{ content: "Run a different task", status: "in_progress" }]),
  "new task content creates a new todo batch",
);
assert.equal(
  shouldOpenTodoPanelByDefault(),
  false,
  "todo batches collapse by default regardless of completion",
);

const iterations = 200_000;
const started = performance.now();
for (let i = 0; i < iterations; i += 1) {
  if (shouldShowTodoPanel("todo-perf", "todo-perf", completedTodos)) {
    throw new Error("unexpected visible todo panel during performance loop");
  }
}
const elapsed = performance.now() - started;
const perCallUs = (elapsed * 1000) / iterations;

assert.ok(elapsed < 500, `todo visibility check is too slow: ${elapsed.toFixed(2)} ms`);
console.log(
  `todo visibility checks: ${iterations} calls in ${elapsed.toFixed(2)} ms (${perCallUs.toFixed(3)} us/call)`,
);
