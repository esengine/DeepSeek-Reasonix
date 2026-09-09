import assert from "node:assert/strict";
import React, { act, type ComponentProps } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { TopicbarActionsRegion } from "../app-shell/TopicbarActionsRegion";
import { LocaleProvider } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost", pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document,
  Node: dom.window.Node, HTMLElement: dom.window.HTMLElement, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
const warnings: unknown[][] = [];
const originalError = console.error;
console.error = (...args) => { warnings.push(args); };
const opened: string[] = [];
const bridge = {
  async ExternalOpenersForTab() { return { openers: [{ id: "editor", name: "Editor", kind: "editor" as const }], preferred: "editor", workspaceOpenable: true }; },
  async SetPreferredExternalOpener() {},
  async OpenWorkspaceInExternalOpenerForTab(tabId: string) { opened.push(tabId); },
};
const noop = () => {};
const session: ComponentProps<typeof TopicbarActionsRegion>["session"] = {
  sessionHasContent: true, getSessionMarkdown: () => "fixture", exportSession: noop,
  toggleTerminal: noop, terminalOpen: false, openSessionSummary: noop, tasksOpen: false,
};
try {
  let baseline = 0;
  for (let index = 0; index < 128; index++) {
    const tabId = index % 2 ? "B" : "A";
    await act(async () => root.render(<LocaleProvider><ToastProvider>
      <TopicbarActionsRegion sessionIdentity={tabId} external={{ tabId, dismissSignal: index, bridge }} session={session} />
    </ToastProvider></LocaleProvider>));
    assert.equal(document.querySelectorAll(".external-opener").length, 1, "one live external opener after every resource replacement");
    const count = document.querySelectorAll("*").length;
    if (!index) baseline = count;
    assert.equal(count, baseline, "reconciliation never leaves attached orphan controls");
  }
  await act(async () => document.querySelector<HTMLButtonElement>(".external-opener__primary")!.click());
  assert.deepEqual(opened, ["B"], "the surviving control routes only to the final session");
  assert.equal(warnings.length, 0, "resource replacement emits no duplicate-key or reconciliation warnings");
  await act(async () => root.unmount());
  assert.equal(document.querySelectorAll(".external-opener").length, 0);
  console.log("topicbar lifecycle: 128 replacements retain one action group and no orphan DOM");
} finally { console.error = originalError; dom.window.close(); }
