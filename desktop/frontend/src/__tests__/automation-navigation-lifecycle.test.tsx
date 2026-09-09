import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useAutomationNavigation } from "../app-runtime/useAutomationNavigation";
import { useAppNavigationStore as navigation } from "../store/appNavigation";
import type { DesktopNavigationIntent } from "../app-runtime/desktopNavigationOwner";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
let seq = 0;
const queued: { request: DesktopNavigationIntent; intent: number; resolve(): void }[] = [];
let commands!: ReturnType<typeof useAutomationNavigation>;
function Probe() {
  commands = useAutomationNavigation({ noteIntent: () => ++seq,
    enqueue: (request, intent) => new Promise<void>(resolve => queued.push({ request, intent, resolve })) });
  return null;
}
try {
  await act(async () => root.render(<Probe />));
  navigation.getState().openPage({ kind: "automation" });
  const a = commands.openAutomationTopic("project", "fixture", "A");
  assert.deepEqual(queued[0].request, { kind: "topic", scope: "project", workspaceRoot: "fixture", topicId: "A" });
  assert.equal(navigation.getState().page.kind, "automation", "keep the management page until the target is accepted");
  commands.topicAccepted(queued[0].intent);
  assert.equal(navigation.getState().page.kind, "workspace");
  assert.equal(navigation.getState().automationReturn, true);
  navigation.getState().openPage({ kind: "automation" });
  const b = commands.openAutomationTopic("project", "fixture", "B");
  queued[0].resolve(); await a;
  commands.topicAccepted(queued[1].intent);
  assert.equal(navigation.getState().page.kind, "workspace", "old finally cannot retire the newer link");
  queued[1].resolve(); await b;
  navigation.getState().openPage({ kind: "automation" });
  const c = commands.openAutomationTopic("project", "fixture", "C");
  const original = queued[2].intent;
  navigation.getState().openPage({ kind: "settings", tab: "general" });
  navigation.getState().openPage({ kind: "automation" });
  commands.topicAccepted(original);
  assert.equal(navigation.getState().page.kind, "automation", "ABA page replacement cannot regain navigation rights");
  assert.ok(seq > original, "page replacement revokes the controller's navigation intent");
  queued[2].resolve(); await c;
  const d = commands.openAutomationTopic("project", "fixture", "D");
  const last = queued[3].intent;
  queued[3].resolve(); await d;
  commands.topicAccepted(last);
  assert.equal(navigation.getState().page.kind, "automation", "an unaccepted terminal request leaves no live link");
  await act(async () => root.unmount());
  const before = seq;
  commands.openAutomationTopic("project", "fixture", "disposed");
  navigation.getState().returnToWorkspace();
  assert.equal(seq, before, "unmount releases both command and page subscription");
  console.log("automation navigation: accepted-target return, page ABA, exact finally and disposal passed");
} finally { dom.window.close(); }
