import { initialState, reducer } from "../lib/useController";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    passed += 1;
    process.stdout.write(`  PASS  ${label}\n`);
  } else {
    failed += 1;
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
  }
}

const state = reducer({
  ...initialState,
  promptEpoch: 7,
  ask: { id: "ask-old", questions: [] },
  mcpInteraction: { id: "mcp-new", server: "server", mode: "form", message: "new" },
  pendingPrompt: true,
}, { type: "expire_prompt", id: "ask-old", epoch: 7, kind: "ask" });

eq(state.ask, undefined, "stale Ask expiry removes only its matching card");
eq(state.mcpInteraction?.id, "mcp-new", "stale Ask expiry preserves a newer MCP card");

console.log(`prompt expiry: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
