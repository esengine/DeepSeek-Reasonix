// Run: tsx src/__tests__/parse-todos.test.ts
//
// Coverage for parseTodos (src/lib/tools.ts): pulls the task list out of a
// todo_write call's raw-JSON args, returning [] for anything that is not
// JSON with an array-valued "todos" key.

import { parseTodos } from "../lib/tools";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nparse-todos");

// ── valid todos ─────────────────────────────────────────────────────
eq(
  parseTodos('{"todos":[{"content":"Add the parser","status":"pending"}]}'),
  [{ content: "Add the parser", status: "pending" }],
  "single pending todo parses",
);
eq(
  parseTodos(
    '{"todos":[{"content":"One","status":"pending"},{"content":"Two","status":"in_progress","activeForm":"Doing two","level":1}]}',
  ),
  [
    { content: "One", status: "pending" },
    { content: "Two", status: "in_progress", activeForm: "Doing two", level: 1 },
  ],
  "multiple todos with activeForm and level parse",
);
eq(parseTodos('{"todos":[]}'), [], "empty todos array parses to []");

// ── non-JSON input ──────────────────────────────────────────────────
eq(parseTodos("not json"), [], "non-JSON string returns []");
eq(parseTodos(""), [], "empty string returns []");

// ── missing / malformed todos key ───────────────────────────────────
eq(parseTodos("{}"), [], "missing todos key returns []");
eq(parseTodos('{"foo":1}'), [], "object without todos returns []");
eq(parseTodos('{"todos":"pending"}'), [], "string todos returns []");
eq(parseTodos('{"todos":{}}'), [], "object todos returns []");
eq(parseTodos('{"todos":42}'), [], "number todos returns []");
eq(parseTodos("null"), [], "JSON null returns []");

console.log(`\n${passed} passed, ${failed} failed\n`);
process.exit(failed === 0 ? 0 : 1);
