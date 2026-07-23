// Run: tsx src/__tests__/use-controller-stream-progress.test.ts
//
// Covers the delivery-mode liveness fixes:
// 1. Partial tool dispatches upsert a running card (instead of being dropped)
//    and carry streaming argChars progress.
// 2. usageSeq bumps on every usage event regardless of source, so the right
//    panel keeps refreshing during sub-agent runs.
// 3. A mid-turn context snapshot reporting used=0 does not collapse a gauge
//    that already shows real usage.

import { initialState, reducer } from "../lib/useController";
import type { WireEvent } from "../lib/types";

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

function ev(s: typeof initialState, e: WireEvent) {
  return reducer(s, { type: "event", e });
}

// --- 1. partial dispatch upserts a running card with argChars ---
{
  let s = { ...initialState, running: true, turnActive: true };
  s = ev(s, { kind: "tool_dispatch", tool: { id: "c1", name: "write_file", readOnly: false, partial: true } } as WireEvent);
  const card = s.items.find((it) => it.kind === "tool" && it.id === "c1");
  eq(Boolean(card), true, "partial dispatch creates a running tool card");
  eq(card?.kind === "tool" ? card.status : "", "running", "partial card is running");
  eq(card?.kind === "tool" ? card.args : "x", "", "partial card has no args yet");

  s = ev(s, { kind: "tool_dispatch", tool: { id: "c1", name: "write_file", readOnly: false, partial: true, argChars: 8192 } } as WireEvent);
  const card2 = s.items.find((it) => it.kind === "tool" && it.id === "c1");
  eq(card2?.kind === "tool" ? card2.argChars : 0, 8192, "arg progress updates the card");
  eq(s.turnArgChars, 8192, "turnArgChars mirrors streaming progress");

  s = ev(s, { kind: "tool_dispatch", tool: { id: "c1", name: "write_file", args: '{"path":"a"}', readOnly: false } } as WireEvent);
  const card3 = s.items.find((it) => it.kind === "tool" && it.id === "c1");
  eq(card3?.kind === "tool" ? card3.args : "", '{"path":"a"}', "full dispatch merges args into the same card");
  eq(card3?.kind === "tool" ? card3.argChars : 1, undefined, "full dispatch clears argChars");
  eq(
    s.items.filter((it) => it.kind === "tool" && it.id === "c1").length,
    1,
    "partial + full dispatch never duplicate the card",
  );

  s = ev(s, { kind: "tool_dispatch", tool: { id: "c1", name: "write_file", args: '{"path":"a"}', readOnly: false, refreshed: true, diff: "@@ -1 +1 @@\n-old\n+new\n", added: 1, removed: 1 } } as WireEvent);
  const refreshed = s.items.find((it) => it.kind === "tool" && it.id === "c1");
  eq(refreshed?.kind === "tool" ? refreshed.fileDiff?.diff : "", "@@ -1 +1 @@\n-old\n+new\n", "same-ID refresh replaces the live preview");
  eq(s.items.filter((it) => it.kind === "tool" && it.id === "c1").length, 1, "preview refresh never duplicates the card");

  s = ev(s, { kind: "usage", usage: { promptTokens: 100, completionTokens: 50, totalTokens: 150, cacheHitTokens: 0, cacheMissTokens: 0 } } as WireEvent);
  eq(s.turnArgChars, 0, "usage event resets the streaming estimate");
}

// --- 1b. partial dispatch without an ID never creates an orphan card ---
{
  let s = { ...initialState, running: true, turnActive: true };
  // OpenAI-compatible streams can surface the name before the call ID.
  s = ev(s, { kind: "tool_dispatch", tool: { name: "write_file", readOnly: false, partial: true, argChars: 2048 } } as WireEvent);
  eq(s.items.filter((it) => it.kind === "tool").length, 0, "id-less partial creates no card");
  eq(s.turnArgChars, 2048, "id-less partial still counts streaming progress");

  s = ev(s, { kind: "tool_dispatch", tool: { id: "c1", name: "write_file", readOnly: false, partial: true, argChars: 4096 } } as WireEvent);
  s = ev(s, { kind: "tool_dispatch", tool: { id: "c1", name: "write_file", args: '{"path":"a"}', readOnly: false } } as WireEvent);
  eq(s.items.filter((it) => it.kind === "tool").length, 1, "late ID yields exactly one card, no orphan");
  const only = s.items.find((it) => it.kind === "tool");
  eq(only?.kind === "tool" ? only.id : "", "c1", "surviving card carries the real call ID");
}

// --- 2. usageSeq bumps for every source ---
{
  let s = { ...initialState, running: true, turnActive: true };
  s = ev(s, { kind: "usage", usage: { promptTokens: 10, completionTokens: 5, totalTokens: 15, cacheHitTokens: 0, cacheMissTokens: 0 } } as WireEvent);
  eq(s.usageSeq, 1, "executor usage bumps usageSeq");
  s = ev(s, { kind: "usage", usage: { promptTokens: 10, completionTokens: 5, totalTokens: 15, cacheHitTokens: 0, cacheMissTokens: 0, source: "subagent" } } as WireEvent);
  eq(s.usageSeq, 2, "subagent usage bumps usageSeq");
  eq(s.usage?.source ?? "", "", "subagent usage does not replace executor gauge usage");
}

// --- 3. context no-regress guard while a turn runs ---
{
  let s = { ...initialState, running: true, turnActive: true, context: { used: 14000, window: 1000000, sessionTokens: 20000 } };
  s = reducer(s, { type: "context", context: { used: 0, window: 1000000, sessionTokens: 20000 } } as never);
  eq(s.context.used, 14000, "mid-turn used=0 snapshot keeps last known fill");

  let idle = { ...initialState, context: { used: 14000, window: 1000000, sessionTokens: 20000 } };
  idle = reducer(idle, { type: "context", context: { used: 0, window: 1000000, sessionTokens: 0 } } as never);
  eq(idle.context.used, 0, "idle used=0 snapshot applies (genuine reset)");
}

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
