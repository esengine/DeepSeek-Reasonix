// Run: tsx src/__tests__/workspace-selection-comment.test.tsx

import { JSDOM } from "jsdom";
import { registerHooks } from "node:module";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { WorkspacePanel } from "../components/WorkspacePanel";
import { LocaleProvider } from "../lib/i18n";
import { resetWorkspaceTreeMemoryForTests } from "../lib/workspaceTreeMemory";
import type { AppBindings } from "../lib/bridge";

// Markdown previews lazy-load MarkdownRenderer, whose KaTeX stylesheet belongs
// to the same production chunk. Node's tsx loader has no CSS module support,
// so map stylesheet imports to the existing empty asset stub in this DOM test.
registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.endsWith(".css")) {
      return nextResolve("./asset-stub-for-tests.ts", { ...context, parentURL: import.meta.url });
    }
    return nextResolve(specifier, context);
  },
});

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    await act(async () => {
      await flushPromises();
    });
    if (predicate()) return;
  }
  ok(false, `timeout waiting for ${label}`);
}

class TestResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

function installDom() {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.HTMLTextAreaElement = dom.window.HTMLTextAreaElement;
  globalThis.Event = dom.window.Event;
  globalThis.InputEvent = dom.window.InputEvent;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.PointerEvent = dom.window.MouseEvent as unknown as typeof PointerEvent;
  globalThis.MutationObserver = dom.window.MutationObserver;
  globalThis.ResizeObserver = TestResizeObserver;
  dom.window.ResizeObserver = TestResizeObserver;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  (dom.window.HTMLElement.prototype as unknown as { attachEvent: () => void }).attachEvent = () => {};
  (dom.window.HTMLElement.prototype as unknown as { detachEvent: () => void }).detachEvent = () => {};
  Object.defineProperty(dom.window.HTMLElement.prototype, "scrollIntoView", { configurable: true, value: () => {} });
  Object.defineProperty(dom.window.HTMLElement.prototype, "offsetWidth", {
    configurable: true,
    get: () => 320,
  });
  Object.defineProperty(dom.window.HTMLElement.prototype, "offsetHeight", {
    configurable: true,
    get: function offsetHeight(this: HTMLElement) {
      return this.classList.contains("workspace-tree") ? 300 : this.dataset.index ? 24 : 0;
    },
  });
  Object.defineProperty(dom.window.HTMLElement.prototype, "getBoundingClientRect", {
    configurable: true,
    value: function getBoundingClientRect(this: HTMLElement) {
      const width = 320;
      const height = this.classList.contains("workspace-tree") ? 300 : this.dataset.index ? 24 : 0;
      return {
        x: 0,
        y: 0,
        top: 0,
        left: 0,
        right: width,
        bottom: height,
        width,
        height,
        toJSON: () => ({}),
      } as DOMRect;
    },
  });
  return dom;
}

const CODE_BODY = "const value = 1;\nfunction handleError() {\n  return 42;\n}\n";

type CommentCall = { path: string; code: string; comment?: string };

async function renderSelectionWorkspace(onAddCodeToChat: (path: string, code: string, comment?: string) => void) {
  resetWorkspaceTreeMemoryForTests();
  const dom = installDom();
  window.go = {
    main: {
      App: {
        ListDirForTab: async (_tabID: string, rel: string) => (rel === "" ? [{ name: "app.ts", path: "app.ts", isDir: false }] : []),
        SearchFileRefsForTab: async () => [],
        WorkspaceGitHistory: async () => [],
        WorkspaceChanges: async () => ({ files: [], gitAvailable: true }),
        WorkspaceChangeDetail: async () => ({}),
        ReadFileForTab: async (_tabID: string, path: string) => ({
          path,
          body: CODE_BODY,
          size: CODE_BODY.length,
          truncated: false,
          binary: false,
        }),
      } as Partial<AppBindings> as AppBindings,
    },
  };
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const calls: CommentCall[] = [];
  await act(async () => {
    root.render(
      <LocaleProvider>
        <WorkspacePanel
          open
          tabId="tab-a"
          cwd="/repo"
          maximized={false}
          initialViewMode="files"
          onClose={() => {}}
          onToggleMaximized={() => {}}
          onAddCodeToChat={(path, code, comment) => calls.push({ path, code, comment })}
        />
      </LocaleProvider>,
    );
    await flushPromises();
  });
  await waitFor("file tree row", () => Boolean(document.querySelector('[data-workspace-path="app.ts"]')));
  await act(async () => {
    (document.querySelector('[data-workspace-path="app.ts"]') as HTMLElement | null)?.click();
    await flushPromises();
  });
  await waitFor("code preview", () => document.querySelector(".workspace-preview__body")?.textContent?.includes("handleError") === true);
  return { dom, root, calls };
}

