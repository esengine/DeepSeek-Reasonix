// Run: tsx src/__tests__/composer-session-draft.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { Composer } from "../components/Composer";
import { invalidateCache } from "../lib/composerHistory";
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
        ListDir: async () => [],
        ListDirForTab: async () => [],
        SearchFileRefs: async () => [],
        SearchFileRefsForTab: async () => [],
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
    const switchingDraft = nextProps.sessionKey !== undefined && nextProps.sessionKey !== currentProps.sessionKey;
    currentProps = {
      ...currentProps,
      ...(switchingDraft ? { insertRequest: null } : {}),
      ...nextProps,
    };
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

function sendButton(): HTMLButtonElement {
  const node = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  if (!node) throw new Error("composer send button did not render");
  return node;
}

function contextItemCount(): number {
  return document.querySelectorAll(".composer-context__item").length;
}

async function openComposerInputMenu(): Promise<HTMLButtonElement[]> {
  await act(async () => {
    textarea().dispatchEvent(new window.MouseEvent("contextmenu", {
      bubbles: true,
      cancelable: true,
      clientX: 20,
      clientY: 20,
    }));
    await flushTimers();
  });
  return Array.from(document.querySelectorAll(".context-menu__item")) as HTMLButtonElement[];
}

function textPasteEvent(text: string): Event {
  const event = new Event("paste", { bubbles: true, cancelable: true });
  Object.defineProperty(event, "clipboardData", {
    configurable: true,
    value: {
      files: [],
      items: [],
      types: ["text/plain"],
      getData: (kind: string) => (kind === "text" || kind === "text/plain" ? text : ""),
    },
  });
  return event;
}

async function drainAnimationFrame() {
  await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
  await flushTimers();
}

console.log("\ncomposer session draft");

{
  const dom = installDom();
  const { root, rerender } = await renderComposer();
  const content = document.querySelector(".composer__content") as HTMLDivElement | null;
  if (!content) throw new Error("composer content area did not render");

  textarea().blur();
  await act(async () => {
    content.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true }));
    await drainAnimationFrame();
  });
  eq(document.activeElement, textarea(), "clicking empty composer space focuses the text input");
  eq(textarea().selectionStart, 0, "empty composer space click places the caret on the first line");

  await rerender({ insertRequest: { id: 100, text: "existing draft", mode: "replace" } });
  await act(async () => {
    textarea().setSelectionRange(0, 0);
    textarea().blur();
    content.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true }));
    await drainAnimationFrame();
  });
  eq(textarea().selectionStart, textarea().value.length, "clicking composer space places an existing draft caret at the end");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

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
  // A queued follow-up belongs to the session where it was entered. Switching
  // to an idle session must neither auto-send it there nor discard it from the
  // running source session.
  const dom = installDom();
  const sent: Array<{ tab: string; text: string }> = [];
  const { root, rerender } = await renderComposer({
    running: true,
    tabId: "tab-a",
    sessionKey: "session:project:/repo:topic-a:session-a",
    onSend: (text, _submit, targetTabId) => {
      sent.push({ tab: targetTabId ?? "", text });
    },
  });

  await rerender({ insertRequest: { id: 10, text: "follow up in A", mode: "replace" } });
  await act(async () => {
    sendButton().click();
    await flushTimers();
  });
  ok(document.querySelector(".composer-guidance-item") !== null, "session A shows its queued guidance before switching");

  await rerender({
    running: false,
    tabId: "tab-b",
    sessionKey: "session:project:/repo:topic-b:session-b",
    onSend: (text, _submit, targetTabId) => {
      sent.push({ tab: targetTabId ?? "", text });
    },
  });
  eq(sent.length, 0, "switching to idle session B does not send session A guidance");
  ok(document.querySelector(".composer-guidance-item") === null, "session B does not inherit session A guidance shelf");

  await rerender({
    running: true,
    tabId: "tab-a",
    sessionKey: "session:project:/repo:topic-a:session-a",
    onSend: (text, _submit, targetTabId) => {
      sent.push({ tab: targetTabId ?? "", text });
    },
  });
  ok(document.querySelector(".composer-guidance-item") !== null, "session A restores its queued guidance after switching back");

  await rerender({ running: false });
  await act(async () => {
    await flushTimers();
  });
  eq(sent.length, 1, "session A sends its queued guidance when its own turn finishes");
  eq(sent[0]?.tab, "tab-a", "restored guidance stays routed to session A");
  eq(sent[0]?.text, "follow up in A", "restored guidance keeps its original text");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const sent: Array<{ display: string; submit?: string }> = [];
  const { root } = await renderComposer({
    onSend: (display, submit) => {
      sent.push({ display, submit });
    },
  });
  const rawPaste = "error: failed to compile\r\nat loader.ts:10\r\nat run.ts:22";
  const normalizedPaste = "error: failed to compile\nat loader.ts:10\nat run.ts:22";
  const event = textPasteEvent(rawPaste);

  await act(async () => {
    const input = textarea();
    input.selectionStart = input.selectionEnd = 0;
    input.dispatchEvent(event);
    await flushTimers();
  });

  eq(event.defaultPrevented, true, "short text paste is handled by React state");
  eq(textarea().value, normalizedPaste, "short multiline paste is visible in the composer");

  await act(async () => {
    sendButton().click();
    await flushTimers();
  });

  eq(sent.length, 1, "short multiline paste submits once");
  eq(sent[0]?.display, normalizedPaste, "short multiline paste is preserved in display text");
  eq(sent[0]?.submit, normalizedPaste, "short multiline paste is preserved in submit text");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const saveStarted = deferred<void>();
  const savePastedFile = deferred<string>();
  const sent: string[] = [];
  installBridgeApp({
    SavePastedFile: async () => {
      saveStarted.resolve();
      return savePastedFile.promise;
    },
  });
  const { root, rerender } = await renderComposer({
    onSend: (text) => {
      sent.push(text);
    },
  });
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
  await rerender({ insertRequest: { id: 11, text: "session B stays writable", mode: "replace" } });
  ok(sendButton().disabled === false, "session B is not blocked by session A's pending attachment");
  await act(async () => {
    sendButton().click();
    await flushTimers();
  });
  eq(sent.join(","), "session B stays writable", "session B can submit while session A attachment is pending");
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

