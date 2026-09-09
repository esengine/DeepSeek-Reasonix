import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useRemoteComposerSend, useRemoteComposerRuntimeActions } from "../lib/useRemoteComposerIntegration";
import { useSessionOperations } from "../app-runtime/useSessionOperations";
import type { RemoteSessionApi } from "../lib/useRemoteSession";
import { useRemoteNavigationCommand } from "../lib/remoteNavigationCommands";
import { RemoteNavigationHarness } from "./helpers/RemoteNavigationHarness";
import { LocaleProvider } from "../lib/i18n";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
function deferred() { let resolve!: () => void; const promise = new Promise<void>(done => { resolve = done; }); return { promise, resolve }; }
let gate = deferred();
const calls: string[] = [];
Object.assign(window, { go: { main: { App: {
  RegisterNavigationIntent: async () => { calls.push("navigation-intent"); },
  OpenRemoteProjectTab: async (host: string, workspace: string, options: { newSession?: boolean }) => { calls.push(`new:${host}:${workspace}:${options.newSession}`); },
} } } });
const session = {
  setModel: async (value: string) => { calls.push(`model:${value}`); },
  setEffort: async (value: string) => { calls.push(`effort:${value}`); },
  compact: async (value: string) => { calls.push(`compact:${value}`); },
  runManagementCommand: async (value: string, hydrate?: boolean) => { calls.push(`manage:${value}:${hydrate}`); },
  pauseGoal: async () => { calls.push("remote-pause"); },
  resumeGoal: async () => { calls.push("remote-resume"); },
  retryHydration: async () => { calls.push("hydrate"); },
} as unknown as RemoteSessionApi;
let send!: ReturnType<typeof useRemoteComposerSend>;
let runtime!: ReturnType<typeof useRemoteComposerRuntimeActions>;
let action: Promise<unknown> | undefined;
function Probe({ tab, generation = "1", remote = true }: { tab: string; generation?: string; remote?: boolean }) {
  const navigateRemote = useRemoteNavigationCommand();
  const target = { tabId: tab, sessionKey: tab + generation };
  const operations = useSessionOperations({ visible: target, resources: ["A", "B"].map(tabId => ({ tabId, sessionKey: tabId + generation })) });
  send = useRemoteComposerSend({ hostId: "fixture", workspace: "fixture" }, tab, "goal", "", session,
    async (display, submit) => { calls.push(`send:${tab}:${display}:${submit}`); },
    async (id, goal) => { calls.push(`goal:${id}:${goal}`); await gate.promise; },
    () => { calls.push("clear"); }, { target, operations, navigateRemote });
  runtime = useRemoteComposerRuntimeActions({ target, operations, remote, session,
    runGoalAction: run => { action = Promise.resolve(run()); },
    pauseLocal: async id => { calls.push(`pause:${id}`); }, resumeLocal: async id => { calls.push(`resume:${id}`); },
    setLocalEffort: async (id, level) => { calls.push(`local-effort:${id}:${level}`); }, showError: message => { throw Error(message); } });
  return null;
}
const paint = (tab: string, generation = "1", remote = true) => act(async () => root.render(<LocaleProvider><RemoteNavigationHarness><Probe tab={tab} generation={generation} remote={remote} /></RemoteNavigationHarness></LocaleProvider>));
try {
  await paint("A");
  await send("/model model-fixture"); await send("/effort high"); await send("/compact fixture");
  await send("/context"); await send("/clear");
  assert.deepEqual(calls, ["model:model-fixture", "effort:high", "compact:fixture", "manage:/context:false", "clear"]);
  calls.length = 0;
  await send("/new");
  assert.deepEqual(calls, ["navigation-intent", "new:fixture:fixture:true"], "new-session uses the common owner; Serve lifecycle hydrates the target instead of the captured source callback");
  calls.length = 0;
  const pending = send("display", " submit bytes ");
  assert.deepEqual(calls, ["goal:A:submit bytes"]);
  await paint("B"); gate.resolve(); await pending;
  assert.deepEqual(calls, ["goal:A:submit bytes", "send:A:display: submit bytes "], "goal and send keep source A and preserve provider-visible submit bytes");
  calls.length = 0; gate = deferred(); await paint("A");
  const replaced = send("next"); await paint("A", "2"); gate.resolve(); await replaced;
  assert.deepEqual(calls, ["goal:A:next"], "replacement session receives no stale post-goal submit");
  calls.length = 0;
  runtime.pauseGoal(); await action; runtime.resumeGoal(); await action;
  assert.deepEqual(calls, ["remote-pause", "remote-resume"]);
  calls.length = 0; await paint("A", "2", false);
  runtime.pauseGoal(); await action; runtime.resumeGoal(); await action; runtime.setEffort("max");
  await act(async () => {});
  assert.deepEqual(calls, ["pause:A", "resume:A", "local-effort:A:max"]);
  calls.length = 0; gate = deferred(); await paint("A");
  const disposed = send("disposed"); await act(async () => root.unmount()); gate.resolve(); await disposed;
  runtime.pauseGoal();
  assert.deepEqual(calls, ["goal:A:disposed"]);
  console.log("remote composer commands: management routing, source Goal/send, byte preservation, runtime ports and disposal passed");
} finally { dom.window.close(); }
