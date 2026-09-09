import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import type { TopicbarView } from "../app-shell/TopicbarRegion";

const dom = new JSDOM("<div id='root'></div>", { pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, HTMLElement: dom.window.HTMLElement,
  KeyboardEvent: dom.window.KeyboardEvent, IS_REACT_ACT_ENVIRONMENT: true });
const { default: React, act } = await import("react");
const { createRoot } = await import("react-dom/client");
const { TopicbarRegion } = await import("../app-shell/TopicbarRegion");
const { LocaleProvider } = await import("../lib/i18n");
const root = createRoot(document.getElementById("root")!);
const calls: string[] = [];
const commands = {
  openAutomation: () => { calls.push("automation"); }, toggleSidebar: () => { calls.push("sidebar"); },
  setTitleDraft: (value: string) => { calls.push(`draft:${value}`); }, commitRename: () => { calls.push("commit"); },
  cancelRename: () => { calls.push("cancel"); }, startRename: () => { calls.push("rename"); },
  openWorktree: (id: string) => { calls.push(`worktree:${id}`); },
};
const view: TopicbarView = {
  automationReturn: true, automationReturnLabel: "Back to automation", chromeHidden: true,
  sidebar: { title: "Sidebar", blocked: false, pressed: false, collapsed: true },
  title: { text: "A", hover: "Full A", renameLabel: "Rename", editing: false, draft: "Draft A", editSize: 12, canRename: true, workspaceLabel: "Project" },
  subtitle: { visible: true, title: "Workspace", worktreeTabId: "A", mergeLabel: "Merge", mergeTooltip: "Merge back", sourcePlatform: "feishu", sourceLabel: "Channel" },
};
const paint = (next = view) => act(async () => root.render(<LocaleProvider><TopicbarRegion view={next} commands={commands}>
  <div className="topicbar__actions"><button>Fixture action</button></div>
</TopicbarRegion></LocaleProvider>));
const click = (selector: string) => act(async () => document.querySelector<HTMLButtonElement>(selector)!.click());
try {
  await paint();
  assert.deepEqual([...document.querySelector("header")!.children].map(node => node.className),
    ["btn btn--small", "tooltip-trigger", "topicbar__identity", "topicbar__spacer", "topicbar__actions"], "region extraction adds no DOM wrapper");
  const action = document.querySelector(".topicbar__actions button");
  assert.equal(document.querySelectorAll(".topicbar__subtitle .worktree-badge").length, 1);
  assert.ok(document.querySelector(".worktree-badge")!.getAttribute("aria-label"), "isolated worktree identity is accessible");
  await click(".topicbar__title-button");
  await click(".topicbar__worktree-btn");
  await click(".topicbar__chrome-btn");
  await click(".btn");
  assert.deepEqual(calls, ["rename", "worktree:A", "sidebar", "automation"]);
  assert.equal(document.activeElement, document.querySelector(".btn"), "return control establishes focus synchronously before navigating");
  await paint({ ...view, sidebar: { ...view.sidebar, blocked: true }, subtitle: { ...view.subtitle, worktreeTabId: "B" } });
  await click(".topicbar__chrome-btn");
  await click(".topicbar__worktree-btn");
  assert.equal(calls.filter(value => value === "sidebar").length, 1);
  assert.equal(calls.at(-1), "worktree:B", "synchronous command receives the rendered source identity");
  await paint({ ...view, title: { ...view.title, editing: true } });
  const input = document.querySelector<HTMLInputElement>("input")!;
  assert.equal(input.value, "Draft A"); assert.equal(input.size, 12);
  assert.equal(input.selectionStart, 0); assert.equal(input.selectionEnd, input.value.length);
  await act(async () => {
    Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")!.set!.call(input, "Renamed A");
    input.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  });
  assert.equal(calls.at(-1), "draft:Renamed A", "DOM event is converted synchronously to a draft value");
  const enter = new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true });
  const escape = new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true });
  await act(async () => { input.dispatchEvent(enter); input.dispatchEvent(escape); });
  assert.equal(enter.defaultPrevented, true); assert.equal(escape.defaultPrevented, true);
  assert.deepEqual(calls.slice(-2), ["commit", "cancel"]);
  await act(async () => input.blur());
  assert.equal(calls.at(-1), "commit", "blur retains the existing rename commit contract");
  assert.equal(action, document.querySelector(".topicbar__actions button"), "title editing preserves action subtree identity");
  await paint({ ...view, subtitle: { ...view.subtitle, worktreeTabId: undefined } });
  assert.equal(document.querySelector(".worktree-badge"), null, "ordinary topics do not claim isolated worktree identity");
  assert.equal(document.querySelector(".topicbar__worktree-btn"), null, "ordinary topics expose no worktree merge action");
  await act(async () => root.unmount());
  console.log("topicbar region: DOM structure, source commands, focus, rename keys and action identity passed");
} finally { dom.window.close(); }
