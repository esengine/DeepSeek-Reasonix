// Run: tsx src/__tests__/composer-inbox-recovery.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { Composer } from "../components/Composer";
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

function flushTimers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, check: () => boolean) {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    if (check()) return;
    await act(async () => { await flushTimers(); });
  }
  ok(false, label);
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
  globalThis.PointerEvent = dom.window.MouseEvent as unknown as typeof PointerEvent;
  globalThis.MutationObserver = dom.window.MutationObserver;
  globalThis.File = dom.window.File;
  globalThis.FileReader = dom.window.FileReader;
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
    collaborationMode: "normal" as CollaborationMode,
    toolApprovalMode: "ask" as ToolApprovalMode,
    tokenMode: "full" as TokenMode,
    goal: "",
    cwd: "/repo",
    modelLabel: "DeepSeek-R1",
    tabId: "tab-a",
    sessionKey: "session-a",
    onSend: () => {},
    onCancel: () => undefined,
    onCycleMode: () => {},
    onSetMode: () => {},
    onSetCollaborationMode: () => {},
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

function recoveredSnapshot(count = 3) {
  const items = Array.from({ length: count }, (_, index) => ({
    id: `recovered-${index + 1}`,
    intent: "followup",
    state: "uncertain",
    preview: `Recovered instruction ${index + 1}`,
    byteSize: 128,
    position: index + 1,
  }));
  return {
    revision: 1,
    paused: true,
    recovered: true,
    recoveredCount: count,
    items,
    itemsCount: items.length,
    bytes: items.length * 128,
    maxItems: 64,
    maxBytes: 64 * 1024 * 1024,
  };
}

console.log("\ncomposer inbox recovery");

{
  const dom = installDom();
  let snapshot = recoveredSnapshot();
  const pauseCalls: Array<{ tabId: string; paused: boolean }> = [];
  installBridgeApp({
    InboxSnapshot: async () => snapshot,
    SetInboxPaused: async (tabId: string, paused: boolean) => {
      pauseCalls.push({ tabId, paused });
      if (!paused) snapshot = { ...snapshot, paused: false, recovered: false };
    },
  });
  const { root } = await renderComposer();

  await waitFor("recovery banner rendered", () => document.querySelector(".composer-inbox-recovery") !== null);
  ok(document.querySelector(".composer-inbox-recovery")?.textContent?.includes("Recovered 3 pending instructions") === true, "banner reports recovered count");
  ok(document.querySelectorAll(".composer-guidance-item").length === 2, "queue starts with bounded preview");

  const review = document.querySelector(".composer-inbox-recovery .btn") as HTMLButtonElement;
  await act(async () => { review.click(); await flushTimers(); });
  ok(document.querySelectorAll(".composer-guidance-item").length === 3, "review action expands the recovered queue");

  const buttons = Array.from(document.querySelectorAll(".composer-inbox-recovery .btn")) as HTMLButtonElement[];
  await act(async () => { buttons[2].click(); await flushTimers(); });
  ok(pauseCalls.at(-1)?.paused === true, "keep paused confirms the server-side pause");
  ok(document.querySelector(".composer-inbox-recovery") !== null, "keep paused preserves the resume path");
  ok(buttons[2].textContent === "Paused" && buttons[2].disabled, "confirmed pause is visible and idempotent");

  await act(async () => { buttons[1].click(); await flushTimers(); });
  ok(pauseCalls.at(-1)?.paused === false, "continue resumes the durable inbox");
  await waitFor("recovery banner cleared after resume", () => document.querySelector(".composer-inbox-recovery") === null);
  ok(document.querySelector(".composer-inbox-recovery") === null, "successful resume clears the recovery banner");

  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom();
  let resolveTabA!: (value: ReturnType<typeof recoveredSnapshot>) => void;
  const tabAPromise = new Promise<ReturnType<typeof recoveredSnapshot>>((resolve) => { resolveTabA = resolve; });
  installBridgeApp({
    InboxSnapshot: async (tabId: string) => tabId === "tab-a"
      ? tabAPromise
      : { ...recoveredSnapshot(0), paused: false, recovered: false },
    SetInboxPaused: async () => {},
  });
  const { root, rerender } = await renderComposer();
  await rerender({ tabId: "tab-b", sessionKey: "session-b" });
  resolveTabA(recoveredSnapshot(2));
  await act(async () => { await flushTimers(); });
  ok(document.querySelector(".composer-inbox-recovery") === null, "stale snapshot cannot show recovery controls on another session");

  await act(async () => { root.unmount(); });
  dom.window.close();
}

if (failed > 0) {
  process.stderr.write(`\n${failed} failed, ${passed} passed\n`);
  process.exit(1);
}
process.stdout.write(`\n${passed} passed\n`);
