// Run: tsx src/__tests__/tool-subject.test.ts

import { subjectOf } from "../lib/tools";
import { toolGroupKind } from "../components/ToolGroup";
import { historyMessagesToItems, isReadOnlyTool } from "../lib/useController";
import type { HistoryMessage } from "../lib/types";

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

console.log("\ntool subject contract");

eq(subjectOf("task", JSON.stringify({ description: "audit docs" })), "audit docs", "task subject uses description");
eq(subjectOf("run_skill", JSON.stringify({ name: "code-reviewer", arguments: "review this branch" })), "code-reviewer", "run_skill subject uses skill name");
eq(
  subjectOf("use_capability", JSON.stringify({ action: "call", capability_id: "mcp-tool:github/search_issues" })),
  "mcp-tool:github/search_issues",
  "use_capability subject uses capability_id",
);
eq(subjectOf("use_capability", JSON.stringify({ action: "list" })), "list", "use_capability list falls back to action");

const capabilityArgs = JSON.stringify({ action: "call", capability_id: "mcp-tool:db/write" });
eq(
  toolGroupKind({ kind: "tool", id: "reader", name: "use_capability", args: capabilityArgs, readOnly: true, status: "done" }),
  "explore",
  "resolved read-only MCP groups as research",
);
eq(
  toolGroupKind({ kind: "tool", id: "writer", name: "use_capability", args: capabilityArgs, readOnly: false, status: "done" }),
  "modify",
  "resolved writer MCP stays in the modify group",
);

const history = historyMessagesToItems([{
  role: "assistant",
  content: "",
  toolCalls: [
    {
      id: "old",
      name: "use_capability",
      arguments: capabilityArgs,
    },
    {
      id: "reader",
      name: "use_capability",
      arguments: capabilityArgs,
      resolvedName: "mcp__db__read",
      capabilityId: "mcp-tool:db/read",
      resolvedReadOnly: true,
    },
    {
      id: "writer",
      name: "use_capability",
      arguments: capabilityArgs,
      resolvedName: "mcp__db__write",
      capabilityId: "mcp-tool:db/write",
      resolvedReadOnly: false,
    },
  ],
}] as HistoryMessage[], "h-").items.filter((item) => item.kind === "tool");
eq(history[0]?.kind === "tool" ? history[0].readOnly : true, false, "old proxy history fails closed as visible");
eq(history[1]?.kind === "tool" ? history[1].readOnly : false, true, "resolved reader history restores read-only classification");
eq(history[2]?.kind === "tool" ? history[2].readOnly : true, false, "resolved writer history restores writer classification");
eq(history[2]?.kind === "tool" ? history[2].resolvedName : "", "mcp__db__write", "history keeps resolved MCP target");
eq(isReadOnlyTool("read_file"), true, "legacy built-in reader remains read-only");
eq(isReadOnlyTool("grep"), true, "legacy search reader remains read-only");
eq(isReadOnlyTool("use_capability"), false, "legacy unresolved proxy remains fail-closed");
eq(isReadOnlyTool("unknown_tool"), false, "unknown history tool remains fail-closed");

if (failed) {
  process.stdout.write(`\n${failed} failed, ${passed} passed\n`);
  process.exit(1);
}
process.stdout.write(`\n${passed} passed\n`);
