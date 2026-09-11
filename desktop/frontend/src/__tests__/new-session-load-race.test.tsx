// Run: tsx src/__tests__/new-session-load-race.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { initialState, reducer, runtimeReadyForSubmit, useController, type Item } from "../lib/useController";
import type { NavigationResult } from "../lib/navigationSurfaceTransition";
import { historySliceFromMessages } from "./mockHistorySlice";
import type { AppBindings } from "../lib/bridge";
import type { BalanceInfo, CheckpointMeta, ContextInfo, EffortInfo, HistoryMessage, HistorySliceRequest, JobView, Meta, TabMeta, WireEvent } from "../lib/types";
import { installDesktopHostStub } from "./desktopHostStub";
import { meta, tabMeta } from "./helpers/sessionSwitchFixtures";
import { resetSessionDiagnostics, sessionPipelineDiagnostics } from "../lib/sessionDiagnostics";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    ok(true, label);
  } else {
    ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
  }
}

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    await act(async () => {
      await flushPromises();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}


console.log("\nnew session load race");

const resetSourceItems: Item[] = [{ kind: "user", id: "old-user", text: "old prompt" }];
const resetPlaceholderItems: Item[] = [{ kind: "user", id: "placeholder-user", text: "placeholder prompt" }];
const resetState = reducer(
  {
    ...initialState,
    items: resetSourceItems,
    hydrating: true,
    hydrateReason: "open-topic",
    hydratePlaceholderItems: resetPlaceholderItems,
  },
  { type: "reset" },
);
eq(resetState.items.length, 0, "reset clears real transcript items");
eq(resetState.hydratePlaceholderItems?.length, 1, "reset preserves hydration placeholder separately");

const emptyHistoryState = reducer(resetState, { type: "history", messages: [] });
eq(emptyHistoryState.items.length, 0, "empty history keeps the real transcript empty");
eq(emptyHistoryState.hydrateHistoryLoaded, true, "empty history marks transcript hydration loaded");
eq(emptyHistoryState.hydratePlaceholderItems?.length ?? 0, 0, "empty history clears hydration placeholder items");

const hydrateDoneState = reducer(emptyHistoryState, { type: "hydrate_done" });
eq(Boolean(hydrateDoneState.hydrateHistoryLoaded), false, "hydrate_done clears the history-loaded marker");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

const staleHistory = deferred<HistoryMessage[]>();
const staleSessionMeta = deferred<Meta>();
let newSessionCalls = 0;
let backendCanonicalTodos = [{ content: "Old task", status: "in_progress" }];
let holdNextMeta = false;
let staleMetaStarted = false;
let backendRuntimeEpoch = "runtime-old";
let backendPendingPrompt = false;
let promptReplayCalls = 0;
const resumeRPCGate = deferred<void>();
const channelRPCGate = deferred<void>();
const context: ContextInfo = { used: 12, window: 100, sessionTokens: 12 };
const effort: EffortInfo = { supported: true, current: "auto", default: "auto", levels: ["auto"] };
const balance: BalanceInfo = { available: false, display: "" };
const jobs: JobView[] = [];
const checkpoints: CheckpointMeta[] = [];

const appStubTable = {
      RegisterNavigationIntent: async () => {},
      ListTabs: async () => {
        return [tabMeta({
          runtime: { phase: "ready", epoch: backendRuntimeEpoch },
          running: backendPendingPrompt,
          pendingPrompt: backendPendingPrompt,
          cancellable: backendPendingPrompt,
        })];
      },
      MetaForTab: async () => {
        if (holdNextMeta) {
          holdNextMeta = false;
          staleMetaStarted = true;
          return staleSessionMeta.promise;
        }
        return meta({ canonicalTodos: backendCanonicalTodos, runtime: { phase: "ready", epoch: backendRuntimeEpoch } });
      },
      ContextUsageForTab: async () => context,
      EffortForTab: async () => effort,
      BalanceForTab: async () => balance,
      JobsForTab: async () => jobs,
      CheckpointsForTab: async () => checkpoints,
      HistoryForTab: async () => staleHistory.promise,
      HistoryPageForTab: async () => {
        const messages = await staleHistory.promise;
        return { messages, startTurn: 0, endTurn: messages.filter((message) => message.role === "user").length, totalTurns: messages.filter((message) => message.role === "user").length, hasOlder: false };
      },
      HistorySliceForTab: async (tabID: string, req: HistorySliceRequest) =>
        historySliceFromMessages(tabID, await staleHistory.promise, req),
      HistoryCheckpointTurnsForTab: async () => [],
      ReplayPendingPrompts: async () => {},
      ReplayPendingPromptsForTab: async (tabID: string) => {
        promptReplayCalls += 1;
        if (tabID !== "tab-a" || !backendPendingPrompt) return;
        desktopStub.emit("agent:event", {
            kind: "ask_request",
            tabId: tabID,
            runtimeEpoch: backendRuntimeEpoch,
            ask: { id: `replayed-${backendRuntimeEpoch}`, questions: [{ id: "choice", prompt: "Recovered after hydration", options: [] }] },
          });
      },
      NewSession: async () => {
        newSessionCalls += 1;
        backendCanonicalTodos = [];
      },
      NewSessionForTab: async (tabID: string) => {
        if (tabID !== "tab-a") throw new Error(`unexpected new-session target ${tabID}`);
        newSessionCalls += 1;
        backendCanonicalTodos = [];
      },
      ResumeSessionPageForTab: async () => {
        backendCanonicalTodos = [{ content: "Restored task", status: "completed" }];
        const oldEpoch = backendRuntimeEpoch;
        backendRuntimeEpoch = "runtime-resumed";
        backendPendingPrompt = true;
        desktopStub.emit("runtime:rebuilt", "tab-a", backendRuntimeEpoch);
        desktopStub.emit("agent:event", {
            kind: "ask_request",
            tabId: "tab-a",
            runtimeEpoch: oldEpoch,
            ask: { id: "stale-old-epoch", questions: [{ id: "choice", prompt: "Stale", options: [] }] },
          });
desktopStub.emit("agent:event", {
            kind: "ask_request",
            tabId: "tab-a",
            runtimeEpoch: backendRuntimeEpoch,
            ask: { id: "pre-response-resume", questions: [{ id: "choice", prompt: "Before Resume RPC returns", options: [] }] },
          });
        await resumeRPCGate.promise;
        return {
          messages: [{ role: "user", content: "restore" }, { role: "assistant", content: "done" }],
          startTurn: 0,
          endTurn: 1,
          totalTurns: 1,
          hasOlder: false,
        };
      },
      OpenChannelSessionPageForTab: async () => {
        const oldEpoch = backendRuntimeEpoch;
        backendRuntimeEpoch = "runtime-channel";
        backendPendingPrompt = true;
        desktopStub.emit("runtime:rebuilt", "tab-a", backendRuntimeEpoch);
        desktopStub.emit("agent:event", {
            kind: "ask_request",
            tabId: "tab-a",
            runtimeEpoch: oldEpoch,
            ask: { id: "stale-channel-old-epoch", questions: [{ id: "choice", prompt: "Stale channel", options: [] }] },
          });
desktopStub.emit("agent:event", {
            kind: "ask_request",
            tabId: "tab-a",
            runtimeEpoch: backendRuntimeEpoch,
            ask: { id: "pre-response-channel", questions: [{ id: "choice", prompt: "Before channel RPC returns", options: [] }] },
          });
        await channelRPCGate.promise;
        return {
          messages: [{ role: "user", content: "channel" }, { role: "assistant", content: "waiting" }],
          startTurn: 0,
          endTurn: 1,
          totalTurns: 1,
          hasOlder: false,
        };
      },
    } as Partial<AppBindings> as AppBindings;
const desktopStub = installDesktopHostStub(appStubTable);

type Controller = ReturnType<typeof useController>;
let controller: Controller | undefined;

function Probe() {
  controller = useController();
  return null;
}

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(<Probe />);
  await flushPromises();
});
await waitFor("active tab", () => controller?.activeTabId === "tab-a");

