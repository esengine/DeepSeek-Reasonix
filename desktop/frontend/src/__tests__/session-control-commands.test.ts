import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useSessionOperations, type SessionResource } from "../app-runtime/useSessionOperations";
import { useSessionControlCommands } from "../app-runtime/useSessionControlCommands";
import { sessionIdentityKey } from "../app-runtime/sessionTarget";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const resource = (tabId: string, generation = 1): SessionResource => ({ tabId,
  sessionKey: sessionIdentityKey({ tabId, sessionPath: `/${tabId}`, sessionGeneration: generation }) });
const a = resource("A"), b = resource("B");
const calls: string[] = [], errors: string[] = [];
let finish: ((value: boolean) => void) | undefined;
let started: (() => void) | undefined;
let delayed = false;
let commands!: ReturnType<typeof useSessionControlCommands>;
function Probe({ visible = a, resources = [a, b] }: { visible?: SessionResource; resources?: SessionResource[] }) {
  const operations = useSessionOperations({ visible, resources });
  commands = useSessionControlCommands({ activeTabId: visible.tabId, resources, operations,
    showToast: message => errors.push(message), clearWorkspaceConflict() {}, ports: {
      cancel: async () => ({ discardedItemIds: [] }), cancelForTab: async () => ({ discardedItemIds: [] }),
      acceptDelivery: async () => {}, disconnectRemote: async () => {},
      cancelJobForTab: async (tab, job) => {
        calls.push(`${tab}:${job}`);
        if (!delayed) return true;
        const result = new Promise<boolean>(resolve => { finish = resolve; });
        started?.();
        return result;
      }, refreshBackgroundRuntimes: async () => { calls.push("refresh"); },
    } });
  return null;
}
const root = createRoot(document.getElementById("root")!);
const paint = (resources = [a, b], visible = a) => act(async () => root.render(React.createElement(Probe, { resources, visible })));
try {
  await paint();
  const retained = commands.cancelRuntimeJob;
  assert.equal(await retained("B", "background"), true);
  assert.deepEqual(calls, ["B:background"], "background cancellation reaches its source port without taking active UI ownership");
  assert.equal(await retained("A", "active"), true);
  assert.deepEqual(calls.slice(1), ["A:active", "refresh"]);
  assert.equal(await retained("missing", "gone"), false);
  assert.equal(calls.length, 3, "removed resources never reach the bridge");

  delayed = true;
  let entered = new Promise<void>(resolve => { started = resolve; });
  const stale = retained("B", "old-generation");
  await entered;
  await paint([a, resource("B", 2)]);
  finish!(true);
  assert.equal(await stale, false, "replacement invalidates an in-flight cancellation result");
  assert.equal(calls[calls.length - 1], "B:old-generation", "stale results cannot refresh replacement UI");

  entered = new Promise<void>(resolve => { started = resolve; });
  const switched = retained("B", "current-generation");
  await entered;
  await paint([a, resource("B", 2)], resource("B", 2));
  finish!(true);
  assert.equal(await switched, true, "switching visible tabs does not cancel a live source operation");
  assert.deepEqual(errors, []);
  await act(async () => root.unmount());
  const count = calls.length;
  await retained("B", "unmounted");
  assert.equal(calls.length, count, "retained commands are inert after unmount");
  console.log("session control commands: canonical background target, active target, replacement, navigation and disposal passed");
} finally { dom.window.close(); }
