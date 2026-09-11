// Run: tsx src/__tests__/session-diagnostics.test.ts
//
// Phase F session-switch/history diagnostics: activation lifecycle timings,
// history page counters (index hit/miss, stale), mounted-row counts, provider
// registration, and the bounded activation log.

import {
  activationFailureClass,
  activationLog,
  noteActivationRequested,
  noteActivationSettled,
  noteActivationStarted,
  noteHistoryPage,
  noteTranscriptRowCounts,
  registerMarkdownWorkerDiagnostics,
  registerTranscriptCacheDiagnostics,
  resetSessionDiagnostics,
  sessionPipelineDiagnostics,
} from "../lib/sessionDiagnostics";

let passed = 0;
let failed = 0;

function ok(cond: boolean, label: string) {
  if (cond) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

resetSessionDiagnostics();

// --- activation lifecycle with derived phase timings ---
{
  noteActivationRequested("r1");
  noteActivationStarted("r1", "tab_a");
  noteActivationSettled("r1", "ready");
  const snapshot = sessionPipelineDiagnostics();
  const a = snapshot.activation;
  ok(Boolean(a), "last activation present in snapshot");
  ok(a?.tabId === "tab_a", "activation carries the tab id");
  ok(a?.ticketToStartingMs !== undefined && a.ticketToStartingMs >= 0, "ticket→starting derived");
  ok(a?.startingToReadyMs !== undefined && a.startingToReadyMs >= 0, "starting→ready derived");
  ok(a?.totalMs !== undefined && a.totalMs >= (a?.startingToReadyMs ?? 0), "total covers the phases");
  ok(a?.failureClass === undefined, "ready activation has no failure class");
}

// --- failed activation records only the failure class, never the message ---
{
  noteActivationRequested("r2");
  noteActivationSettled("r2", "failed", "dial tcp 10.0.0.1: connect: connection timeout");
  const a = sessionPipelineDiagnostics().activation;
  ok(a?.outcome === "failed", "failed outcome recorded");
  ok(a?.failureClass === "timeout", "timeout classified");
  ok(activationFailureClass("authorization cancelled by user") === "cancelled", "cancel class");
  ok(activationFailureClass("cursor is stale") === "stale", "stale class");
  ok(activationFailureClass("session file not found") === "missing", "missing class");
  ok(activationFailureClass("weird backend state") === "other", "other class");
  ok(activationFailureClass("") === "unknown", "empty error is unknown");
}

// --- first settle wins (a stale terminal event can't rewrite the outcome) ---
{
  noteActivationRequested("r3");
  noteActivationSettled("r3", "cancelled");
  noteActivationSettled("r3", "ready");
  const log = activationLog();
  ok(log.find((a) => a.requestId === "r3")?.outcome === "cancelled", "first terminal phase wins");
}

// --- history page counters: index hits/misses and stale ---
{
  noteHistoryPage({ entries: 120, inlineBytes: 64_000, durationMs: 3, stale: false, source: "live-index" });
  noteHistoryPage({ entries: 40, inlineBytes: 10_000, durationMs: 12, stale: false, source: "scan" });
  noteHistoryPage({ entries: 0, inlineBytes: 0, durationMs: 1, stale: true, source: "index" });
  const h = sessionPipelineDiagnostics().history;
  ok(h?.pages === 3, "all pages counted");
  ok(h?.staleCount === 1, "stale pages counted");
  ok(h?.indexHits === 2, "index + live-index count as hits");
  ok(h?.indexMisses === 1, "scan counts as a miss");
  ok(h?.entries === 0 && h?.stale === true, "last page stats win");
}

// --- mounted rows + registered providers ---
{
  noteTranscriptRowCounts(30, 412);
  registerMarkdownWorkerDiagnostics(() => ({ pending: 1, completed: 9, avgParseMs: 4, maxParseMs: 11, fallbackActive: false, workerFailures: 0 }));
  registerTranscriptCacheDiagnostics(() => ({
    residentSessions: 2,
    maxResidentSessions: 3,
    bodyBytes: 1024,
    bodyBudgetBytes: 32 << 20,
    markdownBytes: 2048,
    markdownBudgetBytes: 16 << 20,
    historyEvictions: 1,
    markdownEvictions: 0,
  }));
  const snapshot = sessionPipelineDiagnostics();
  ok(snapshot.mountedRows?.mounted === 30 && snapshot.mountedRows.total === 412, "mounted row counts flow through");
  ok(snapshot.markdownWorker?.completed === 9, "markdown worker provider flows through");
  ok(snapshot.transcriptCache?.residentSessions === 2 && snapshot.transcriptCache.historyEvictions === 1, "cache provider flows through");
}

// --- a throwing provider never breaks the snapshot ---
{
  registerMarkdownWorkerDiagnostics(() => {
    throw new Error("boom");
  });
  const snapshot = sessionPipelineDiagnostics();
  ok(snapshot.markdownWorker === undefined && snapshot.transcriptCache !== undefined, "throwing provider is isolated");
}

// --- activation log is bounded ---
{
  resetSessionDiagnostics();
  for (let i = 0; i < 200; i += 1) noteActivationRequested(`flood-${i}`);
  const log = activationLog();
  ok(log.length === 128, `activation log bounded at 128 (got ${log.length})`);
  ok(log[log.length - 1].requestId === "flood-199", "newest entries kept");
}

// --- reset clears everything ---
{
  resetSessionDiagnostics();
  const snapshot = sessionPipelineDiagnostics();
  ok(snapshot.activation === undefined && snapshot.history === undefined, "reset drops activation/history");
  ok(activationLog().length === 0, "reset clears the log");
}

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
