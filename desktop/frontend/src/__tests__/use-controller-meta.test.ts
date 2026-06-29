// Run: tsx src/__tests__/use-controller-meta.test.ts

import { foregroundRunningFromRuntimeMeta, initialState, metaFromTab, reducer, sameMeta, shouldReconcileStaleTurn } from "../lib/useController";
import type { Meta, TabMeta, WireUsage } from "../lib/types";

type LooseTabMeta = Omit<TabMeta, "toolApprovalMode"> & { toolApprovalMode?: TabMeta["toolApprovalMode"] | "" };

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
    workspaceRoot: "/repo",
    workspaceName: "repo",
    workspacePath: "/repo",
    gitBranch: "main",
    imageInputEnabled: true,
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

function tab(overrides: Partial<LooseTabMeta> = {}): TabMeta {
  return {
    id: "tab-1",
    scope: "project",
    workspaceRoot: "/repo",
    workspaceName: "repo",
    workspacePath: "/repo",
    gitBranch: "main",
    topicId: "topic-1",
    topicTitle: "Topic",
    label: "DeepSeek-R1",
    ready: true,
    running: false,
    mode: "normal",
    collaborationMode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    goal: "",
    goalStatus: "stopped",
    active: true,
    cwd: "/repo",
    ...overrides,
  } as TabMeta;
}

function usage(source: string): WireUsage {
  return {
    promptTokens: 100,
    completionTokens: 20,
    totalTokens: 120,
    cacheHitTokens: 80,
    cacheMissTokens: 20,
    sessionCacheHitTokens: 80,
    sessionCacheMissTokens: 20,
    source,
    cost: 0.001,
    currency: "$",
  };
}

console.log("\nuse controller meta");

{
  eq(sameMeta(meta(), meta()), true, "identical meta is unchanged");
  eq(sameMeta(meta({ collaborationMode: "normal" }), meta({ collaborationMode: "plan" })), false, "collaboration mode changes invalidate meta equality");
  eq(sameMeta(meta({ workspacePath: "/repo" }), meta({ workspacePath: "/other" })), false, "workspace path changes invalidate meta equality");
  eq(sameMeta(meta({ gitBranch: "main" }), meta({ gitBranch: "feature" })), false, "git branch changes invalidate meta equality");
  eq(sameMeta(meta({ imageInputEnabled: true }), meta({ imageInputEnabled: false })), false, "image input capability changes invalidate meta equality");
}