// Selects all code lines inside the preview and returns the exact text the
// floating "Add to Chat" menu will read from the DOM selection (line-number
// gutters stay outside the range).
function selectPreviewText(): string {
  const body = document.querySelector(".workspace-preview__body") as HTMLElement | null;
  if (!body) throw new Error("preview body did not render");
  const lines = Array.from(body.querySelectorAll<HTMLElement>(".code-line-text, pre.code code"));
  const first = lines[0];
  const last = lines[lines.length - 1];
  if (!first || !last) throw new Error("code block did not render in preview");
  const selection = window.getSelection();
  if (!selection) throw new Error("window selection is unavailable");
  const range = document.createRange();
  const firstNode = first.firstChild;
  const lastNode = last.lastChild;
  if (!firstNode || !lastNode) throw new Error("code block has no text");
  range.setStart(firstNode, 0);
  range.setEnd(lastNode, lastNode.textContent?.length ?? 0);
  selection.removeAllRanges();
  selection.addRange(range);
  return selection.toString();
}

async function openSelectionMenu(): Promise<void> {
  const body = document.querySelector(".workspace-preview__body") as HTMLElement;
  await act(async () => {
    body.dispatchEvent(new MouseEvent("mouseup", { bubbles: true, cancelable: true, clientX: 120, clientY: 140 }));
    await flushPromises();
  });
}

function floatingMenuButton(label: string): HTMLButtonElement {
  const button = Array.from(document.querySelectorAll<HTMLButtonElement>(".floating-menu button")).find((candidate) =>
    candidate.textContent?.includes(label),
  );
  if (!button) throw new Error(`floating menu button "${label}" did not render`);
  return button;
}

async function openCommentPopover(): Promise<HTMLElement> {
  await openSelectionMenu();
  await act(async () => {
    floatingMenuButton("Add to Chat").click();
    await flushPromises();
  });
  const popover = document.querySelector(".workspace-comment-popover") as HTMLElement | null;
  if (!popover) throw new Error("comment popover did not render");
  return popover;
}

async function typeComment(popover: HTMLElement, text: string): Promise<void> {
  const input = popover.querySelector("textarea");
  if (!input) throw new Error("comment textarea did not render");
  await act(async () => {
    // focus() fires focusin, which React's IE-compat change-event polyfill uses
    // to track the active element before keyup can report the value change.
    input.focus();
    const setter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, "value")?.set;
    if (!setter) throw new Error("textarea value setter is unavailable");
    setter.call(input, text);
    input.dispatchEvent(new window.InputEvent("input", { bubbles: true, data: text.slice(-1), inputType: "insertText" }));
    input.dispatchEvent(new window.Event("change", { bubbles: true }));
    input.dispatchEvent(new window.KeyboardEvent("keyup", { key: "x", bubbles: true }));
  });
}

console.log("\nworkspace selection comment popover");