{
  const dom = installDom();
  const submitStarted = deferred<void>();
  const releaseSubmit = deferred<void>();
  const sent: Array<{ tab: string; text: string }> = [];
  const { root, rerender } = await renderComposer({
    tabId: "tab-a",
    sessionKey: "session:project:/repo:topic-a:session-a",
    onSend: async (text, _submit, targetTabId) => {
      sent.push({ tab: targetTabId ?? "", text });
      submitStarted.resolve();
      await releaseSubmit.promise;
    },
  });

  await rerender({ insertRequest: { id: 12, text: "slow submit in A", mode: "replace" } });
  await act(async () => {
    sendButton().click();
    await submitStarted.promise;
  });

  await rerender({
    tabId: "tab-b",
    sessionKey: "session:project:/repo:topic-b:session-b",
    onSend: (text, _submit, targetTabId) => {
      sent.push({ tab: targetTabId ?? "", text });
    },
  });
  await rerender({ insertRequest: { id: 13, text: "fast submit in B", mode: "replace" } });
  ok(sendButton().disabled === false, "session B is not blocked by session A's in-flight submit");
  await act(async () => {
    sendButton().click();
    await flushTimers();
  });
  eq(sent[0]?.tab, "tab-a", "slow submit retains session A as its target");
  eq(sent[1]?.tab, "tab-b", "session B submit keeps its own target");

  await act(async () => {
    releaseSubmit.resolve();
    await flushTimers();
    root.unmount();
  });
  dom.window.close();
}

{
  // Clipboard menu actions await browser APIs. Their eventual mutation must
  // stay with the draft that owned the selection, even after a tab switch.
  const dom = installDom();
  const clipboardRead = deferred<string>();
  Object.defineProperty(window.navigator, "clipboard", {
    configurable: true,
    value: {
      read: async () => [],
      readText: () => clipboardRead.promise,
      writeText: async () => {},
    },
  });
  const { root, rerender } = await renderComposer({
    tabId: "tab-a",
    sessionKey: "session:project:/repo:topic-a:session-a",
  });
  await rerender({ insertRequest: { id: 20, text: "A:", mode: "replace" } });
  textarea().setSelectionRange(2, 2);
  const menuItems = await openComposerInputMenu();
  await act(async () => {
    menuItems[2]?.click();
    await flushTimers();
  });
  await rerender({
    tabId: "tab-b",
    sessionKey: "session:project:/repo:topic-b:session-b",
    insertRequest: { id: 21, text: "B stays clean", mode: "replace" },
  });
  await act(async () => {
    clipboardRead.resolve("pasted into A");
    await flushTimers();
  });
  eq(textarea().value, "B stays clean", "async clipboard paste does not mutate the switched-to session");
  await rerender({ tabId: "tab-a", sessionKey: "session:project:/repo:topic-a:session-a" });
  eq(textarea().value, "A:pasted into A", "async clipboard paste returns to its source draft");

  await act(async () => root.unmount());
  dom.window.close();
}

