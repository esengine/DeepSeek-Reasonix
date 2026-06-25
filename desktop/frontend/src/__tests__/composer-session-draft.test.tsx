// Run: tsx src/__tests__/composer-session-draft.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { Composer } from "../components/Composer";
import { composerDraftKeyForTab } from "../lib/composerDraftKey";
import { LocaleProvider } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";
import type { CollaborationMode, TokenMode, ToolApprovalMode } from "../lib/types";

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

function flushTimers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => {
    resolve = next;
  });
  return { promise, resolve };
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
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.HTMLTextAreaElement = dom.window.HTMLTextAreaElement;
  globalThis.Event = dom.window.Event;
  globalThis.CustomEvent = dom.window.CustomEvent;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.InputEvent = dom.window.InputEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.File = dom.window.File;
  globalThis.FileReader = dom.window.FileReader;
  globalThis.PointerEvent = dom.window.MouseEvent as unknown as typeof PointerEvent;
  globalThis.MutationObserver = dom.window.MutationObserver;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  globalThis.ResizeObserver = TestResizeObserver;
  Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
  Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: () => ({
      matches: true,
      media: "(prefers-reduced-motion: reduce)",
      onchange: null,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
      dispatchEvent: () => false,
    }),
  });
  return dom;
}

function installBridgeApp(methods: Record<string, unknown>) {
  (window as unknown as { go: { main: { App: Record<string, unknown> } } }).go = {
    main: {
      App: {
        Commands: async () => [],
        Models: async () => [],
        ModelsForTab: async () => [],
        ...methods,
      },
    },
  };
}

async function renderComposer(props: Partial<Parameters<typeof Composer>[0]> = {}) {
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  let currentProps: Parameters<typeof Composer>[0] = {
    running: false,
    collaborationMode: "normal",
    toolApprovalMode: "ask" as ToolApprovalMode,
    tokenMode: "full" as TokenMode,
    goal: "",
    cwd: "/repo",
    modelLabel: "DeepSeek-R1",
    tabId: "single-surface-tab",
    sessionKey: "session:project:/repo:topic-a:session-a",
    onSend: () => {},
    onCancel: () => undefined,
    onCycleMode: () => {},
    onSetMode: () => {},
    onSetCollaborationMode: (_mode: CollaborationMode) => {},
    onSetToolApprovalMode: () => {},
    onToggleYoloApprovalMode: () => {},
    onClearGoal: () => {},
    onSwitchModel: () => {},
    onSetEffort: () => {},
    onSetTokenMode: () => {},
    ready: true,
    ...props,
  };
  const paint = async (nextProps: Partial<Parameters<typeof Composer>[0]> = {}) => {
    currentProps = { ...currentProps, ...nextProps };
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ToastProvider>
            <Composer {...currentProps} />
          </ToastProvider>
        </LocaleProvider>,
      );
      await flushTimers();
    });
  };
  await paint();
  return { root, rerender: paint };
}

function textarea(): HTMLTextAreaElement {
  const node = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!node) throw new Error("composer textarea did not render");
  return node;
}

function contextItemCount(): number {
  return document.querySelectorAll(".composer-context__item").length;
}

console.log("\ncomposer session draft");

{
  const withoutPath = composerDraftKeyForTab({
    id: "tab-a",
    scope: "project",
    workspaceRoot: "/repo",
    topicId: "topic-a",
    sessionPath: "",
  }, "tab-a");
  const withPath = composerDraftKeyForTab({
    id: "tab-a",
    scope: "project",
    workspaceRoot: "/repo",
    topicId: "topic-a",
    sessionPath: "/repo/.reasonix/sessions/topic-a.jsonl",
  }, "tab-a");
  eq(withPath, withoutPath, "topic draft key stays stable when session path appears");
}

{
  const dom = installDom();
  const { root, rerender } = await renderComposer();

  await rerender({ insertRequest: { id: 1, text: "draft for A", mode: "replace" } });
  await rerender({ insertRequest: { id: 2, text: "@/repo/src/app.ts", mode: "insert" } });
  eq(textarea().value, "draft for A", "session A text is visible before switching");
  eq(contextItemCount(), 1, "session A workspace ref is visible before switching");

  await rerender({ sessionKey: "session:project:/repo:topic-b:session-b" });
  eq(textarea().value, "", "session B does not inherit session A text");
  eq(contextItemCount(), 0, "session B does not inherit session A context refs");

  await rerender({ insertRequest: { id: 3, text: "draft for B", mode: "replace" } });
  eq(textarea().value, "draft for B", "session B keeps its own text draft");

  await rerender({ sessionKey: "session:project:/repo:topic-a:session-a" });
  eq(textarea().value, "draft for A", "session A text is restored when switching back");
  eq(contextItemCount(), 1, "session A context refs are restored when switching back");

  await rerender({ sessionKey: "session:project:/repo:topic-b:session-b" });
  eq(textarea().value, "draft for B", "session B text is restored independently");
  eq(contextItemCount(), 0, "session B context refs stay isolated");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const saveStarted = deferred<void>();
  const savePastedFile = deferred<string>();
  installBridgeApp({
    SavePastedFile: async () => {
      saveStarted.resolve();
      return savePastedFile.promise;
    },
  });
  const { root, rerender } = await renderComposer();
  const file = new File(["draft attachment"], "draft.txt", { type: "text/plain", lastModified: 1 });
  const event = new Event("paste", { bubbles: true, cancelable: true });
  Object.defineProperty(event, "clipboardData", {
    configurable: true,
    value: {
      files: [file],
      items: [],
      types: [],
      getData: () => "",
    },
  });

  await act(async () => {
    textarea().dispatchEvent(event);
    await saveStarted.promise;
  });
  await rerender({ sessionKey: "session:project:/repo:topic-b:session-b" });
  await act(async () => {
    savePastedFile.resolve("/tmp/reasonix/draft.txt");
    await flushTimers();
  });
  eq(contextItemCount(), 0, "async attachment does not land in the switched-to session");

  await rerender({ sessionKey: "session:project:/repo:topic-a:session-a" });
  eq(contextItemCount(), 1, "async attachment returns to the source session draft");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
