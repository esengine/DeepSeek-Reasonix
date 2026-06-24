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
const { shouldShowTodoPanel, todoDismissalKey } = await import(moduleUrl);

const completedTodos = [
  { content: "Inspect the report", status: "completed" },
  { content: "Ship the fix", status: "completed" },
];
const activeTodos = [
  { content: "Inspect the report", status: "in_progress" },
  { content: "Ship the fix", status: "pending" },
];

assert.equal(
  shouldShowTodoPanel("todo-final", null, completedTodos),
  true,
  "a completed todo list stays visible in collapsed form until the user dismisses it",
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
assert.notEqual(
  activeKey,
  todoDismissalKey([{ ...activeTodos[0], status: "completed" }, { ...activeTodos[1], status: "in_progress" }]),
  "real progress produces a fresh dismissal key",
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