{
  const dom = installDom();
  const clipboardWrite = deferred<void>();
  const clipboardWriteStarted = deferred<void>();
  Object.defineProperty(window.navigator, "clipboard", {
    configurable: true,
    value: {
      read: async () => [],
      readText: async () => "",
      writeText: () => {
        clipboardWriteStarted.resolve();
        return clipboardWrite.promise;
      },
    },
  });
  const { root, rerender } = await renderComposer({
    tabId: "tab-a",
    sessionKey: "session:project:/repo:topic-a:session-a",
  });
  await rerender({ insertRequest: { id: 22, text: "abcdef", mode: "replace" } });
  await act(async () => {
    await new Promise((resolve) => window.requestAnimationFrame(() => resolve(null)));
    await flushTimers();
  });
  textarea().setSelectionRange(1, 4);
  const menuItems = await openComposerInputMenu();
  await act(async () => {
    menuItems[0]?.click();
    await clipboardWriteStarted.promise;
  });
  await rerender({
    tabId: "tab-b",
    sessionKey: "session:project:/repo:topic-b:session-b",
    insertRequest: { id: 23, text: "B is untouched", mode: "replace" },
  });
  await act(async () => {
    clipboardWrite.resolve();
    await flushTimers();
    await flushTimers();
  });
  eq(textarea().value, "B is untouched", "async cut does not delete text from the switched-to session");
  await rerender({ tabId: "tab-a", sessionKey: "session:project:/repo:topic-a:session-a" });
  eq(textarea().value, "aef", "async cut completes in its source draft");

  await act(async () => root.unmount());
  dom.window.close();
}

{
  const dom = installDom();
  invalidateCache();
  const historyPage = deferred<unknown>();
  installBridgeApp({ ScanPromptHistory: () => historyPage.promise });
  const { root, rerender } = await renderComposer({
    tabId: "tab-a",
    sessionKey: "session:project:/repo:topic-a:session-a",
  });
  await rerender({ insertRequest: { id: 24, text: "draft A", mode: "replace" } });
  textarea().setSelectionRange(0, 0);
  await act(async () => {
    textarea().dispatchEvent(new window.KeyboardEvent("keydown", { key: "ArrowUp", code: "ArrowUp", bubbles: true }));
    await flushTimers();
  });
  await rerender({
    tabId: "tab-b",
    sessionKey: "session:project:/repo:topic-b:session-b",
    insertRequest: { id: 25, text: "draft B", mode: "replace" },
  });
  await act(async () => {
    historyPage.resolve({
      entries: [{ text: "older prompt for A", at: 1, sessionPath: "/a.jsonl", turn: 0 }],
      nonce: "history-test",
      hasOlder: false,
    });
    await flushTimers();
  });
  eq(textarea().value, "draft B", "async prompt history does not overwrite the switched-to session");
  await rerender({ tabId: "tab-a", sessionKey: "session:project:/repo:topic-a:session-a" });
  eq(textarea().value, "older prompt for A", "async prompt history result returns to its source draft");

  await act(async () => root.unmount());
  dom.window.close();
}

{
  // Session-reference expansion awaits PreviewSession. A tab switch during
  // that await must not make A expand B's identically-labelled folded paste.
  const dom = installDom();
  const previewResult = deferred<Array<{ role: string; content: string }>>();
  let previewCalls = 0;
  const sent: Array<{ tab: string; submit: string }> = [];
  installBridgeApp({
    ListSessions: async () => [{
      path: "/history.jsonl",
      preview: "history",
      title: "History",
      turns: 1,
      createdAt: 1,
      lastActivityAt: 1,
      modTime: 1,
      current: false,
      open: false,
    }],
    PreviewSession: async () => {
      previewCalls += 1;
      return previewResult.promise;
    },
  });
  const { root, rerender } = await renderComposer({
    tabId: "tab-a",
    sessionKey: "session:project:/repo:topic-a:session-a",
    onSend: (_display, submit, targetTabId) => {
      sent.push({ tab: targetTabId ?? "", submit: submit ?? "" });
    },
  });
  const longA = Array.from({ length: 20 }, (_, index) => `A line ${index}`).join("\n");
  const longB = Array.from({ length: 20 }, (_, index) => `B line ${index}`).join("\n");
  await act(async () => {
    textarea().dispatchEvent(textPasteEvent(longA));
    await flushTimers();
  });
  textarea().setSelectionRange(textarea().value.length, textarea().value.length);
  await act(async () => {
    textarea().dispatchEvent(textPasteEvent(" @past:chats"));
    await flushTimers();
  });
  await act(async () => {
    textarea().dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", code: "Enter", bubbles: true }));
    await flushTimers();
  });
  await act(async () => {
    textarea().dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", code: "Enter", bubbles: true }));
    await flushTimers();
  });
  await act(async () => {
    sendButton().click();
    await flushTimers();
  });
  eq(previewCalls, 1, "source session reference starts asynchronous expansion");
  await rerender({
    tabId: "tab-b",
    sessionKey: "session:project:/repo:topic-b:session-b",
  });
  await act(async () => {
    textarea().dispatchEvent(textPasteEvent(longB));
    await flushTimers();
    previewResult.resolve([{ role: "user", content: "history context" }]);
    await flushTimers();
  });
  eq(sent[0]?.tab, "tab-a", "session-context submit retains its source tab");
  ok(sent[0]?.submit.includes("A line 19") === true, "session-context submit expands source folded paste");
  ok(sent[0]?.submit.includes("B line 19") === false, "session-context submit excludes switched-to folded paste");

  await act(async () => root.unmount());
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
