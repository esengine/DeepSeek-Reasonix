import React, { act, type ComponentProps } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import assert from "node:assert/strict";
import { DecisionFooterRegion } from "../app-shell/DecisionFooterRegion";
import { LocaleProvider } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost/", pretendToBeVisual: true });
Object.assign(globalThis, {
  window: dom.window, document: dom.window.document, localStorage: dom.window.localStorage,
  IS_REACT_ACT_ENVIRONMENT: true,
});
for (const name of ["Node", "Element", "HTMLElement", "HTMLTextAreaElement", "Event", "CustomEvent", "MutationObserver"]) {
  Object.defineProperty(globalThis, name, { configurable: true, value: Reflect.get(dom.window, name) });
}
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
Object.defineProperty(window, "matchMedia", { value: () => ({ matches: true, addEventListener() {}, removeEventListener() {} }) });
Object.assign(globalThis, {
  requestAnimationFrame: () => 1, cancelAnimationFrame() {},
  ResizeObserver: class { observe() {} disconnect() {} unobserve() {} },
});
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { value() {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { value() {} });
const root = createRoot(document.getElementById("root")!);
type Props = ComponentProps<typeof DecisionFooterRegion>;
const noop = () => {};
const composer: Props["composer"] = { hidden: false, inert: false, hero: false, props: {
  running: false, collaborationMode: "normal", toolApprovalMode: "ask", goal: "", cwd: "/fixture",
  modelLabel: "fixture-model", ready: true,
  onSend: noop, onCancel: noop, onCycleMode: noop, onSetMode: noop,
  onSetCollaborationMode: noop, onSetToolApprovalMode: noop, onToggleYoloApprovalMode: noop,
  onClearGoal: noop, onSwitchModel: noop, onSetEffort: noop,
  insertRequest: { id: 1, text: "retained draft", mode: "replace" },
} };
let props: Props = {
  hidden: false, className: "footer", footerRef: noop, composer,
  todo: { identity: "todo-a", props: { stateKey: "todo-a", todos: [{ content: "visible work", status: "in_progress" }],
    running: true, pendingPrompt: false, onDismiss: noop } },
  undo: { identity: "undo-a", props: { meta: { turns: 1, filesRestored: [], filesRemoved: [], onUndo: noop } } },
};
async function paint() {
  await act(async () => {
    root.render(<LocaleProvider><ToastProvider><DecisionFooterRegion {...props} /></ToastProvider></LocaleProvider>);
    await Promise.all([import("../components/TodoPanel"), import("../components/UndoRewindBanner"), import("../components/ClearContextCard")]);
  });
}

try {
  await paint();
  const textarea = document.querySelector<HTMLTextAreaElement>("#composer-input")!;
  assert.ok(textarea);
  assert.equal(textarea.value, "retained draft");
  const undo = document.querySelector(".undo-rewind")!;
  assert.ok(undo);
  const todo = Array.from(document.querySelectorAll(".prompt-shelf")).find((node) => node.textContent?.includes("visible work"))!;
  assert.ok(todo);

  props = { ...props, composer: { ...composer, hidden: true, inert: true } };
  await paint();
  const host = document.querySelector<HTMLElement>(".composer-decision-host")!;
  assert.equal(host.hidden, false, "navigation mask preserves the composer footprint");
  assert.ok(host.classList.contains("composer-decision-host--footprint-hidden"));
  assert.ok(host.hasAttribute("inert"), "masked composer rejects interactive input");
  assert.ok(document.querySelector("footer")?.hasAttribute("inert"));
  assert.equal(document.querySelector(".undo-rewind"), undo, "target rewind stays laid out below the mask");
  assert.ok(todo.isConnected, "target Todo stays mounted below the mask");
  assert.equal(document.querySelector("#composer-input"), textarea);
  assert.equal(textarea.value, "retained draft");

  props = { ...props, composer, decision: { kind: "clear-context", identity: "clear-a", props: { onCancel: noop, onConfirm: noop } } };
  await paint();
  assert.equal(document.querySelector("#composer-input"), textarea, "decision card does not remount Composer");
  assert.equal(host.hidden, true, "decision card hides the mounted Composer");
  props = { ...props, decision: undefined };
  await paint();
  assert.equal(document.querySelector("#composer-input"), textarea);
  assert.equal(textarea.value, "retained draft", "dismissed decision restores the original draft");
  console.log("PASS Decision Footer preserves masked layout, Todo/rewind hosts, and Composer identity/draft");
} finally {
  await act(async () => root.unmount());
  dom.window.close();
}