await act(async () => {
  await controller?.refreshMeta();
  await flushPromises();
});
eq(controller?.state.meta?.canonicalTodos?.[0]?.content, "Old task", "pre-reset metadata exposes the current session todo");

holdNextMeta = true;
await act(async () => {
  desktopStub.emit("agent:event", { kind: "turn_done", tabId: "tab-a" });
  await flushPromises();
});
await waitFor("stale metadata request", () => staleMetaStarted);

await act(async () => {
  await controller?.newSession();
  await flushPromises();
});
eq(newSessionCalls, 1, "tab-scoped NewSession is called once");
eq(controller?.state.items.length, 0, "new session clears the visible transcript");
eq(controller?.state.meta?.canonicalTodos?.length, 0, "new session refresh replaces the previous session todo with an authoritative empty list");

await act(async () => {
  staleSessionMeta.resolve(meta({ canonicalTodos: [{ content: "Old task", status: "in_progress" }] }));
  await staleSessionMeta.promise;
  await flushPromises();
});
eq(controller?.state.meta?.canonicalTodos?.length, 0, "metadata started before a session transition cannot restore the previous todo");

await act(async () => {
  staleHistory.resolve([{ role: "user", content: "old prompt" }]);
  await staleHistory.promise;
  await flushPromises();
});

