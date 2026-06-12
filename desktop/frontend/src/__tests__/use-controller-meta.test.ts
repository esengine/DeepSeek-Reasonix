// Run: tsx src/__tests__/use-controller-meta.test.ts

import { initialState, reducer, sameMeta, shouldReconcileStaleTurn } from "../lib/useController";
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

{
  const started = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
  const rendered = reducer(started, { type: "event", e: { kind: "message", text: "done", reasoning: "" } });
  eq(rendered.running, true, "message without turn_done leaves local runtime marked running");
  eq(rendered.turnActive, true, "message without turn_done still belongs to an active turn");
  eq(rendered.live, undefined, "final message closes the live stream before turn_done");
  eq(shouldReconcileStaleTurn(rendered, 1_000, 31_000), true, "stale completed stream still reconciles missed turn_done");
  eq(shouldReconcileStaleTurn(rendered, 1_000, 20_000), false, "fresh completed stream waits before reconciling");
  eq(shouldReconcileStaleTurn({ ...rendered, turnActive: false }, 1_000, 31_000), false, "local pending send before turn_started does not reconcile");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
