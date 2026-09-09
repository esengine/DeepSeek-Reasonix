import assert from "node:assert/strict";
import { createControllerModelCommands } from "../lib/controllerModelCommands";

const calls: Array<{ tab: string; value: string; resolve: () => void; reject: (error: Error) => void }> = [];
const refreshes: string[] = [];
const notices: string[] = [];
(globalThis as unknown as { window: unknown }).window = { go: { main: { App: {
  SetEffortForTab: (tab: string, value: string) => new Promise<void>((resolve, reject) => calls.push({ tab, value, resolve, reject })),
} } } };
const ref = <T>(current: T) => ({ current });
const ports: Parameters<typeof createControllerModelCommands>[0] = {
  statesRef: ref(new Map([["a", { sessionGen: 1, meta: { sessionPath: "session-a", sessionGeneration: 1 } }],
    ["b", { sessionGen: 1, meta: { sessionPath: "session-b", sessionGeneration: 1 } }]])),
  modelSwitchSeqByTab: ref(new Map()),
  modelSwitchSuccessVersionByTab: ref(new Map()),
  modelSwitchQueueByTab: ref(new Map()),
  effortSwitchSeqByTab: ref(new Map()),
  effortSwitchQueueByTab: ref(new Map()),
  enqueueModelSwitch: async () => "applied",
  clearBalanceForTab: () => {},
  dispatchTo: (_tab, action) => { if (action.type === "local_notice") notices.push(action.text); },
  refreshBalanceForTab: async () => {},
  refreshMetaForTab: async tab => { refreshes.push(tab); },
};
const flush = async () => { for (let i = 0; i < 10; i++) await Promise.resolve(); };
const commands = createControllerModelCommands(ports);

const first = commands.setEffortForTab("a", "high");
await flush();
assert.deepEqual(calls.map(call => call.value), ["high"]);
const skipped = commands.setEffortForTab("a", "max");
// Recreating the command facade cannot create a second concurrent write owner.
const latest = createControllerModelCommands(ports).setEffortForTab("a", "auto");
const sibling = commands.setEffortForTab("b", "max");
await flush();
assert.deepEqual(calls.map(call => call.tab), ["a", "b"], "other tabs do not wait for this session");
calls[1].resolve();
await sibling;
calls[0].resolve();
await flush();
assert.deepEqual(calls.map(call => call.value), ["high", "max", "auto"], "intermediate selection was superseded before dispatch");
calls[2].resolve();
await Promise.all([first, skipped, latest]);
assert.deepEqual(refreshes, ["b", "a"], "only authoritative latest selections refresh the UI");

// A pending selection from another session is never sent after navigation.
const running = commands.setEffortForTab("a", "high");
await flush();
const obsolete = commands.setEffortForTab("a", "max");
ports.statesRef.current = new Map([["a", { sessionGen: 2, meta: { sessionPath: "different-session", sessionGeneration: 2 } }]]);
calls[3].resolve();
await Promise.all([running, obsolete]);
assert.equal(calls.length, 4);
assert.deepEqual(refreshes, ["b", "a"]);

// The backend may save a pending value before an idle rebuild fails; refresh
// after failure too, so the pending value remains visible and can be cancelled.
const failure = commands.setEffortForTab("a", "max");
await flush();
calls[4].reject(new Error("injected build failure"));
await failure;
assert.equal(notices.length, 1);
assert.deepEqual(refreshes, ["b", "a", "a"]);
assert.equal(ports.effortSwitchQueueByTab.current.size, 0);
const beforeModel = commands.setEffortForTab("a", "high");
await flush();
const oldModelSelection = commands.setEffortForTab("a", "max");
await commands.setModelForTab("a", "different-model");
calls[5].resolve();
await Promise.all([beforeModel, oldModelSelection]);
assert.equal(calls.length, 6, "a queued choice cannot cross a model switch");
console.log("PASS effort ordering: latest selection, independent tabs, session/model fences, failure reconciliation");