eq(controller?.state.items.length, 0, "stale history load cannot repopulate a new blank session");

let resumeNavigation: NavigationResult<void> | undefined;
let resumeSurfaceSettled = false;
await act(async () => {
  resumeNavigation = controller?.resumeSession("/sessions/restored.jsonl", "tab-a");
  void resumeNavigation?.surfaceReady.then(() => { resumeSurfaceSettled = true; });
  await flushPromises();
});
eq(resumeSurfaceSettled, false, "Resume releases navigation acquisition before target history settles");
eq(controller?.state.ask?.id, "pre-response-resume", "Resume accepts the new-epoch ask and rejects the interleaved old-epoch ask before returning");
await act(async () => {
  resumeRPCGate.resolve();
  await resumeNavigation?.surfaceReady;
  await flushPromises();
});
eq(resumeSurfaceSettled, true, "Resume surfaceReady resolves after authoritative history and reconciliation");
eq(controller?.state.meta?.canonicalTodos?.[0]?.status, "completed", "resuming a session refreshes its authoritative canonical todo state");
eq(controller?.state.ask?.id, "replayed-runtime-resumed", "Resume RPC ask emitted before return is restored after reset/history hydration");
ok(promptReplayCalls > 0, "Resume completion performs a tab-scoped pending-prompt replay");

backendPendingPrompt = false;
await act(async () => {
  desktopStub.emit("agent:event", { kind: "turn_done", tabId: "tab-a", runtimeEpoch: backendRuntimeEpoch });
  await flushPromises();
});
const replayCallsBeforeChannelOpen = promptReplayCalls;
let channelNavigation: NavigationResult<void> | undefined;
await act(async () => {
  channelNavigation = controller?.openChannelSession("/sessions/channel.jsonl", "tab-a");
  await flushPromises();
});
eq(controller?.state.ask?.id, "pre-response-channel", "channel-open accepts the new-epoch ask and rejects the interleaved old-epoch ask before returning");
await act(async () => {
  channelRPCGate.resolve();
  await channelNavigation?.surfaceReady;
  await flushPromises();
});
eq(controller?.state.ask?.id, "replayed-runtime-channel", "channel-open ask emitted before return is restored after reset/history hydration");
ok(promptReplayCalls > replayCallsBeforeChannelOpen, "channel-open completion performs a tab-scoped pending-prompt replay");

await act(async () => {
  root.unmount();
});

// Reusing a blank tab must invalidate the old hydration request. The backend
// may return the same tab id, so the request sequence (not the tab id) is the
// session boundary that prevents orphaned tool cards from coming back.
const reusedOldHistory = deferred<{
  messages: HistoryMessage[];
  startTurn: number;
  endTurn: number;
  totalTurns: number;
  hasOlder: boolean;
}>();
const reusedHistoryCalls: string[] = [];
const reusedTab = tabMeta({ id: "tab-reused", sessionPath: "/sessions/old.jsonl" });
const reusedTabPage = {
  messages: [
    { role: "assistant", content: "", toolCalls: [{ id: "old-call", name: "bash", arguments: "pwd" }] },
    { role: "tool", toolCallId: "old-call", toolName: "bash", content: "/old" },
  ] as HistoryMessage[],
  startTurn: 0,
  endTurn: 0,
  totalTurns: 0,
  hasOlder: false,
};
const reusedEmptyPage = { messages: [], startTurn: 0, endTurn: 0, totalTurns: 0, hasOlder: false };
desktopStub.replaceCommands({
  RegisterNavigationIntent: async () => {},
  ListTabs: async () => [reusedTab],
  MetaForTab: async () => meta({ sessionPath: "/sessions/new.jsonl" }),
  ContextUsageForTab: async () => context,
  EffortForTab: async () => effort,
  BalanceForTab: async () => balance,
  JobsForTab: async () => jobs,
  CheckpointsForTab: async () => checkpoints,
  HistoryPageForTab: async () => {
    reusedHistoryCalls.push("history");
    return reusedHistoryCalls.length === 1 ? reusedOldHistory.promise : reusedEmptyPage;
  },
  HistorySliceForTab: async (tabID: string, req: HistorySliceRequest) => {
    reusedHistoryCalls.push("history");
    const page = reusedHistoryCalls.length === 1 ? await reusedOldHistory.promise : reusedEmptyPage;
    return historySliceFromMessages(tabID, page.messages, req);
  },
  HistoryCheckpointTurnsForTab: async () => [],
  ReplayPendingPrompts: async () => {},
  EnsureBlankTab: async () => ({ ...reusedTab, sessionPath: "/sessions/new.jsonl", active: true }),
} as Partial<AppBindings> as AppBindings);

