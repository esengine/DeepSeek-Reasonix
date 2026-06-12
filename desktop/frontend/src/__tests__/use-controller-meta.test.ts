// Run: tsx src/__tests__/use-controller-meta.test.ts

import { sameMeta } from "../lib/useController";
import type { Meta } from "../lib/types";

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

function meta(overrides: Partial<Meta> = {}): Meta {
  return {
    label: "DeepSeek-R1",
    ready: true,
    eventChannel: "events",
    cwd: "/repo",
    autoApproveTools: false,
    bypass: false,
    collaborationMode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    goal: "",
    goalStatus: "stopped",
    ...overrides,
  };
}

console.log("\nuse controller meta");

{
  eq(sameMeta(meta(), meta()), true, "identical meta is unchanged");
  eq(sameMeta(meta({ collaborationMode: "normal" }), meta({ collaborationMode: "plan" })), false, "collaboration mode changes invalidate meta equality");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
