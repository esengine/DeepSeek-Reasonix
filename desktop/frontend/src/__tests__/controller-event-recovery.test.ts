import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import type { Meta, TabMeta } from "../lib/types";
import type { ControllerRecoveryPorts } from "../lib/controllerEventRecovery";

const dom = new JSDOM("<!doctype html><html><body></body></html>");
globalThis.window = dom.window as unknown as Window & typeof globalThis;
const { installDesktopHostStub } = await import("./desktopHostStub");
const { startControllerEventRecovery } = await import("../lib/controllerEventRecovery");
const tab = { id: "task-a", sessionPath: "new", sessionGeneration: 2 } as TabMeta;
let reads = 0;
const stub = installDesktopHostStub({ ListTabs: async () => { reads++; return [tab]; } });
let meta = { sessionPath: "old", sessionGeneration: 1 } as Meta;
const hydrations: Array<{ done(): void; current(): boolean }> = [];
let runtimeUpdates = 0;
let sessionLoadSeq = 0;
const ports: ControllerRecoveryPorts = {
  navigation: () => 1,
  bindings: () => new Map([[tab.id, JSON.stringify([meta.sessionPath, meta.sessionGeneration, sessionLoadSeq])]]),
  meta: () => meta,
  now: () => 0,
  flush: () => {},
  prepare: next => { meta = { ...meta, sessionPath: next.sessionPath, sessionGeneration: next.sessionGeneration }; },
  reset: () => {},
  runtime: () => { runtimeUpdates++; },
  hydrate: (_tab, current) => {
    sessionLoadSeq++;
    return new Promise<void>(done => hydrations.push({ done, current }));
  },
};
const stop = startControllerEventRecovery(ports);
const tick = () => new Promise(resolve => setImmediate(resolve));
const waitFor = async (ready: () => boolean) => {
  for (let attempt = 0; attempt < 100 && !ready(); attempt++) await new Promise(resolve => setTimeout(resolve, 5));
  assert.ok(ready(), "lazy recovery worker loaded");
};
assert.equal(reads, 0, "mounting does not eagerly request recovery data");
stub.emit("desktop:resync", { generation: "g0", reason: "gap" });
stub.emit("desktop:resync", { generation: "g1", reason: "generation" });
assert.equal(reads, 0, "signals during module loading wait for the authoritative read");
await waitFor(() => hydrations.length === 1);
assert.equal(hydrations.length, 1);
assert.equal(reads, 1, "loading-time signals coalesce into one fresh snapshot");
stub.emit("desktop:resync", { generation: "g2", reason: "generation" }); await tick();
assert.equal(hydrations.length, 2, "new generation reissues a superseded session hydrate even after optimistic metadata moved");
assert.equal(hydrations[0].current(), false);
hydrations[0].done(); await tick();
assert.equal(runtimeUpdates, 0, "old-generation hydration cannot apply runtime state");
hydrations[1].done(); await tick();
assert.equal(runtimeUpdates, 1);
for (const [label, replaceBinding] of [
  ["load sequence", () => { sessionLoadSeq++; }],
  ["session path", () => { meta = { ...meta, sessionPath: "background" }; }],
  ["session generation", () => { meta = { ...meta, sessionGeneration: 99 }; }],
] as const) {
  meta = { ...meta, sessionPath: "old", sessionGeneration: 1 };
  stub.emit("desktop:resync", { generation: "g2", reason: "gap" }); await tick();
  const pending: (typeof hydrations)[number] = hydrations[hydrations.length - 1]!;
  replaceBinding();
  assert.equal(pending.current(), true, `${label} changes without navigation or another desktop:resync`);
  pending.done(); await tick();
  assert.equal(runtimeUpdates, 1, `superseded ${label} cannot commit the old runtime after hydration returns`);
}
stop();
const readsBeforeDispose = reads;
const disposeDuringLoad = startControllerEventRecovery(ports);
stub.emit("desktop:resync", { generation: "g3", reason: "generation" });
disposeDuringLoad();
await tick(); await tick();
assert.equal(reads, readsBeforeDispose, "disposing during lazy loading cannot start a recovery read");
stub.uninstall(); dom.window.close();
console.log("controller event recovery: generation and post-hydration tab bindings fence stale runtime commits");