controller = undefined;
const reuseRoot = createRoot(rootEl);
await act(async () => {
  reuseRoot.render(<Probe />);
  await flushPromises();
});
await waitFor("reused tab startup history", () => reusedHistoryCalls.length === 1);

await act(async () => {
  await controller?.ensureBlankTab("project", "/repo");
  await flushPromises();
});
eq(reusedHistoryCalls.length, 2, "reusing a blank tab forces a fresh history request");
eq(controller?.state.items.some((item) => item.kind === "tool" && item.id === "old-call"), false, "fresh blank-tab hydration has no old tool card");

await act(async () => {
  reusedOldHistory.resolve(reusedTabPage);
  await reusedOldHistory.promise;
  await flushPromises();
});
eq(controller?.state.items.some((item) => item.kind === "tool" && item.id === "old-call"), false, "late old-session history cannot restore an orphaned tool card");

await act(async () => {
  reuseRoot.unmount();
});

// A tab-bar click can overtake EnsureBlankTab while its backend call is still
// in flight. Its intent must invalidate the older completion immediately, and
// the stale backend activation must be repaired after it eventually returns.
const queuedBlank = deferred<TabMeta>();
const raceTabA = tabMeta({ id: "race-a", active: true, sessionPath: "/sessions/race-a.jsonl" });
const raceTabB = tabMeta({ id: "race-b", active: false, sessionPath: "/sessions/race-b.jsonl" });
const raceBlank = tabMeta({ id: "race-blank", active: false, sessionPath: "/sessions/race-blank.jsonl" });
let raceBackendActiveId = raceTabA.id;
const raceHistoryCalls: string[] = [];
const raceSetActiveCalls: string[] = [];
desktopStub.replaceCommands({
  RegisterNavigationIntent: async () => {},
  ListTabs: async () => [raceTabA, raceTabB, raceBlank].map((tab) => ({ ...tab, active: tab.id === raceBackendActiveId })),
  MetaForTab: async (tabID: string) => meta({ sessionPath: `/sessions/${tabID}.jsonl` }),
  ContextUsageForTab: async () => context,
  EffortForTab: async () => effort,
  BalanceForTab: async () => balance,
  JobsForTab: async () => jobs,
  CheckpointsForTab: async () => checkpoints,
  HistoryPageForTab: async (tabID: string) => {
    raceHistoryCalls.push(tabID);
    return reusedEmptyPage;
  },
  HistorySliceForTab: async (tabID: string, req: HistorySliceRequest) => {
    raceHistoryCalls.push(tabID);
    return historySliceFromMessages(tabID, reusedEmptyPage.messages, req);
  },
  HistoryCheckpointTurnsForTab: async () => [],
  ReplayPendingPrompts: async () => {},
  EnsureBlankTab: async () => {
    const tab = await queuedBlank.promise;
    raceBackendActiveId = tab.id;
    return tab;
  },
  SetActiveTab: async (tabID: string) => {
    raceSetActiveCalls.push(tabID);
    raceBackendActiveId = tabID;
  },
} as Partial<AppBindings> as AppBindings);

controller = undefined;
const queuedRaceRoot = createRoot(rootEl);
await act(async () => {
  queuedRaceRoot.render(<Probe />);
  await flushPromises();
});
await waitFor("queued blank race startup", () => controller?.activeTabId === raceTabA.id);

let pendingBlank: Promise<TabMeta> | undefined;
await act(async () => {
  pendingBlank = controller?.ensureBlankTab("project", "/repo");
  await flushPromises();
});
const tabClickIntent = controller?.noteNavigationIntent();
if (tabClickIntent === undefined) throw new Error("missing queued tab intent");

let pendingTabSwitch: Promise<TabMeta[] | undefined> | undefined;
await act(async () => {
  pendingTabSwitch = controller?.switchTab(raceTabB.id, raceTabB, tabClickIntent);
  await pendingTabSwitch;
  await flushPromises();
});
eq(controller?.activeTabId, raceTabB.id, "queued tab click becomes visible before the older blank completion");
eq(raceBackendActiveId, raceTabB.id, "queued tab click becomes backend-active before the older blank completion");

