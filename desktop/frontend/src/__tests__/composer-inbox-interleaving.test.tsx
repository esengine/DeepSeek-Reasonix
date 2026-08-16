// Run: tsx src/__tests__/composer-inbox-interleaving.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { Composer } from "../components/Composer";
import { LocaleProvider } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";
import type { CollaborationMode, ToolApprovalMode } from "../lib/types";

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
  globalThis.PointerEvent = dom.window.MouseEvent as unknown as typeof PointerEvent;
  globalThis.MutationObserver = dom.window.MutationObserver;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  globalThis.ResizeObserver = TestResizeObserver;
  Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
  Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });
  Object.defineProperty(dom.window.HTMLElement.prototype, "scrollIntoView", { configurable: true, value: () => {} });
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
  const rootElement = document.getElementById("root");
  if (!rootElement) throw new Error("missing root");
  const root = createRoot(rootElement);
  let currentProps: Parameters<typeof Composer>[0] = {
    running: false,
    collaborationMode: "normal",
    toolApprovalMode: "ask" as ToolApprovalMode,
    goal: "",
    cwd: "/repo",
    modelLabel: "DeepSeek-R1",
    tabId: "tab-a",
    sessionKey: "session:project:/repo:topic-a:session-a",
    onSend: () => {},
    onCancel: async () => ({ discardedItemIds: [] }),
    onCycleMode: () => {},
    onSetMode: () => {},
    onSetCollaborationMode: (_mode: CollaborationMode) => {},
    onSetToolApprovalMode: () => {},
    onToggleYoloApprovalMode: () => {},
    onClearGoal: () => {},
    onSwitchModel: () => {},
    onSetEffort: () => {},
    ready: true,
    ...props,
  };
  const paint = async (nextProps: Partial<Parameters<typeof Composer>[0]> = {}) => {
    const switchingDraft = nextProps.sessionKey !== undefined && nextProps.sessionKey !== currentProps.sessionKey;
    currentProps = {
      ...currentProps,
      ...(switchingDraft ? { insertRequest: null, selectedTextRequest: null } : {}),
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

console.log("\ncomposer inbox interleaving");

{
  // A fast tool boundary may consume a newly accepted steer before the Wails
  // enqueue promise resolves. The late receipt must not resurrect the chip.
  const dom = installDom();
  const enqueueStarted = deferred<void>();
  const enqueueReceipt = deferred<{ itemId: string; disposition: string; position: number; paused: boolean }>();
  installBridgeApp({
    EnqueueInboxSteer: async () => {
      enqueueStarted.resolve();
      return enqueueReceipt.promise;
    },
  });
  const { root, rerender } = await renderComposer({ running: true });
  await rerender({ insertRequest: { id: 7001, text: "consume before receipt", mode: "replace" } });
  const sendButton = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  if (!sendButton) throw new Error("running composer send button did not render for early consume");
  await act(async () => {
    sendButton.click();
    await enqueueStarted.promise;
  });
  await rerender({
    guidanceConsumedKey: "early-consume-event",
    guidanceConsumedItemId: "early-consumed-item",
    guidanceConsumedText: "consume before receipt",
  });
  await act(async () => {
    enqueueReceipt.resolve({ itemId: "early-consumed-item", disposition: "steer_accepted", position: 1, paused: false });
    await flushTimers();
  });
  ok(document.querySelector(".composer-guidance-item") === null, "an early consume event prevents the late enqueue receipt from resurrecting guidance");

  await act(async () => root.unmount());
  dom.window.close();
}

{
  // Stop settlement belongs to its source draft. A slow cancellation in A
  // must not disable or swallow a separate stop request after switching to B.
  const dom = installDom();
  installBridgeApp({});
  const cancelAStarted = deferred<void>();
  const cancelAReceipt = deferred<{ discardedItemIds: string[] }>();
  const cancelledTabs: string[] = [];
  const { root, rerender } = await renderComposer({
    running: true,
    onCancel: () => {
      cancelledTabs.push("tab-a");
      cancelAStarted.resolve();
      return cancelAReceipt.promise;
    },
  });
  let stopButton = document.querySelector(".composer__btn--stop") as HTMLButtonElement | null;
  if (!stopButton) throw new Error("session A stop button did not render");
  await act(async () => {
    stopButton?.click();
    await cancelAStarted.promise;
  });

  await rerender({
    running: true,
    tabId: "tab-b",
    sessionKey: "session:project:/repo:topic-b:session-b",
    onCancel: async () => {
      cancelledTabs.push("tab-b");
      return { discardedItemIds: [] };
    },
  });
  stopButton = document.querySelector(".composer__btn--stop") as HTMLButtonElement | null;
  if (!stopButton) throw new Error("session B stop button did not render");
  ok(stopButton.disabled === false, "session B stop stays enabled while session A cancellation settles");
  await act(async () => {
    stopButton?.click();
    await flushTimers();
  });
  eq(cancelledTabs.join(","), "tab-a,tab-b", "session B can issue its own stop while session A cancellation settles");

  await act(async () => {
    cancelAReceipt.resolve({ discardedItemIds: [] });
    await flushTimers();
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