{
  const { dom, root, calls } = await renderSelectionWorkspace((path, code, comment) => calls.push({ path, code, comment }));
  const selected = selectPreviewText();
  ok(selected.includes("handleError"), "test selection captures the previewed code");

  await openSelectionMenu();
  ok(Boolean(floatingMenuButton("Add to Chat")), "the selection menu offers Add to Chat before the popover");
  let popover: HTMLElement | null = null;
  await act(async () => {
    floatingMenuButton("Add to Chat").click();
    await flushPromises();
  });
  popover = document.querySelector(".workspace-comment-popover");
  if (!popover) throw new Error("comment popover did not render");
  await typeComment(popover, "  check the catch block  ");
  await act(async () => {
    (popover.querySelector(".workspace-comment-popover__confirm") as HTMLElement | null)?.click();
    await flushPromises();
  });
  eq(calls.length, 1, "confirming with a comment calls onAddCodeToChat once");
  eq(calls[0]?.path, "app.ts", "the comment is attached to the selected file path");
  eq(calls[0]?.code, selected, "the comment flow sends the selected code");
  eq(calls[0]?.comment, "check the catch block", "the comment is trimmed before being sent");
  eq(document.querySelector(".workspace-comment-popover"), null, "confirming closes the comment popover");

  await act(async () => root.unmount());
  dom.window.close();
}

{
  const { dom, root, calls } = await renderSelectionWorkspace((path, code, comment) => calls.push({ path, code, comment }));
  selectPreviewText();
  const popover = await openCommentPopover();
  await act(async () => {
    (popover.querySelector(".workspace-comment-popover__confirm") as HTMLElement | null)?.click();
    await flushPromises();
  });
  eq(calls.length, 1, "an empty comment still adds the selection");
  eq(calls[0]?.comment, undefined, "an empty comment is not passed to the composer");
  eq(calls[0]?.code.includes("handleError"), true, "the selection is sent even without a comment");

  await act(async () => root.unmount());
  dom.window.close();
}

{
  const { dom, root, calls } = await renderSelectionWorkspace((path, code, comment) => calls.push({ path, code, comment }));
  selectPreviewText();
  const popover = await openCommentPopover();
  await act(async () => {
    (popover.querySelector(".workspace-comment-popover__cancel") as HTMLElement | null)?.click();
    await flushPromises();
  });
  eq(calls.length, 0, "cancelling the comment popover adds nothing");
  eq(document.querySelector(".workspace-comment-popover"), null, "cancelling closes the comment popover");

  await act(async () => root.unmount());
  dom.window.close();
}

{
  const { dom, root, calls } = await renderSelectionWorkspace((path, code, comment) => calls.push({ path, code, comment }));
  selectPreviewText();
  const popover = await openCommentPopover();
  await act(async () => {
    popover.querySelector("textarea")?.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
    await flushPromises();
  });
  eq(calls.length, 0, "Escape discards the comment without adding");
  eq(document.querySelector(".workspace-comment-popover"), null, "Escape closes the comment popover");

  await act(async () => root.unmount());
  dom.window.close();
}

{
  const { dom, root, calls } = await renderSelectionWorkspace((path, code, comment) => calls.push({ path, code, comment }));
  selectPreviewText();
  const popover = await openCommentPopover();
  await typeComment(popover, "cmd+enter should submit");
  await act(async () => {
    popover.querySelector("textarea")?.dispatchEvent(
      new window.KeyboardEvent("keydown", { key: "Enter", metaKey: true, bubbles: true, cancelable: true }),
    );
    await flushPromises();
  });
  eq(calls.length, 1, "Cmd+Enter confirms the comment");
  eq(calls[0]?.comment, "cmd+enter should submit", "Cmd+Enter sends the typed comment");

  await act(async () => root.unmount());
  dom.window.close();
}

{
  const { dom, root, calls } = await renderSelectionWorkspace((path, code, comment) => calls.push({ path, code, comment }));
  selectPreviewText();
  const popover = await openCommentPopover();
  await act(async () => {
    popover.querySelector("textarea")?.dispatchEvent(new window.Event("scroll", { bubbles: true }));
    await flushPromises();
  });
  ok(document.querySelector(".workspace-comment-popover") != null, "scrolling inside the comment textarea keeps the popover open");
  eq(calls.length, 0, "scrolling inside the comment textarea adds nothing");

  await act(async () => root.unmount());
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