await act(async () => {
  queuedBlank.resolve({ ...raceBlank, active: true });
  await pendingBlank;
  await flushPromises();
});
eq(controller?.activeTabId, raceTabB.id, "late blank completion cannot replace the newer visible tab");
eq(raceHistoryCalls.includes(raceBlank.id), false, "stale blank completion does not hydrate the abandoned tab");
eq(raceBackendActiveId, raceTabB.id, "late blank completion reasserts the newer backend-active tab");
eq(raceSetActiveCalls.join(","), `${raceTabB.id},${raceTabB.id}`, "stale blank completion repairs backend focus exactly once");

await act(async () => {
  queuedRaceRoot.unmount();
});

const guardedStartupTabs = deferred<TabMeta[]>();
const staleProjectA = "/repo/project-a";
const targetProjectB = "/repo/project-b";
const ensureBlankSurfaceCalls: Array<{ scope: string; workspaceRoot: string }> = [];
desktopStub.replaceCommands({
  RegisterNavigationIntent: async () => {},
  ListTabs: async () => guardedStartupTabs.promise,
  MetaForTab: async (tabID: string) => tabID === "tab-new"
    ? meta({ cwd: targetProjectB, workspaceRoot: targetProjectB, workspaceName: "project-b", workspacePath: targetProjectB })
    : meta({ cwd: staleProjectA, workspaceRoot: staleProjectA, workspaceName: "project-a", workspacePath: staleProjectA }),
  ContextUsageForTab: async () => context,
  EffortForTab: async () => effort,
  BalanceForTab: async () => balance,
  JobsForTab: async () => jobs,
  CheckpointsForTab: async () => checkpoints,
  HistoryForTab: async () => [],
  HistoryPageForTab: async () => ({ messages: [], startTurn: 0, endTurn: 0, totalTurns: 0, hasOlder: false }),
  HistoryCheckpointTurnsForTab: async () => [],
  ReplayPendingPrompts: async () => {},
  EnsureBlankSurface: async (scope: string, workspaceRoot: string) => {
    ensureBlankSurfaceCalls.push({ scope, workspaceRoot });
    return tabMeta({
      id: "tab-new",
      topicId: "topic-new",
      topicTitle: "New session",
      workspaceRoot: targetProjectB,
      workspaceName: "project-b",
      workspacePath: targetProjectB,
      cwd: targetProjectB,
    });
  },
} as Partial<AppBindings> as AppBindings);

controller = undefined;
const guardRoot = createRoot(rootEl);

await act(async () => {
  guardRoot.render(<Probe />);
  await flushPromises();
});

await act(async () => {
  await controller?.ensureBlankSurface("project", targetProjectB);
  await flushPromises();
});

eq(ensureBlankSurfaceCalls.length, 1, "EnsureBlankSurface is called once");
eq(ensureBlankSurfaceCalls[0]?.workspaceRoot, targetProjectB, "EnsureBlankSurface keeps the requested project root");
eq(controller?.activeTabId, "tab-new", "blank surface becomes active before startup sync resolves");
eq(controller?.state.meta?.workspaceRoot, targetProjectB, "blank surface exposes the new project root");

await act(async () => {
  guardedStartupTabs.resolve([tabMeta({
    id: "tab-old",
    topicId: "topic-old",
    topicTitle: "Old session",
    workspaceRoot: staleProjectA,
    workspaceName: "project-a",
    workspacePath: staleProjectA,
    cwd: staleProjectA,
  })]);
  await guardedStartupTabs.promise;
  await flushPromises();
});

eq(controller?.activeTabId, "tab-new", "guarded startup sync cannot restore an older active tab");
eq(controller?.state.meta?.workspaceRoot, targetProjectB, "guarded startup sync cannot restore the old project root");

await act(async () => {
  guardRoot.unmount();
});

