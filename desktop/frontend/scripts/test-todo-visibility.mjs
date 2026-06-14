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
const { shouldShowTodoPanel } = await import(moduleUrl);

const completedTodos = [
  { content: "Inspect the report", status: "completed" },
  { content: "Ship the fix", status: "completed" },
];

assert.equal(
  shouldShowTodoPanel("todo-final", null, completedTodos),
  true,
  "the final all-completed todo_write must remain visible",
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

const iterations = 200_000;
const started = performance.now();
for (let i = 0; i < iterations; i += 1) {
  if (!shouldShowTodoPanel("todo-perf", null, completedTodos)) {
    throw new Error("unexpected hidden todo panel during performance loop");
  }
}
const elapsed = performance.now() - started;
const perCallUs = (elapsed * 1000) / iterations;

assert.ok(elapsed < 500, `todo visibility check is too slow: ${elapsed.toFixed(2)} ms`);
console.log(
  `todo visibility checks: ${iterations} calls in ${elapsed.toFixed(2)} ms (${perCallUs.toFixed(3)} us/call)`,
);
