import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useSessionClearCommands, type SessionClearCommandsInput } from "../app-runtime/useSessionClearCommands";
import type { Translator } from "../lib/i18n";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);

const t = ((key: string) => key) as Translator;
const authority = { checkpoint() {}, ownsUI: () => true };

const operationCalls: { target: unknown; channel: string; input: unknown }[] = [];
const portCalls: string[] = [];
const notices: { text: string; level?: string }[] = [];
let dockRefreshes = 0;
let clearError: Error | null = null;

const operations: SessionClearCommandsInput["operations"] = async (target, channel, input, execute) => {
  operationCalls.push({ target, channel, input });
  try {
    const value = await execute(input, authority);
    return { status: "completed", value };
  } catch (error) {
    return { status: "failed", error };
  }
};

let states!: ReturnType<typeof useSessionClearCommands>;
function Probe({ activeTabId, remote = false }: { activeTabId?: string; remote?: boolean }) {
  states = useSessionClearCommands({
    activeTabId,
    activeSessionIdentity: "A:1",
    remote,
    t,
    notice: (text, level) => { notices.push({ text, level }); },
    operations,
    refreshDock: () => { dockRefreshes += 1; },
    ports: {
      clearSession: async () => {
        portCalls.push("local");
        if (clearError) throw clearError;
      },
      clearRemoteSession: async (tabId) => { portCalls.push(`remote:${tabId}`); },
      retryRemoteHydration: async () => { portCalls.push("hydrate"); },
    },
  });
  return null;
}
const paint = (props?: { activeTabId?: string; remote?: boolean }) =>
  act(async () => root.render(<Probe activeTabId="A" {...props} />));

try {
  await paint();
  assert.equal(states.clearContextPending, false, "clear confirmation starts closed");
  await act(async () => { states.setClearContextPending(true); });
  assert.equal(states.clearContextPending, true, "requesting clear opens the confirmation");
  await act(async () => { states.cancelClearContext(); });
  assert.equal(states.clearContextPending, false, "cancel closes the confirmation");

  await act(async () => { states.setClearContextPending(true); });
  await act(async () => { await states.confirmClearContext(); });
  assert.equal(states.clearContextPending, false, "confirm closes the confirmation before executing");
  assert.deepEqual(operationCalls, [{ target: { tabId: "A", sessionKey: "A:1" }, channel: "clear-context", input: { remote: false } }],
    "confirm captures the committed tab and session identity at click time");
  assert.deepEqual(portCalls, ["local"], "local confirm clears through the controller port");
  assert.equal(dockRefreshes, 1, "a completed clear refreshes the dock");
  assert.deepEqual(notices, [{ text: "clearContext.done", level: undefined }], "a completed clear notices success");

  operationCalls.length = 0;
  portCalls.length = 0;
  notices.length = 0;
  dockRefreshes = 0;
  await paint({ remote: true });
  await act(async () => { await states.confirmClearContext(); });
  assert.deepEqual(operationCalls[0]?.input, { remote: true }, "remote surfaces route the remote flag into the operation");
  assert.deepEqual(portCalls, ["remote:A", "hydrate"], "remote confirm clears the remote tab and retries hydration");
  assert.equal(dockRefreshes, 1, "remote completion still refreshes the dock");

  operationCalls.length = 0;
  portCalls.length = 0;
  notices.length = 0;
  dockRefreshes = 0;
  await paint();
  clearError = new Error("boom");
  await act(async () => { await states.confirmClearContext(); });
  assert.deepEqual(notices, [{ text: "boom", level: "warn" }], "a failed clear surfaces the error as a warning");
  assert.equal(dockRefreshes, 0, "a failed clear does not refresh the dock");

  notices.length = 0;
  clearError = new Error("");
  await act(async () => { await states.confirmClearContext(); });
  assert.deepEqual(notices, [{ text: "clearContext.failed", level: "warn" }], "an empty error falls back to the localized failure notice");
  clearError = null;

  operationCalls.length = 0;
  await paint({ activeTabId: undefined });
  await act(async () => { states.setClearContextPending(true); });
  await act(async () => { await states.confirmClearContext(); });
  assert.equal(operationCalls.length, 0, "confirm without an active tab runs no operation");
  assert.equal(states.clearContextPending, true, "confirm without a target leaves the confirmation untouched");

  console.log("session clear commands: pending lifecycle, local/remote chains, failure notices and empty-target gate passed");
} finally { dom.window.close(); }