// Exercise the production hook's modern entry points: none may install an
// independently fetched legacy prefix or leave the new runtime paused forever.
let modernEpoch = "modern-start";
let modernPath = "/sessions/modern-start.jsonl";
let modernSnapshots = 0;
let legacyReads = 0;
let modernAdoptions = 0;
const slowModernGate = deferred<void>();
const modernPhases = { resolveMs: 1, loadMs: 2, rebindMs: 3, historyMs: 0, totalMs: 6, loadedMessages: 2, loadedBytes: 100, historyEntries: 0, durableReads: 1, outcome: "ok" };
const modernReplace = (name: string) => {
  modernEpoch = name;
  modernPath = `/sessions/${name}.jsonl`;
  desktopStub.emit("runtime:rebuilt", "tab-a", modernEpoch);
};
const legacyRead = async () => { legacyReads++; throw new Error("modern hydration read legacy history"); };
desktopStub.replaceCommands({
  RegisterNavigationIntent: async () => {},
  ListTabs: async () => [tabMeta({ runtime: { phase: "ready", epoch: modernEpoch } })],
  MetaForTab: async () => meta({ sessionPath: modernPath, runtime: { phase: "ready", epoch: modernEpoch } }),
  ContextUsageForTab: async () => context,
  EffortForTab: async () => effort,
  BalanceForTab: async () => balance,
  JobsForTab: async () => jobs,
  CheckpointsForTab: async () => checkpoints,
  HistoryCheckpointTurnsForTab: async () => [],
  HistoryForTab: legacyRead, HistoryPageForTab: legacyRead, HistorySliceForTab: legacyRead,
  ResumeSessionPageForTab: legacyRead, OpenChannelSessionPageForTab: legacyRead,
  ReplayPendingPrompts: async () => {},
  ReplayPendingPromptsForTab: async () => {},
  TranscriptSnapshotForTab: async () => {
    modernSnapshots++;
    return { protocolVersion: 1, snapshotId: modernEpoch, identity: { sessionId: modernEpoch, headId: modernEpoch, rewriteEpoch: 0, runtimeEpoch: modernEpoch },
      projectionRevision: 0, coveredThroughSeq: 0, records: [], activeRecords: [], runtime: { status: "completed", pendingEvents: [] },
      activeAttempts: [], before: 0, hasOlder: false, totalRecords: 0, totalTurns: 0, stale: false };
  },
  NewSessionForTab: async () => modernReplace("modern-new"),
  ClearSessionForTab: async () => { modernReplace("modern-clear"); return { sessionPath: modernPath, sessionGeneration: 2 }; },
  ResumeTranscriptSessionForTab: async (_tab: string, path: string) => {
    modernAdoptions++; modernReplace(path.includes("slow") ? "modern-slow" : "modern-resume");
    if (path.includes("slow")) await slowModernGate.promise;
    return { ...modernPhases, totalMs: path.includes("slow") ? 500 : modernPhases.totalMs };
  },
  OpenChannelTranscriptSessionForTab: async () => { modernAdoptions++; modernReplace("modern-channel"); return modernPhases; },
} as Partial<AppBindings>);
controller = undefined;
const modernRoot = createRoot(rootEl);
await act(async () => { modernRoot.render(<Probe />); await flushPromises(); });
await waitFor("modern startup snapshot", () => controller?.state.transcriptProtocol === 1);
const verifyModernSuffix = async (label: string) => {
  await act(async () => {
    desktopStub.emit("agent:event", { kind: "user_message", tabId: "tab-a", runtimeEpoch: modernEpoch,
      sessionId: modernEpoch, seq: 1, messageId: `${modernEpoch}-user`, text: label });
    desktopStub.emit("agent:event", { kind: "text", tabId: "tab-a", runtimeEpoch: modernEpoch,
      sessionId: modernEpoch, seq: 2, messageId: `${modernEpoch}-assistant`, text: "suffix" });
    await flushPromises();
  });
  eq(controller?.state.items.filter((item) => item.kind === "user").length, 1, `${label} accepts one user after its snapshot`);
  eq(controller?.state.live?.text, "suffix", `${label} accepts the ordered live suffix`);
};
await verifyModernSuffix("startup");
await act(async () => { await controller?.newSession(); await flushPromises(); });
eq(controller?.state.items.length, 0, "modern new session installs its empty cut");
await verifyModernSuffix("new");
await act(async () => { await controller?.clearSession(); await flushPromises(); });
eq(controller?.state.items.length, 0, "modern clear installs its empty cut");
await verifyModernSuffix("clear");
await act(async () => { await controller?.resumeSession("/sessions/modern-resume.jsonl", "tab-a")?.surfaceReady; await flushPromises(); });
eq(sessionPipelineDiagnostics().resumeHistory?.source, "transcript-snapshot", "modern resume records snapshot installation");
eq(sessionPipelineDiagnostics().duplicateLoadCount, 0, "modern resume receives backend load evidence");
ok(typeof sessionPipelineDiagnostics().resumeSnapshotMs === "number", "modern resume measures snapshot time separately");
await verifyModernSuffix("resume");
await act(async () => { await controller?.openChannelSession("/sessions/modern-channel.jsonl", "tab-a")?.surfaceReady; await flushPromises(); });
eq(sessionPipelineDiagnostics().resumeHistory?.source, "transcript-snapshot", "modern channel records snapshot installation");
await verifyModernSuffix("channel");
eq(modernAdoptions, 2, "resume and channel use adoption without a legacy history payload");
eq(modernSnapshots, 5, "each modern entry point obtains one authoritative cut");
eq(legacyReads, 0, "modern entry points never read legacy history");
let staleModern: NavigationResult<void> | undefined;
await act(async () => {
  staleModern = controller?.resumeSession("/sessions/slow.jsonl", "tab-a");
  await flushPromises();
});
eq(sessionPipelineDiagnostics().duplicateLoadCount, null, "pending switch does not reuse previous evidence");
await act(async () => { await controller?.resumeSession("/sessions/fast.jsonl", "tab-a")?.surfaceReady; await flushPromises(); });
await act(async () => { slowModernGate.resolve(); await staleModern?.surfaceReady; await flushPromises(); });
eq(sessionPipelineDiagnostics().resumeSwitch?.totalMs, modernPhases.totalMs, "stale modern adoption cannot overwrite committed diagnostics");
eq(sessionPipelineDiagnostics().resumeHistory?.source, "transcript-snapshot", "modern race retains authoritative snapshot evidence");
await act(async () => { modernRoot.unmount(); });
// ── session switch: one history commit, composer bound to the new runtime ────
// The switch shows the restored transcript as soon as its page lands, but the
// tab is only submittable once the runtime reconcile confirms which session the
// controller now owns. A superseded switch must not paint over the newer one.
const switchMetaGate = deferred<Meta>();
const slowSwitchGate = deferred<void>();
let switchMetaHeld = false;
let switchMetaPath = "/sessions/one.jsonl";
let switchResumeCalls = 0;
let switchHistoryPageCalls = 0;
const switchTab = tabMeta({ id: "tab-switch", sessionPath: "/sessions/one.jsonl" });
const switchPage = (text: string, durableReads = 1) => ({
  messages: [{ role: "user", content: text } as HistoryMessage],
  startTurn: 0,
  endTurn: 1,
  totalTurns: 1,
  hasOlder: false,
  switch: {
    resolveMs: 0, loadMs: 1, rebindMs: 2, historyMs: 1, totalMs: 4,
    loadedMessages: 1, loadedBytes: 64, historyEntries: 1, durableReads, outcome: "ok",
  },
});
desktopStub.replaceCommands({
  RegisterNavigationIntent: async () => {},
  ListTabs: async () => [switchTab],
  MetaForTab: async () => {
    if (switchMetaHeld) return switchMetaGate.promise;
    return meta({ sessionPath: switchMetaPath });
  },
  ContextUsageForTab: async () => context,
  EffortForTab: async () => effort,
  BalanceForTab: async () => balance,
  JobsForTab: async () => jobs,
  CheckpointsForTab: async () => checkpoints,
  HistoryPageForTab: async () => {
    switchHistoryPageCalls += 1;
    return switchPage("full-history-refetch");
  },
  HistorySliceForTab: async (tabID: string, req: HistorySliceRequest) => historySliceFromMessages(tabID, [], req),
  HistoryCheckpointTurnsForTab: async () => [],
  ReplayPendingPrompts: async () => {},
  ReplayPendingPromptsForTab: async () => {},
  ResumeSessionPageForTab: async (_tabID: string, path: string) => {
    switchResumeCalls += 1;
    if (path.includes("slow")) await slowSwitchGate.promise;
    const page = switchPage(path);
    page.switch.totalMs = path.includes("slow") ? 500 : 4;
    return page;
  },
});

