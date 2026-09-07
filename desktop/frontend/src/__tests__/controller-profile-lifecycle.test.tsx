import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useControllerProfileCommands } from "../lib/useControllerProfileCommands";
import { useSessionOperations } from "../app-runtime/useSessionOperations";
import type { ControllerProfileResource } from "../app-runtime/controllerProfileOwner";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
function deferred() {
  let resolve!: (value: boolean) => void; let reject!: (error: Error) => void;
  const promise = new Promise<boolean>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const calls: string[] = [], errors: unknown[] = [];
let pending = deferred();
let profileFailure: Error | undefined;
let profileGate: ReturnType<typeof deferred> | undefined;
const ports = {
  model: async (tab: string, name: string) => { calls.push(`model:${tab}:${name}`); return pending.promise; },
  profile: async (tab: string, collaboration: string, approval: string, goal: string) => {
    calls.push(`profile:${tab}:${collaboration}:${approval}:${goal}`);
    if (profileGate) return profileGate.promise;
    if (profileFailure) throw profileFailure;
    return true;
  },
};
let commands!: ReturnType<typeof useControllerProfileCommands>;
function Probe({ tab, generation, plan, ready, epoch, remote }: { tab: string; generation: number; plan: boolean; ready: boolean; epoch: string; remote: boolean }) {
  const profiles: ControllerProfileResource[] = ["A", "B"].map(tabId => ({
    target: { tabId, sessionKey: tabId + generation }, remote: remote && tabId === "A",
    profile: { collaboration: tabId === "A" && plan ? "plan" : "normal", approval: "ask", goal: tabId === "B" ? "B goal" : "" },
  }));
  const target = profiles.find(value => value.target.tabId === tab)!.target;
  const operations = useSessionOperations({ visible: target, resources: profiles.map(value => value.target) });
  commands = useControllerProfileCommands({ target, profiles, ready, runtimeEpoch: epoch, remote: remote && tab === "A", operations, ports,
    remoteModel: async name => { calls.push(`remote:${tab}:${name}`); await pending.promise; }, report: error => errors.push(error) });
  return null;
}
const paint = (tab = "A", plan = true, generation = 1, ready = false, epoch = "runtime-1", remote = false) => act(async () => root.render(
  <Probe tab={tab} generation={generation} plan={plan} ready={ready} epoch={epoch} remote={remote} />));
try {
  await paint();
  const change = commands.switchModel("first");
  await paint("B", false);
  pending.resolve(true); assert.equal(await change, false, "a completed source write does not regain UI ownership on B");
  assert.deepEqual(calls, ["model:A:first", "profile:A:normal:ask:"], "post-rebuild profile is the latest committed source value, not the old render or B");

  calls.length = 0; pending = deferred(); await paint();
  const stale = commands.switchModel("replaced");
  await paint("A", true, 2); pending.resolve(true);
  assert.equal(await stale, false);
  assert.deepEqual(calls, ["model:A:replaced"], "replacement session rejects old post-model profile writes");

  calls.length = 0; pending = deferred(); await paint();
  const first = commands.switchModel("old");
  const old = pending; pending = deferred();
  const second = commands.switchModel("new");
  old.resolve(true); assert.equal(await first, false);
  pending.resolve(true); assert.equal(await second, true);
  assert.deepEqual(calls, ["model:A:old", "model:A:new", "profile:A:plan:ask:"], "superseded model cannot restore its profile or clear the new request");

  calls.length = 0; errors.length = 0; pending = deferred();
  const failure = commands.switchModelFromUi("failure");
  await paint("B"); await paint("A"); pending.reject(Error("old source failure"));
  assert.equal(await failure, false); assert.deepEqual(errors, [], "A-B-A cannot revive old error UI");

  pending = deferred();
  const currentFailure = commands.switchModelFromUi("current-failure");
  const error = Error("model failed"); pending.reject(error);
  assert.equal(await currentFailure, false); assert.deepEqual(errors, [error], "UI failure is presented exactly once");
  errors.length = 0; pending = deferred();
  const directFailure = commands.switchModel("slash-model"); pending.reject(error);
  await assert.rejects(directFailure, error);
  assert.deepEqual(errors, [], "awaiting callers retain the reject contract without duplicate UI handling");

  profileFailure = Error("restore failed");
  assert.equal(await commands.applyProfile("A", false), false, "send readiness retains false-on-failure semantics");
  await paint("A", true, 1, true);
  assert.deepEqual(errors, [profileFailure], "background restoration reports its real error once");
  profileFailure = undefined; errors.length = 0; await paint();

  calls.length = 0; await paint("A", true, 1, true);
  assert.deepEqual(calls, ["profile:A:plan:ask:"], "ready restoration shares source-profile execution");
  await paint("A", false, 1, true);
  assert.equal(calls.at(-1), "profile:A:normal:ask:");
  calls.length = 0;
  await paint("A", false, 1, true, "runtime-2");
  assert.deepEqual(calls, ["profile:A:normal:ask:"], "same profile on a replacement runtime is restored without relying on object churn");

  for (const modelFirst of [true, false]) {
    await paint(); calls.length = 0; errors.length = 0;
    profileGate = deferred(); pending = deferred();
    if (!modelFirst) await paint("A", true, 1, true);
    const overlapping = commands.switchModelFromUi("overlap");
    await act(async () => pending.resolve(true));
    if (modelFirst) await paint("A", true, 1, true);
    const sharedError = Error("one Controller application failed");
    await act(async () => profileGate!.reject(sharedError));
    assert.equal(await overlapping, false);
    assert.deepEqual(errors, [sharedError], "model and readiness observers share one failure owner in either completion order");
    profileGate = undefined;
  }

  calls.length = 0; pending = deferred(); await paint("A", true, 1, true, "runtime-1", true);
  const remote = commands.switchModel("remote-model");
  await paint("B", true, 1, false, "runtime-1", true);
  pending.resolve(true); assert.equal(await remote, false);
  assert.deepEqual(calls, ["remote:A:remote-model"], "remote model stays source-bound and never uses local profile restoration");

  await paint(); calls.length = 0; pending = deferred();
  const disposed = commands.switchModel("disposed");
  await act(async () => root.unmount()); pending.resolve(true); await disposed;
  commands.switchModel("after-unmount");
  assert.deepEqual(calls, ["model:A:disposed"], "unmount revokes continuation and stable entry immediately");
  console.log("controller profile lifecycle: committed source, replacement, ordering, ABA, ready restore and disposal passed");
} finally { dom.window.close(); }
