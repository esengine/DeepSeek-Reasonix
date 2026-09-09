import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { Composer } from "../components/Composer";
import { LocaleProvider } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";
import { projectConversation } from "../app-runtime/conversationProjection";
import { initialState } from "../lib/useController";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost/", pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, localStorage: dom.window.localStorage,
  IS_REACT_ACT_ENVIRONMENT: true, requestAnimationFrame: () => 1, cancelAnimationFrame() {},
  ResizeObserver: class { observe() {} disconnect() {} unobserve() {} },
});
for (const name of ["Node", "Element", "HTMLElement", "HTMLTextAreaElement", "Event", "CustomEvent", "MutationObserver", "File"]) {
  Object.defineProperty(globalThis, name, { configurable: true, value: Reflect.get(dom.window, name) });
}
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
window.matchMedia = (() => ({ matches: true, addEventListener() {}, removeEventListener() {} })) as typeof window.matchMedia;
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { value() {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { value() {} });
const calls: string[] = [];
let localWrites = 0;
const forbidden = async () => { localWrites++; throw new Error("remote surface called local file/inbox mutation"); };
Object.assign(window, { go: { main: { App: { SavePastedFile: forbidden, SavePastedImage: forbidden,
  ModelsForTab: async () => [], ListInboxItems: async () => [],
  EnqueueInboxFollowup: forbidden, EnqueueInboxSteer: forbidden, EnqueueInboxSteerForTurn: forbidden,
  EnqueueInboxFollowupWithInvocations: forbidden,
} } } });
const root = createRoot(document.getElementById("root")!);
const noop = () => {};
const view = projectConversation({ local: initialState, remote: { transcript: initialState, running: true,
  modelLabel: "remote fixture", commands: [] }, activeTabId: "remote-A", backgroundRuntimes: [], connectingLabel: "connecting" });
try {
  await act(async () => root.render(<LocaleProvider><ToastProvider><Composer {...view.composer}
    tabId="remote-A" sessionKey="remote-A" collaborationMode="normal" toolApprovalMode="ask" goal="" ready
    onSend={() => { calls.push("send"); }} onSteer={async (text, tab) => { calls.push(`steer:${tab}:${text}`); }}
    onCancel={noop} onCycleMode={noop} onSetMode={noop} onSetCollaborationMode={noop}
    onSetToolApprovalMode={noop} onToggleYoloApprovalMode={noop} onClearGoal={noop}
    onSwitchModel={noop} onSetEffort={noop} insertRequest={{ id: 1, text: "remote guidance", mode: "replace" }}
  /></ToastProvider></LocaleProvider>));
  const input = document.querySelector<HTMLInputElement>("input[type=file]")!;
  assert.equal(input.disabled, true);
  assert.equal(document.querySelector<HTMLElement>(".composer-wrap")!.style.getPropertyValue("--wails-drop-target"), "");
  await act(async () => {
    Object.defineProperty(input, "files", { value: [new File(["fixture"], "fixture.txt")] });
    input.dispatchEvent(new Event("change", { bubbles: true }));
  });
  const button = document.querySelector<HTMLButtonElement>(".composer__btn--send")!;
  assert.equal(button.disabled, false);
  await act(async () => button.click());
  assert.deepEqual(calls, ["steer:remote-A:remote guidance"], "running remote input uses the remote steer port, never conversational submit");
  assert.equal(localWrites, 0);
  assert.equal(document.querySelector<HTMLTextAreaElement>("#composer-input")!.value, "");
  console.log("remote Composer: real file control, native drop boundary and running guidance preserve source routing");
} finally { await act(async () => root.unmount()); dom.window.close(); }