{
  const preserved = metaFromTab(tab({ toolApprovalMode: "" }), meta({ toolApprovalMode: "auto", autoApproveTools: false }));
  eq(preserved.toolApprovalMode, "auto", "blank tab snapshot preserves explicit auto approval mode");
  eq(preserved.autoApproveTools, false, "blank tab snapshot does not silently resurrect yolo approval");
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

{
  const started = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
  const waiting = reducer(started, { type: "event", e: { kind: "approval_request", approval: { id: "1", tool: "bash", subject: "go test" } } });
  eq(waiting.running, true, "approval prompt keeps the turn running");
  eq(waiting.pendingPrompt, true, "approval prompt marks pendingPrompt");
  eq(waiting.cancellable, true, "approval prompt remains cancellable");

  const canceling = reducer(waiting, { type: "cancel_requested" });
  eq(canceling.approval, undefined, "cancel_requested clears approval prompt locally");
  eq(canceling.pendingPrompt, false, "cancel_requested clears pendingPrompt locally");
  eq(canceling.cancelRequested, true, "cancel_requested marks cancelling");
  eq(canceling.running, true, "cancel_requested waits for backend turn_done before idling");
  const stalePrompt = reducer(canceling, { type: "event", e: { kind: "approval_request", approval: { id: "late", tool: "bash", subject: "sleep" } } });
  eq(stalePrompt.approval, undefined, "late approval after cancel_requested stays hidden");

  const backgroundOnly = reducer(initialState, { type: "backend_status", running: false, backgroundJobs: 1, cancellable: false });
  eq(backgroundOnly.running, false, "background jobs alone do not make the composer runstatus active");
  eq(backgroundOnly.backgroundJobs, 1, "backend_status stores background job count");
  eq(backgroundOnly.cancellable, false, "background jobs alone are not foreground-cancellable");

  const omittedCancellableBackgroundOnly = reducer(initialState, { type: "backend_status", running: true, backgroundJobs: 1 });
  eq(omittedCancellableBackgroundOnly.running, false, "missing cancellable does not promote background-only metadata");
  eq(omittedCancellableBackgroundOnly.cancellable, false, "missing cancellable stays non-cancellable with background-only metadata");
  eq(foregroundRunningFromRuntimeMeta({ running: true }), true, "legacy running metadata remains foreground-running");
  eq(foregroundRunningFromRuntimeMeta({ running: true, pendingPrompt: true, backgroundJobs: 1 }), true, "pending prompts remain foreground-running");
  eq(foregroundRunningFromRuntimeMeta({ running: true, backgroundJobs: 1 }), false, "background jobs without cancellable are background-only");
}

{
  const restoredContext = reducer(initialState, {
    type: "context",
    context: {
      used: 42,
      window: 200,
      sessionTokens: 120,
      compactRatio: 0.5,
      sessionCost: 0.012,
      sessionCurrency: "$",
      cacheHitTokens: 80,
      cacheMissTokens: 20,
    },
  });
  const reset = reducer(restoredContext, { type: "reset" });
  eq(reset.context.used, 0, "reset clears context used tokens");
  eq(reset.context.window, 200, "reset preserves context window");
  eq(reset.context.sessionTokens, 0, "reset clears context session tokens");
  eq(reset.context.cacheHitTokens, undefined, "reset clears restored cache hit tokens");
  eq(reset.context.cacheMissTokens, undefined, "reset clears restored cache miss tokens");
  eq(reset.context.sessionCost, undefined, "reset clears restored context session cost");
  eq(reset.sessionCost, 0, "reset clears restored session cost state");
  eq(reset.sessionCurrency, "¥", "reset restores default session currency");
}

{
  const idleExecutor = reducer(
    { ...initialState, context: { used: 0, window: 200, sessionTokens: 0 } },
    { type: "event", e: { kind: "usage", usage: usage("executor") } },
  );
  eq(idleExecutor.sessionTokens, 0, "executor usage outside a turn does not inflate session tokens");
  eq(idleExecutor.context.used, 0, "executor usage outside a turn does not refresh context used tokens");

  const idleHelper = reducer(initialState, { type: "event", e: { kind: "usage", usage: usage("classifier") } });
  eq(idleHelper.sessionTokens, 0, "helper usage outside a turn does not inflate session tokens");
  eq(idleHelper.sessionCost, 0, "helper usage outside a turn does not inflate session cost");

  const pendingClassifier = reducer(
    { ...initialState, running: true, context: { used: 0, window: 200, sessionTokens: 0 } },
    { type: "event", e: { kind: "usage", usage: usage("classifier") } },
  );
  eq(pendingClassifier.sessionTokens, 120, "classifier usage while send is running counts toward session tokens");
  eq(pendingClassifier.sessionCost, 0.001, "classifier usage while send is running counts toward session cost");
  eq(pendingClassifier.context.used, 0, "classifier usage while send is running does not refresh context used tokens");

  const active = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
  const activeHelper = reducer(active, { type: "event", e: { kind: "usage", usage: usage("subagent") } });
  eq(activeHelper.sessionTokens, 120, "helper usage inside a turn still counts toward session tokens");
  eq(activeHelper.sessionCost, 0.001, "helper usage inside a turn still counts toward session cost");
}

{
  let s = reducer(initialState, { type: "user", text: "first", seq: 0 });
  s = reducer(s, { type: "event", e: { kind: "turn_started" } });
  s = reducer(s, { type: "event", e: { kind: "notice", level: "info", text: "runtime notice" } });
  s = reducer(s, { type: "event", e: { kind: "turn_done" } });
  const merged = reducer(s, {
    type: "history_checkpoint_turns",
    turns: [0],
  });
  const user = merged.items.find((item) => item.kind === "user");
  const notice = merged.items.find((item) => item.kind === "notice" && item.text === "runtime notice");
  eq(user?.kind === "user" && user.checkpointTurn, 0, "turn_done checkpoint merge stamps user turn zero");
  eq(Boolean(notice), true, "turn_done checkpoint merge preserves runtime notices");
}

{
  let s = reducer(initialState, {
    type: "history_page",
    mode: "replace",
    page: {
      messages: [
        { role: "user", content: "recent prompt" },
        { role: "assistant", content: "recent answer" },
      ],
      startTurn: 60,
      endTurn: 61,
      totalTurns: 61,
      hasOlder: true,
    },
  });
  eq(s.items.some((item) => item.kind === "user" && item.text === "recent prompt"), true, "history page replace renders the latest window");
  eq(s.historyStartTurn, 60, "history page stores the older cursor");
  eq(s.historyHasOlder, true, "history page records older availability");
  const checkpointed = reducer(s, {
    type: "history_checkpoint_turns",
    turns: Array.from({ length: 61 }, (_, index) => index + 1000),
  });
  const recentUser = checkpointed.items.find((item) => item.kind === "user" && item.text === "recent prompt");
  eq(recentUser?.kind === "user" && recentUser.checkpointTurn, 1060, "paged checkpoint merge uses the window start turn");
  s = reducer(s, { type: "history_older_start" });
  eq(s.historyOlderLoading, true, "older history request marks loading");
  s = reducer(s, {
    type: "history_page",
    mode: "prepend",
    page: {
      messages: [
        { role: "user", content: "older prompt" },
        { role: "assistant", content: "older answer" },
      ],
      startTurn: 0,
      endTurn: 1,
      totalTurns: 61,
      hasOlder: false,
    },
  });
  const users = s.items.filter((item) => item.kind === "user");
  eq(users[0]?.kind === "user" && users[0].text, "older prompt", "older history prepends before the current window");
  eq(users[1]?.kind === "user" && users[1].text, "recent prompt", "older history keeps the current window");
  eq(s.historyHasOlder, false, "older history clears hasOlder when all pages are loaded");
  eq(s.historyOlderLoading, false, "older history clears loading");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
