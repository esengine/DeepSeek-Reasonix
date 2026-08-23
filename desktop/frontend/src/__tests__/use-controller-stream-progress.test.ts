// Run: tsx src/__tests__/use-controller-stream-progress.test.ts
//
// Covers the delivery-mode liveness fixes:
// 1. Partial tool dispatches upsert a running card (instead of being dropped)
//    and carry streaming argChars progress.
// 2. usageSeq bumps on every usage event regardless of source, so the right
//    panel keeps refreshing during sub-agent runs.
// 3. A mid-turn context snapshot reporting used=0 does not collapse a gauge
//    that already shows real usage.
// 4. A retry event repairs stale idle snapshots in either delivery order so
//    the turn remains stoppable.
// 5. TPS telemetry accumulates provider-output intervals without tool gaps and
//    survives both missing usage and missing turn_done events.

import { initialState, promptEventClock, reducer } from "../lib/useController";
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

// Desktop keeps ordinary completion receipts off the transcript, but retains
// details for the change panel and surfaces actionable gaps as a short notice.
{
  const before = {
    ...initialState,
    seq: 2,
    items: [{ kind: "user" as const, id: "u1", text: "update it" }],
  };
  const complete = ev(before, {
    kind: "completion_summary",
    completion: {
      preset: "balanced",
      verdict: "complete",
      mutations: 3,
      checks_passed: 12,
      checks_failed: 0,
      checks_suppressed: 0,
      review: "passed",
      gap_kinds: [],
      constraint_degraded: false,
      floor: "standard",
      attention: false,
    },
  });
  eq(complete.items.length, before.items.length + 1, "workspace mutations add a neutral change notice");
  const changeNotice = complete.items[complete.items.length - 1];
  eq(changeNotice?.kind === "notice" ? changeNotice.level : "", "info", "workspace mutations use an info notice, not a warning");
  eq(changeNotice?.kind === "notice" ? changeNotice.completionSummary : undefined, complete.completionSummary, "neutral change notice retains its own normalized summary");
  eq(complete.completionSummary?.preset, "balanced", "ordinary completion summary remains available to the change panel");

  const after = ev(complete, {
    kind: "completion_summary",
    completion: {
      preset: "balanced",
      verdict: "partial",
      mutations: 3,
      checks_passed: 12,
      checks_failed: 1,
      checks_suppressed: 2,
      review: "passed",
      gap_kinds: ["stale_check"],
      constraint_degraded: true,
      floor: "delivery",
      attention: true,
    },
  });
  eq(after.items.length, complete.items.length + 1, "actionable completion summary adds one compact transcript notice");
  const notice = after.items[after.items.length - 1];
  eq(notice?.kind === "notice" ? notice.variant : "", "completion", "quality gap uses the completion notice variant");
  eq(notice?.kind === "notice" ? notice.action : "", "open_changes", "quality gap links to the change panel");
  eq(notice?.kind === "notice" ? notice.completionSummary : undefined, after.completionSummary, "completion notice retains its own normalized summary");
  eq(notice?.kind === "notice" ? notice.text.includes("balanced") : true, false, "compact notice does not expose internal preset values");
  eq(after.completionSummary?.checks_failed, 1, "actionable completion summary is retained for details");

  const switchedFloor = ev({
    ...complete,
    meta: { label: "test", ready: true, eventChannel: "", cwd: "/repo", qualityFloor: "delivery" },
  }, {
    kind: "completion_summary",
    completion: {
      ...complete.completionSummary!,
      verdict: "partial",
      gap_kinds: ["unverified_change"],
      floor: "standard",
      attention: false,
    },
  });
  const switchedNotice = switchedFloor.items[switchedFloor.items.length - 1];
  eq(switchedNotice?.kind === "notice" ? switchedNotice.level : "", "info", "turn-time standard summary stays neutral after switching to delivery");

  const suppressed = ev(complete, {
    kind: "completion_summary",
    completion: {
      ...complete.completionSummary!,
      mutations: 0,
      checks_suppressed: 1,
      gap_kinds: ["suppressed_requirement"],
      floor: "delivery",
      attention: true,
    },
  });
  const suppressedNotice = suppressed.items[suppressed.items.length - 1];
  eq(suppressedNotice?.kind === "notice" ? suppressedNotice.title : "", "This turn still needs attention", "required suppression uses a generic attention notice");

  const restarted = ev(after, { kind: "turn_started" });
  eq(restarted.completionSummary, undefined, "a new turn clears the previous turn's quality details");
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

  s = ev(s, { kind: "tool_dispatch", tool: {
    id: "c1",
    name: "write_file",
    args: '{"path":"a"}',
    readOnly: false,
    refreshed: true,
    resolvedName: "mcp__db__write",
    capabilityId: "mcp-tool:db/write",
  } } as WireEvent);
  const resolved = s.items.find((it) => it.kind === "tool" && it.id === "c1");
  eq(resolved?.kind === "tool" ? resolved.resolvedName : "", "mcp__db__write", "same-ID refresh stores resolved target");
  eq(resolved?.kind === "tool" ? resolved.capabilityId : "", "mcp-tool:db/write", "same-ID refresh stores capability id");
  eq(resolved?.kind === "tool" ? resolved.readOnly : true, false, "same-ID refresh replaces proxy read-only classification");

  s = ev(s, { kind: "tool_result", tool: {
    id: "c1",
    name: "use_capability",
    args: '{"action":"call","capability_id":"mcp-tool:db/write-v2"}',
    readOnly: false,
    resolvedName: "mcp__db__write_v2",
    capabilityId: "mcp-tool:db/write-v2",
    output: "done",
  } } as WireEvent);
  const completed = s.items.find((it) => it.kind === "tool" && it.id === "c1");
  eq(completed?.kind === "tool" ? completed.resolvedName : "", "mcp__db__write_v2", "tool result also refreshes resolved target");
  eq(completed?.kind === "tool" ? completed.capabilityId : "", "mcp-tool:db/write-v2", "tool result also refreshes capability id");
  eq(completed?.kind === "tool" ? completed.readOnly : true, false, "tool result preserves resolved writer classification");

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

// --- 1c. stream_attempt discard rolls back partial tool cards and text ---
{
  let s = { ...initialState, running: true, turnActive: true };
  s = ev(s, { kind: "stream_attempt", streamAttempt: { id: "sa-1", action: "begin", attempt: 1, max: 6 } } as WireEvent);
  s = ev(s, { kind: "text", text: "partial half" } as WireEvent);
  s = ev(s, { kind: "tool_dispatch", tool: { id: "c1", name: "edit_file", readOnly: false, partial: true, argChars: 6000, attemptId: "sa-1" } } as WireEvent);
  // Concurrent background sub-agent tool must not be journaled.
  s = ev(s, { kind: "tool_dispatch", tool: { id: "child-1", name: "read_file", readOnly: true, partial: true, parentId: "task-1", attemptId: "sa-1" } } as WireEvent);
  eq(s.items.filter((it) => it.kind === "tool" && it.id === "c1").length, 1, "partial edit_file card appears during attempt");
  eq(s.items.filter((it) => it.kind === "tool" && it.id === "child-1").length, 1, "sub-agent partial is still shown");
  eq(s.live?.text, "partial half", "partial text is live during attempt");
  eq(s.turnArgChars, 6000, "arg progress tracked during attempt");

  s = ev(s, { kind: "stream_attempt", streamAttempt: { id: "sa-1", action: "discard", attempt: 1, max: 6, reason: "premature_eof" } } as WireEvent);
  eq(s.items.filter((it) => it.kind === "tool" && it.id === "c1").length, 0, "discard removes uncommitted parent tool card");
  eq(s.items.filter((it) => it.kind === "tool" && it.id === "child-1").length, 1, "discard keeps concurrent sub-agent tool card");
  eq(s.live?.text ?? "", "", "discard clears attempt text (not concatenate)");
  eq(s.turnArgChars, 0, "discard restores turnArgChars baseline");

  s = ev(s, { kind: "stream_attempt", streamAttempt: { id: "sa-2", action: "begin", attempt: 2, max: 6 } } as WireEvent);
  s = ev(s, { kind: "text", text: "full answer" } as WireEvent);
  s = ev(s, { kind: "tool_dispatch", tool: { id: "c2", name: "edit_file", readOnly: false, partial: true, argChars: 12000, attemptId: "sa-2" } } as WireEvent);
  s = ev(s, { kind: "stream_attempt", streamAttempt: { id: "sa-2", action: "commit", attempt: 2, max: 6 } } as WireEvent);
  s = ev(s, { kind: "tool_dispatch", tool: { id: "c2", name: "edit_file", args: '{"path":"a"}', readOnly: false } } as WireEvent);
  eq(s.items.filter((it) => it.kind === "tool" && it.id === "c2").length, 1, "final success has committed parent tool card");
  eq(s.items.filter((it) => it.kind === "tool" && it.id === "child-1").length, 1, "sub-agent card still present after commit");
  eq(s.live?.text, "full answer", "final text is the committed attempt only");
}

// --- 1d. stale discard must not clear a newer attempt journal ---
{
  let s = { ...initialState, running: true, turnActive: true };
  s = ev(s, { kind: "stream_attempt", streamAttempt: { id: "sa-new", action: "begin", attempt: 2, max: 6 } } as WireEvent);
  s = ev(s, { kind: "tool_dispatch", tool: { id: "c-new", name: "edit_file", readOnly: false, partial: true, attemptId: "sa-new" } } as WireEvent);
  eq(s.streamAttemptJournal?.id, "sa-new", "journal tracks current attempt");
  s = ev(s, { kind: "stream_attempt", streamAttempt: { id: "sa-old", action: "discard", attempt: 1, max: 6, reason: "premature_eof" } } as WireEvent);
  eq(s.streamAttemptJournal?.id, "sa-new", "stale discard leaves current journal");
  eq(s.items.filter((it) => it.kind === "tool" && it.id === "c-new").length, 1, "stale discard does not remove current partial card");
  s = ev(s, { kind: "turn_done" } as WireEvent);
  eq(s.streamAttemptJournal, undefined, "turn_done clears stream attempt journal");
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

// --- 4. retrying is authoritative foreground activity ---
{
  let s = ev({ ...initialState }, { kind: "turn_started" } as WireEvent);
  s = reducer(s, {
    type: "backend_status",
    running: false,
    pendingPrompt: false,
    backgroundJobs: 0,
    cancelRequested: false,
    cancellable: false,
  });
  eq(s.running, false, "stale idle snapshot reproduces the hidden-stop state");

  s = ev(s, { kind: "retrying", retryAttempt: 3, retryMax: 10 } as WireEvent);
  eq(s.retry?.attempt, 3, "retry status keeps the current attempt");
  eq(s.retry?.max, 10, "retry status keeps the retry budget");
  eq(s.running, true, "retry event restores the active turn");
  eq(s.turnActive, true, "retry event restores the turn epoch");
  eq(s.cancellable, true, "retry event keeps Stop and Escape cancellation available");
  eq(s.turnStartAt > 0, true, "retry event restores timing for a reattached turn");

  const repaired = s;
  const completed = ev(repaired, { kind: "turn_done" } as WireEvent);
  eq(completed.running, false, "turn_done still ends the repaired turn");
  eq(completed.retry, undefined, "turn_done clears the retry indicator");

  const failed = ev(repaired, { kind: "turn_done", err: "shared window overflow" } as WireEvent);
  eq(failed.running, false, "terminal context error clears running");
  eq(failed.pendingPrompt, false, "terminal context error clears pending prompt");
  eq(failed.messageAction, undefined, "terminal context error restores message actions");

  const staleSnapshotAt = promptEventClock();
  s = ev(repaired, { kind: "retrying", retryAttempt: 4, retryMax: 10 } as WireEvent);
  s = reducer(s, {
    type: "backend_status",
    running: false,
    pendingPrompt: false,
    backgroundJobs: 0,
    cancelRequested: false,
    cancellable: false,
    snapshotAt: staleSnapshotAt,
  });
  eq(s.running, true, "idle snapshot fetched before retry cannot hide Stop when it returns later");
  eq(s.turnActive, true, "idle snapshot fetched before retry cannot end the active turn");
  eq(s.cancellable, true, "idle snapshot fetched before retry preserves cancellation");
  eq(s.retry?.attempt, 4, "stale idle snapshot preserves the newer retry status");

  s = reducer(s, {
    type: "backend_status",
    running: false,
    pendingPrompt: false,
    backgroundJobs: 0,
    cancelRequested: false,
    cancellable: false,
    snapshotAt: Number.MAX_SAFE_INTEGER,
  });
  eq(s.running, false, "fresh idle snapshot can reconcile a missed turn_done");
  eq(s.retry, undefined, "fresh idle snapshot clears the retry indicator");
}

// --- 5. TPS telemetry excludes tool gaps and preserves fallback estimates ---
{
  const originalNow = Date.now;
  let now = 1_000;
  Date.now = () => now;
  try {
    let s = ev({ ...initialState }, { kind: "turn_started" } as WireEvent);
    now = 1_100;
    s = ev(s, { kind: "text", text: "abcd" } as WireEvent);
    now = 2_100;
    s = ev(s, { kind: "usage", usage: { promptTokens: 10, completionTokens: 4, totalTokens: 14, cacheHitTokens: 0, cacheMissTokens: 10 } } as WireEvent);
    s = ev(s, { kind: "message", text: "abcd" } as WireEvent);
    eq(s.turnModelActiveMs, 1_000, "first provider output interval is accumulated");
    eq(s.turnOutputCharsAtUsage, 0, "completed assistant message resets the live-character baseline");

    // A long tool gap must not lower TPS for the next provider request.
    now = 8_000;
    s = ev(s, { kind: "text", text: "abcdefgh" } as WireEvent);
    now = 9_000;
    s = ev(s, { kind: "turn_done" } as WireEvent);
    eq(s.lastTurnOutputTokens, 6, "missing final usage adds only the in-flight character estimate");
    eq(s.lastTurnModelMs, 2_000, "tool gap is excluded from completed TPS duration");
    eq(s.lastTurnOutputEstimated, true, "missing final usage marks completed TPS as estimated");

    // Providers that omit per-request usage must still close the first model
    // interval before the tool runs.
    now = 12_000;
    s = ev({ ...initialState }, { kind: "turn_started" } as WireEvent);
    now = 12_100;
    s = ev(s, { kind: "text", text: "abcd" } as WireEvent);
    now = 13_100;
    s = ev(s, { kind: "message", text: "abcd" } as WireEvent);
    s = ev(s, { kind: "tool_dispatch", tool: { id: "missing-usage", name: "read_file", args: "{}", readOnly: true } } as WireEvent);
    now = 19_000;
    s = ev(s, { kind: "text", text: "efgh" } as WireEvent);
    now = 20_000;
    s = ev(s, { kind: "turn_done" } as WireEvent);
    eq(s.lastTurnOutputTokens, 2, "missing usage estimates output across provider requests");
    eq(s.lastTurnModelMs, 2_000, "missing usage still excludes the tool gap");

    now = 21_000;
    s = ev({ ...initialState }, { kind: "turn_started" } as WireEvent);
    now = 21_100;
    s = ev(s, { kind: "text", text: "abcd" } as WireEvent);
    now = 22_100;
    s = ev(s, { kind: "usage", usage: { promptTokens: 10, completionTokens: 1, totalTokens: 11, cacheHitTokens: 0, cacheMissTokens: 10, estimated: true } } as WireEvent);
    s = ev(s, { kind: "turn_done" } as WireEvent);
    eq(s.lastTurnOutputEstimated, true, "provider-estimated usage marks completed TPS as estimated");

    now = 23_000;
    s = ev({ ...initialState }, { kind: "turn_started" } as WireEvent);
    s = ev(s, { kind: "text", text: "abcd" } as WireEvent);
    now = 24_000;
    s = reducer(s, {
      type: "backend_status",
      running: false,
      pendingPrompt: false,
      backgroundJobs: 0,
      cancelRequested: false,
      cancellable: false,
    });
    eq(s.lastTurnOutputTokens, 1, "idle reconciliation snapshots fallback output telemetry");
    eq(s.lastTurnModelMs, 1_000, "idle reconciliation closes the active provider interval");
    eq(s.lastTurnOutputEstimated, true, "idle reconciliation preserves the estimated marker");
  } finally {
    Date.now = originalNow;
  }
}

// --- 6. TPS telemetry follows executor output-token semantics and retry intervals ---
{
  const originalNow = Date.now;
  let now = 30_000;
  Date.now = () => now;
  try {
    let s = ev({ ...initialState }, { kind: "turn_started" } as WireEvent);
    now = 30_100;
    s = ev(s, { kind: "text", text: "abcd" } as WireEvent);
    now = 31_100;
    s = ev(s, { kind: "usage", usage: {
      promptTokens: 100,
      completionTokens: 20,
      reasoningTokens: 10,
      totalTokens: 120,
      cacheHitTokens: 0,
      cacheMissTokens: 100,
      source: "executor",
    } } as WireEvent);
    s = ev(s, { kind: "turn_done" } as WireEvent);
    eq(s.lastTurnOutputTokens, 20, "reasoning tokens are not added twice to completed TPS");
    eq(s.lastTurnModelMs, 1_000, "reasoning usage preserves the executor output interval");

    now = 32_000;
    s = ev({ ...initialState }, { kind: "turn_started" } as WireEvent);
    now = 32_100;
    s = ev(s, { kind: "text", text: "abcd" } as WireEvent);
    now = 32_500;
    s = ev(s, { kind: "usage", usage: {
      promptTokens: 50,
      completionTokens: 100,
      totalTokens: 150,
      cacheHitTokens: 0,
      cacheMissTokens: 50,
      source: "subagent",
    } } as WireEvent);
    now = 33_100;
    s = ev(s, { kind: "usage", usage: {
      promptTokens: 10,
      completionTokens: 10,
      totalTokens: 20,
      cacheHitTokens: 0,
      cacheMissTokens: 10,
      source: "executor",
    } } as WireEvent);
    s = ev(s, { kind: "turn_done" } as WireEvent);
    eq(s.lastTurnOutputTokens, 10, "subagent usage is excluded from executor TPS tokens");
    eq(s.lastTurnModelMs, 1_000, "subagent usage does not close the executor output interval");

    now = 34_000;
    s = ev({ ...initialState }, { kind: "turn_started" } as WireEvent);
    s = ev(s, { kind: "stream_attempt", streamAttempt: { id: "tps-a1", action: "begin", attempt: 1, max: 2 } } as WireEvent);
    now = 34_100;
    s = ev(s, { kind: "text", text: "abcdefgh" } as WireEvent);
    now = 35_100;
    s = ev(s, { kind: "stream_attempt", streamAttempt: { id: "tps-a1", action: "discard", attempt: 1, max: 2 } } as WireEvent);
    now = 38_000;
    s = ev(s, { kind: "stream_attempt", streamAttempt: { id: "tps-a2", action: "begin", attempt: 2, max: 2 } } as WireEvent);
    s = ev(s, { kind: "text", text: "abcd" } as WireEvent);
    now = 39_000;
    s = ev(s, { kind: "turn_done" } as WireEvent);
    eq(s.lastTurnModelMs, 2_000, "discarded sampling attempts exclude retry backoff from TPS");
  } finally {
    Date.now = originalNow;
  }
}

// --- 7. lastRequestTps pairs the closed interval with the usage tokens ---
{
  const originalNow = Date.now;
  let now = 50_000;
  Date.now = () => now;
  try {
    let s = ev({ ...initialState }, { kind: "turn_started" } as WireEvent);
    now = 50_100;
    s = ev(s, { kind: "text", text: "abcd" } as WireEvent);
    now = 51_100;
    // The message event closes the interval BEFORE the usage event arrives.
    s = ev(s, { kind: "message", text: "abcd" } as WireEvent);
    now = 51_200;
    s = ev(s, { kind: "usage", usage: {
      promptTokens: 130, completionTokens: 30, totalTokens: 160,
      contextPromptTokens: 100, contextCompletionTokens: 20,
      cacheHitTokens: 0, cacheMissTokens: 100, source: "executor",
    } } as WireEvent);
    eq(s.lastRequestTps, 20, "sampling recovery pairs the interval with latest-attempt tokens");

    now = 52_000;
    s = ev(s, { kind: "text", text: "more" } as WireEvent);
    now = 52_050;
    s = ev(s, { kind: "message", text: "more" } as WireEvent);
    now = 52_100;
    s = ev(s, { kind: "usage", usage: {
      promptTokens: 5, completionTokens: 50, totalTokens: 55,
      cacheHitTokens: 0, cacheMissTokens: 5, source: "subagent",
    } } as WireEvent);
    eq(s.lastRequestTps, 20, "non-executor usage neither computes nor consumes the pending interval");
    now = 52_300;
    s = ev(s, { kind: "usage", usage: {
      promptTokens: 10, completionTokens: 30, totalTokens: 40,
      cacheHitTokens: 0, cacheMissTokens: 10, source: "executor",
    } } as WireEvent);
    eq(s.lastRequestTps, null, "intervals under the 500ms gate clear stale request TPS");

    now = 53_000;
    s = ev(s, { kind: "text", text: "second" } as WireEvent);
    now = 54_000;
    s = ev(s, { kind: "message", text: "second" } as WireEvent);
    now = 54_100;
    s = ev(s, { kind: "usage", usage: {
      promptTokens: 10, completionTokens: 30, totalTokens: 40,
      cacheHitTokens: 0, cacheMissTokens: 10, source: "executor",
    } } as WireEvent);
    eq(s.lastRequestTps, 30, "a later executor usage refreshes the request TPS");

    now = 55_000;
    s = ev(s, { kind: "text", text: "direct" } as WireEvent);
    now = 56_000;
    s = ev(s, { kind: "usage", usage: {
      promptTokens: 10, completionTokens: 40, totalTokens: 50,
      cacheHitTokens: 0, cacheMissTokens: 10, source: "executor",
    } } as WireEvent);
    eq(s.lastRequestTps, 40, "usage measures an interval still open at arrival");

    now = 57_000;
    s = ev(s, { kind: "turn_done" } as WireEvent);
    eq(s.lastRequestTps, 40, "request TPS persists across turn boundaries");

    now = 58_000;
    s = ev(s, { kind: "turn_started" } as WireEvent);
    now = 58_100;
    s = ev(s, { kind: "usage", usage: {
      promptTokens: 10, completionTokens: 8, totalTokens: 18,
      cacheHitTokens: 0, cacheMissTokens: 10, source: "executor",
    } } as WireEvent);
    eq(s.lastRequestTps, null, "usage without a provider interval clears stale request TPS");
    now = 58_200;
    s = ev(s, { kind: "tool_dispatch", tool: { id: "final-only", name: "read_file", args: "{}", readOnly: true } } as WireEvent);
    eq(s.lastRequestTps, null, "a final-only tool dispatch after usage cannot resurrect stale TPS");

    now = 59_000;
    s = ev(s, { kind: "text", text: "toolcall" } as WireEvent);
    now = 60_000;
    s = ev(s, { kind: "tool_dispatch", tool: { id: "t1", name: "read_file", args: "{}", readOnly: true } } as WireEvent);
    now = 60_100;
    s = ev(s, { kind: "usage", usage: {
      promptTokens: 10, completionTokens: 25, totalTokens: 35,
      cacheHitTokens: 0, cacheMissTokens: 10, source: "executor",
    } } as WireEvent);
    eq(s.lastRequestTps, 25, "tool_dispatch closes the interval the next executor usage pairs with");

    now = 61_000;
    s = ev(s, { kind: "tool_dispatch", tool: { id: "t2", name: "write_file", readOnly: false, partial: true, argChars: 600 } } as WireEvent);
    now = 62_000;
    s = ev(s, { kind: "usage", usage: {
      promptTokens: 10, completionTokens: 30, totalTokens: 40,
      cacheHitTokens: 0, cacheMissTokens: 10, source: "executor",
    } } as WireEvent);
    eq(s.lastRequestTps, 30, "usage closes the interval started by a partial tool dispatch");
    now = 62_100;
    s = ev(s, { kind: "tool_dispatch", tool: { id: "t2", name: "write_file", args: "{}", readOnly: false } } as WireEvent);
    eq(s.lastRequestTps, 30, "the later full tool dispatch preserves the measured request TPS");

    now = 63_000;
    s = ev(s, { kind: "text", text: "closing" } as WireEvent);
    now = 64_000;
    s = ev(s, { kind: "message", text: "closing" } as WireEvent);
    now = 64_100;
    s = ev(s, { kind: "tool_dispatch", tool: { id: "t3", name: "write_file", readOnly: false, partial: true, argChars: 300 } } as WireEvent);
    now = 64_700;
    s = ev(s, { kind: "tool_dispatch", tool: { id: "t3", name: "write_file", args: "{}", readOnly: false } } as WireEvent);
    now = 64_800;
    s = ev(s, { kind: "usage", usage: {
      promptTokens: 10, completionTokens: 30, totalTokens: 40,
      cacheHitTokens: 0, cacheMissTokens: 10, source: "executor",
    } } as WireEvent);
    // The partial restart begins a new interval; the full dispatch closes it
    // and overwrites the message-stashed pending with its own (≥500ms) tail.
    eq(s.lastRequestTps, 50, "a full dispatch overwrites a message-stashed pending with its own tail close");

    now = 65_000;
    s = ev(s, { kind: "text", text: "slow" } as WireEvent);
    now = 68_000;
    s = ev(s, { kind: "message", text: "slow" } as WireEvent);
    now = 68_100;
    s = ev(s, { kind: "usage", usage: {
      promptTokens: 10, completionTokens: 1, totalTokens: 11,
      cacheHitTokens: 0, cacheMissTokens: 10, source: "executor",
    } } as WireEvent);
    eq(s.lastRequestTps, 1 / 3, "slow measurable requests retain their raw sub-one TPS");
  } finally {
    Date.now = originalNow;
  }
}

// --- 8. context occupancy uses prompt tokens and keeps legacy fallback semantics ---
{
  let s = ev({
    ...initialState,
    running: true,
    turnActive: true,
    context: { ...initialState.context, window: 1_000 },
  }, { kind: "usage", usage: {
    promptTokens: 500,
    completionTokens: 20,
    totalTokens: 520,
    contextPromptTokens: 0,
    contextCompletionTokens: 20,
    source: "executor",
  } } as WireEvent);
  eq(s.context.used, 500, "completion-only latest usage falls back to aggregate prompt occupancy");

  s = ev({ ...s, running: true, turnActive: true }, { kind: "usage", usage: {
    promptTokens: 700,
    completionTokens: 30,
    totalTokens: 730,
    contextPromptTokens: 450,
    contextCompletionTokens: 0,
    source: "executor",
  } } as WireEvent);
  eq(s.context.used, 450, "latest-attempt prompt occupancy excludes completion tokens");
}

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