resetSessionDiagnostics();
const switchRoot = createRoot(document.createElement("div"));
await act(async () => {
  switchRoot.render(<Probe />);
  await flushPromises();
});
await waitFor("switch tab active", () => controller?.activeTabId === "tab-switch");

switchMetaHeld = true;
let switchNav: NavigationResult<void> | undefined;
await act(async () => {
  switchNav = controller?.resumeSession("/sessions/two.jsonl", "tab-switch");
  await flushPromises();
});
await waitFor("switched transcript", () => (controller?.state.items.length ?? 0) > 0);
eq(controller?.state.items[0]?.text, "/sessions/two.jsonl", "switch commits the restored transcript before ancillary work finishes");
eq(runtimeReadyForSubmit(controller?.state.meta), false, "composer stays disabled until the switched runtime is reconciled");

await act(async () => {
  switchMetaHeld = false;
  switchMetaPath = "/sessions/two.jsonl";
  switchMetaGate.resolve(meta({ sessionPath: switchMetaPath }));
  await switchNav?.surfaceReady;
  await flushPromises();
});
eq(runtimeReadyForSubmit(controller?.state.meta), true, "composer re-enables once the switched runtime is reconciled");
eq(switchResumeCalls, 1, "a switch issues exactly one resume page request");
eq(switchHistoryPageCalls, 0, "a switch does not refetch the full history page from the frontend");
eq(sessionPipelineDiagnostics().duplicateLoadCount, 0, "switch reports no duplicate durable load");
eq(sessionPipelineDiagnostics().resumeHistory?.source, "resume-loaded", "the switch's first screen is attributed to the resume page");

