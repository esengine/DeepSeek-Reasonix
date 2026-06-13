// Run: tsx src/__tests__/terminal-output.test.ts

import { createTerminalOutputRouter } from "../lib/terminalOutput";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nterminal output router");

{
  const router = createTerminalOutputRouter();
  router.write("term-1", "hello ", []);
  router.write("term-1", "world", []);
  eq(router.takePending("term-1"), "hello world", "buffers output before a tab exists");
  eq(router.takePending("term-1"), "", "pending buffer is cleared after take");
}

{
  const router = createTerminalOutputRouter();
  const chunks: string[] = [];
  router.write("term-2", "prompt$ ", [{ sessionID: "term-2", write: (data) => chunks.push(data) }]);
  eq(chunks.join(""), "prompt$ ", "writes directly when the session tab exists");
  eq(router.takePending("term-2"), "", "does not buffer output routed to an active tab");
}

{
  const router = createTerminalOutputRouter();
  router.write("term-3", "early", []);
  const chunks: string[] = [];
  eq(router.takePending("term-3"), "early", "takePending replays buffered output for new tabs");
  router.write("term-3", " live", [{ sessionID: "term-3", write: (data) => chunks.push(data) }]);
  eq(chunks.join(""), " live", "continues writing live output after the tab appears");
}

{
  const router = createTerminalOutputRouter();
  router.write("term-4", "stale", []);
  router.clearPending("term-4");
  eq(router.takePending("term-4"), "", "clearPending drops buffered output when a tab closes");
}

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
