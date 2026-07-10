// Run: tsx src/__tests__/btw-panel.test.tsx

import { JSDOM } from "jsdom";
import { registerHooks } from "node:module";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { AppBindings } from "../lib/bridge";
import { LocaleProvider } from "../lib/i18n";
import type { BtwStateView } from "../lib/types";

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.endsWith(".svg")) {
      return nextResolve("./asset-stub-for-tests.ts", { ...context, parentURL: import.meta.url });
    }
    return nextResolve(specifier, context);
  },
});

type BtwPanelProps = Parameters<typeof import("../components/BtwPanel").BtwPanel>[0];

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
  ok(actual === expected, actual === expected ? label : `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function flushTimers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
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
  globalThis.CompositionEvent = dom.window.CompositionEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.MutationObserver = dom.window.MutationObserver;
  globalThis.ResizeObserver = TestResizeObserver;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
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

function mockApp({
  state,
  onStart,
  onSubmit,
  onReturn,
}: {
  state: () => Promise<BtwStateView>;
  onStart?: (tabId: string, input: string) => Promise<void>;
  onSubmit?: (tabId: string, input: string) => Promise<void>;
  onReturn?: (tabId: string) => Promise<void>;
}) {
  window.go = {
    main: {
      App: {
        BtwStateForTab: state,
        StartBtwForTab: onStart ?? (async () => {}),
        SubmitBtwForTab: onSubmit ?? (async () => {}),
        ReturnFromBtwForTab: onReturn ?? (async () => {}),
      } as Partial<AppBindings> as AppBindings,
    },
  };
}

async function renderPanel(initial: Partial<BtwPanelProps> = {}) {
  const { BtwPanel } = await import("../components/BtwPanel");
  const rootElement = document.getElementById("root");
  if (!rootElement) throw new Error("missing root");
  const root = createRoot(rootElement);
  let props: BtwPanelProps = {
    tabId: "tab-1",
    sessionKey: "session-a",
    tabSessionKeys: { "tab-1": "session-a" },
    ready: true,
    hasParentContext: true,
    modelLabel: "test-model",
    visible: true,
    onHide: () => {},
    onEnd: () => {},
    ...initial,
  };
  const rerender = async (next: Partial<BtwPanelProps>) => {
    props = { ...props, ...next };
    await act(async () => {
      root.render(<LocaleProvider><BtwPanel {...props} /></LocaleProvider>);
      await flushTimers();
    });
  };
  await rerender({});
  return { root, rerender };
}

async function clickSuggestion(index: number) {
  const button = document.querySelectorAll(".btw-panel__suggestions button")[index] as HTMLButtonElement | undefined;
  if (!button) throw new Error(`BTW suggestion ${index} did not render`);
  await act(async () => {
    button.click();
    await flushTimers();
  });
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    await act(async () => { await flushTimers(); });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

console.log("\nBTW panel interactions");

{
  const dom = installDom();
  mockApp({ state: async () => ({ active: false, running: false, cancelRequested: false, cancellable: false }) });
  const { root, rerender } = await renderPanel();
  let textarea = document.querySelector(".btw-panel textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("BTW textarea did not render");

  await clickSuggestion(0);
  const draftA = textarea.value;
  await rerender({
    sessionKey: "session-a-path",
    tabSessionKeys: { "tab-1": "session-a-path" },
    sessionKeyMigrations: [{ from: "session-a", to: "session-a-path" }],
  });
  textarea = document.querySelector(".btw-panel textarea") as HTMLTextAreaElement | null;
  eq(textarea?.value, draftA, "a fallback key upgrade preserves the current BTW draft");

  await rerender({
    sessionKey: "session-b",
    tabSessionKeys: { "tab-1": "session-b" },
    sessionKeyInvalidations: [{ from: "session-a-path", to: "session-b" }],
  });
  textarea = document.querySelector(".btw-panel textarea") as HTMLTextAreaElement | null;
  eq(textarea?.value, "", "a new session starts with its own empty draft");

  if (!textarea) throw new Error("BTW textarea missing after session switch");
  await clickSuggestion(1);
  await rerender({ sessionKey: "session-a-path", tabSessionKeys: { "tab-1": "session-a-path" }, sessionKeyMigrations: [] });
  textarea = document.querySelector(".btw-panel textarea") as HTMLTextAreaElement | null;
  eq(textarea?.value, draftA, "switching back restores the session-scoped draft");
  ok(document.activeElement === textarea, "visible BTW focuses its composer after a session switch");

  await act(async () => root.unmount());
  dom.window.close();
}

{
  const dom = installDom();
  let resolveOldStart: (() => void) | undefined;
  let returns = 0;
  let starts = 0;
  const oldStart = new Promise<void>((resolve) => {
    resolveOldStart = resolve;
  });
  mockApp({
    state: async () => ({ active: false, running: false, cancelRequested: false, cancellable: false }),
    onStart: async () => {
      starts += 1;
      if (starts === 1) await oldStart;
    },
    onReturn: async () => { returns += 1; },
  });
  const { root, rerender } = await renderPanel();
  await clickSuggestion(0);
  let sendButton = document.querySelector(".btw-panel__send-btn") as HTMLButtonElement | null;
  if (!sendButton || !resolveOldStart) throw new Error("stale start test did not initialize");
  await act(async () => {
    sendButton?.click();
    await flushTimers();
  });
  await rerender({
    sessionKey: "session-b",
    tabSessionKeys: { "tab-1": "session-b" },
    sessionKeyInvalidations: [{ from: "session-a", to: "session-b" }],
  });
  await clickSuggestion(1);
  sendButton = document.querySelector(".btw-panel__send-btn") as HTMLButtonElement | null;
  await act(async () => {
    sendButton?.click();
    await flushTimers();
  });
  await act(async () => {
    resolveOldStart?.();
    await flushTimers();
  });
  eq(returns, 0, "a stale Start promise cannot return from the new session's BTW runtime");

  await act(async () => root.unmount());
  dom.window.close();
}

{
  const dom = installDom();
  let backendState: BtwStateView = { active: true, running: true, cancelRequested: false, cancellable: true };
  mockApp({ state: async () => backendState });
  const { root, rerender } = await renderPanel();
  await waitFor("running session before invalidation", () => document.querySelector(".btw-panel__run-strip") !== null);
  backendState = { active: false, running: false, cancelRequested: false, cancellable: false };
  await rerender({
    sessionKey: "session-b",
    tabSessionKeys: { "tab-1": "session-b" },
    sessionKeyInvalidations: [{ from: "session-a", to: "session-b" }],
  });
  await rerender({
    sessionKey: "session-a",
    tabSessionKeys: { "tab-1": "session-a" },
    sessionOpen: false,
    sessionKeyInvalidations: [],
  });
  ok(document.querySelector(".btw-panel__run-strip") === null, "a session rotation clears the old session's cached running state");

  await act(async () => root.unmount());
  dom.window.close();
}

{
  const dom = installDom();
  let starts = 0;
  mockApp({
    state: async () => ({ active: false, running: false, cancelRequested: false, cancellable: false }),
    onStart: async () => { starts += 1; },
  });
  const { root } = await renderPanel();
  const textarea = document.querySelector(".btw-panel textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("BTW textarea did not render");
  await clickSuggestion(0);
  await act(async () => {
    const event = new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true });
    Object.defineProperty(event, "isComposing", { configurable: true, value: true });
    textarea.dispatchEvent(event);
    await flushTimers();
  });
  eq(starts, 0, "IME candidate confirmation does not submit BTW");

  await act(async () => root.unmount());
  dom.window.close();
}

{
  const dom = installDom();
  let starts = 0;
  let submits = 0;
  const handled: number[] = [];
  mockApp({
    state: async () => ({ active: false, running: false, cancelRequested: false, cancellable: false }),
    onStart: async () => { starts += 1; },
    onSubmit: async () => { submits += 1; },
  });
  const { root, rerender } = await renderPanel({ onStartRequestHandled: (id) => handled.push(id) });
  await clickSuggestion(0);
  const sendButton = document.querySelector(".btw-panel__send-btn") as HTMLButtonElement | null;
  if (!sendButton) throw new Error("BTW send button did not render");
  await act(async () => {
    sendButton.click();
    await flushTimers();
  });
  await waitFor("BTW running state", () => document.querySelector(".btw-panel__run-strip") !== null);
  starts = 0;
  submits = 0;
  await rerender({ startRequest: { id: 7, input: "keep this for later" } });
  const textarea = document.querySelector(".btw-panel textarea") as HTMLTextAreaElement | null;
  eq(textarea?.value, "keep this for later", "a command received while BTW runs is staged in the draft");
  eq(handled.join(","), "7", "the staged command is acknowledged once");
  eq(starts + submits, 0, "a running BTW does not dispatch a duplicate turn");

  await act(async () => root.unmount());
  dom.window.close();
}

{
  const dom = installDom();
  let emitBtw: ((payload: unknown) => void) | undefined;
  let rejectSubmit: ((error: Error) => void) | undefined;
  const pendingSubmit = new Promise<void>((_resolve, reject) => {
    rejectSubmit = reject;
  });
  mockApp({
    state: async () => ({ active: true, running: false, cancelRequested: false, cancellable: false }),
    onSubmit: async () => pendingSubmit,
  });
  window.runtime = {
    EventsOn(name, callback) {
      if (name === "agent:side-event") emitBtw = (payload) => callback(payload);
      return () => {};
    },
    BrowserOpenURL() {},
  };
  const { root } = await renderPanel();
  await waitFor("active BTW runtime", () => document.querySelector(".btw-panel__status")?.textContent?.includes("ready for a follow-up") === true);
  await clickSuggestion(0);
  const submittedPrompt = (document.querySelector(".btw-panel textarea") as HTMLTextAreaElement | null)?.value ?? "";
  const sendButton = document.querySelector(".btw-panel__send-btn") as HTMLButtonElement | null;
  if (!sendButton || !emitBtw || !rejectSubmit) throw new Error("BTW idle race test did not initialize");
  await act(async () => {
    sendButton.click();
    await flushTimers();
  });
  await waitFor("optimistic BTW running state", () => document.querySelector(".btw-panel__run-strip") !== null);
  await act(async () => {
    emitBtw?.({
      kind: "notice",
      text: "BTW side conversation closed after being idle.",
      tabId: "tab-1",
    });
    await flushTimers();
  });
  ok(document.querySelector(".btw-panel__run-strip") === null, "an idle timeout clears optimistic running state immediately");
  ok(document.querySelector(".btw-panel__notice") !== null, "an idle timeout explains that the temporary conversation expired");
  eq((document.querySelector(".btw-panel textarea") as HTMLTextAreaElement | null)?.value, submittedPrompt, "an idle timeout restores the prompt whose submit was not confirmed");
  await act(async () => {
    rejectSubmit?.(new Error("late submit failure"));
    await flushTimers();
  });
  ok(document.querySelector(".btw-panel__run-strip") === null, "a late submit failure cannot revive an expired BTW runtime");
  eq((document.querySelector(".btw-panel textarea") as HTMLTextAreaElement | null)?.value, submittedPrompt, "a late submit failure keeps the restored prompt available");

  await act(async () => root.unmount());
  dom.window.close();
}

if (failed > 0) {
  process.stderr.write(`\n${failed} failed, ${passed} passed\n`);
  process.exit(1);
}
process.stdout.write(`\n${passed} passed\n`);