let slowNav: NavigationResult<void> | undefined;
let fastNav: NavigationResult<void> | undefined;
await act(async () => {
  slowNav = controller?.resumeSession("/sessions/slow.jsonl", "tab-switch");
  await flushPromises();
});
await act(async () => {
  fastNav = controller?.resumeSession("/sessions/fast.jsonl", "tab-switch");
  await fastNav?.surfaceReady;
  await flushPromises();
});
await act(async () => {
  slowSwitchGate.resolve();
  await slowNav?.surfaceReady;
  await flushPromises();
});
eq(controller?.state.items[0]?.text, "/sessions/fast.jsonl", "a superseded switch cannot paint over the newer transcript");
eq(sessionPipelineDiagnostics().resumeSwitch?.totalMs, 4, "superseded response cannot overwrite current switch diagnostics");

await act(async () => {
  switchRoot.unmount();
});
// Navigation admission: an old MetaForTab completion races the new ready=false.
{
  const oldMeta = deferred<Meta>();
  const navigationGate = deferred<void>();
  let holdMeta = false;
  let holdNavigation = false;
  let path = "/sessions/source.jsonl";
  const submissions: string[] = [];
  desktopStub.replaceCommands({
    RegisterNavigationIntent: async () => { if (holdNavigation) await navigationGate.promise; },
    ListTabs: async () => [tabMeta({ id: "meta-race", sessionPath: path })],
    MetaForTab: async () => holdMeta ? oldMeta.promise : meta({ sessionPath: path }),
    ContextUsageForTab: async () => context, EffortForTab: async () => effort,
    BalanceForTab: async () => balance, JobsForTab: async () => jobs,
    CheckpointsForTab: async () => checkpoints, HistoryCheckpointTurnsForTab: async () => [],
    HistorySliceForTab: async (id: string, req: HistorySliceRequest) => historySliceFromMessages(id, [], req),
    ReplayPendingPrompts: async () => {}, ReplayPendingPromptsForTab: async () => {},
    ResumeSessionPageForTab: async (_id: string, target: string) => { path = target; return switchPage(target); },
    StartTurnForTab: async () => { submissions.push(path); return { turnId: "wrong-source" }; },
  });
  const raceRoot = createRoot(document.createElement("div"));
  await act(async () => { raceRoot.render(<Probe />); await flushPromises(); });
  await waitFor("meta race active", () => controller?.activeTabId === "meta-race" && runtimeReadyForSubmit(controller?.state.meta));
  holdMeta = true;
  let oldRefresh: Promise<void> | undefined;
  await act(async () => { oldRefresh = controller?.refreshMeta(); await flushPromises(); });
  holdNavigation = true;
  let navigation: NavigationResult<void> | undefined;
  await act(async () => { navigation = controller?.resumeSession("/sessions/target.jsonl", "meta-race"); await flushPromises(); });
  eq(runtimeReadyForSubmit(controller?.state.meta), false, "switch initially closes admission");
  await act(async () => {
    holdMeta = false;
    oldMeta.resolve(meta({ sessionPath: "/sessions/source.jsonl" }));
    await oldRefresh;
    await flushPromises();
  });
  eq(runtimeReadyForSubmit(controller?.state.meta), false, "old metadata must not reopen admission during navigation registration");
  await act(async () => { await controller?.sendToTab("meta-race", "raced submission").catch(() => {}); await flushPromises(); });
  eq(submissions.length, 0, "pending switch must not send into its source controller");
  await act(async () => { holdNavigation = false; navigationGate.resolve(); await navigation?.surfaceReady; await flushPromises(); raceRoot.unmount(); });
}
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
