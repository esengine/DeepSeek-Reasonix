import assert from "node:assert/strict";
import { setImmediate } from "node:timers/promises";
import { JSDOM } from "jsdom";
import type { AppBindings } from "../lib/bridge";
import type { SessionRecoveryEvent } from "../lib/types";
import { installDesktopHostStub } from "./desktopHostStub";

const dom = new JSDOM("", { url: "http://localhost/" });
globalThis.window = dom.window as unknown as Window & typeof globalThis;
let reconcileCalls = 0;
let reads = 0;
let rejectReconcile: (error: Error) => void = () => {};
const desktopStub = installDesktopHostStub(({ main: { App: {
  ReconcileRecoveryVersions: () => {
    reconcileCalls += 1;
    return new Promise((_resolve, reject) => { rejectReconcile = reject; });
  },
  GetRecoveryLineage: async () => {
    reads += 1;
    return { groupId: "root", state: "diverged", branchCount: 2, unresolved: 1, cleanupEligible: 0, members: [] };
  },
} as unknown as AppBindings } }).main.App);
const { startSessionRecoveryRuntime } = await import("../lib/sessionRecoveryRuntime");
const { setFrontendDiagnosticSink } = await import("../lib/frontendDiagnosticBridge");
const diagnostics: unknown[] = [];
setFrontendDiagnosticSink((_source, type, fields) => diagnostics.push({ type, fields }));
const unhandled: unknown[] = [];
const onUnhandled = (error: unknown) => unhandled.push(error);
process.on("unhandledRejection", onUnhandled);
let recovered = 0;
const stop = startSessionRecoveryRuntime({ onRecovered: () => { recovered += 1; }, onDiverged: () => {} });
const event: SessionRecoveryEvent = {
  recoveryPath: "/sessions/fork.jsonl", scope: "global", topicId: "topic",
  recoveryParentId: "root", recoveryReason: "snapshot_conflict",
};
desktopStub.emit("session:recovered", event);
desktopStub.emit("session:recovered", event);
assert.equal(reconcileCalls, 1, "duplicate recovery is deduplicated");
rejectReconcile(new Error("session version lineage is unavailable: /private/session/path"));
await setImmediate(); // Let Node report any unhandled promise rejection.
assert.deepEqual(unhandled, [], "a failed background reconcile must not escape to the crash handler");
assert.equal(JSON.stringify(diagnostics).includes("/private/session/path"), false, "diagnostics omit raw backend errors");
desktopStub.emit("project-tree:changed-v2", { revision: 2, roots: [""], reason: "reconcile" });
await setImmediate();
assert.equal(reads, 1, "a later catalog revision still classifies the pending recovery");
desktopStub.emit("session:recovered", { ...event, recoveryPath: "/sessions/next-fork.jsonl" });
assert.equal(reconcileCalls, 2, "failure releases the topic's in-flight guard");
assert.equal(recovered, 2, "new recovery events remain usable after failure");
stop();
rejectReconcile(new Error("stopped coordinator failure"));
await setImmediate();
assert.deepEqual(unhandled, [], "rejections after disposal are also contained");
assert.equal([...desktopStub.events.values()].reduce((n, set) => n + set.size, 0), 0, "all host event subscriptions are disposed");
process.off("unhandledRejection", onUnhandled);
dom.window.close();
console.log("  PASS  failed recovery reconciliation stays local and preserves catalog-driven classification");
