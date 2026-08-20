// Run: tsx src/__tests__/composer-ime-mid-caret.test.tsx
//
// Regression coverage for issue #8593: an unrelated React render while a
// plain Composer textarea is composing must not restore the pre-IME value.

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

function eq(actual: unknown, expected: unknown, label: string) {
  ok(actual === expected, actual === expected
    ? label
    : `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function flushTimers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function drainFrames(count = 2): Promise<void> {
  for (let i = 0; i < count; i += 1) {
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    await flushTimers();
  }
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
  globalThis.CompositionEvent = dom.window.CompositionEvent;
  globalThis.InputEvent = dom.window.InputEvent;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
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

function installBridgeApp() {
  (window as unknown as { go: { main: { App: Record<string, unknown> } } }).go = {
    main: {
      App: {
        Commands: async () => [],
        Models: async () => [],
        ModelsForTab: async () => [],
        ListDirForTab: async () => [],
        SearchFileRefsForTab: async () => [],
      },
    },
  };
}

async function renderComposer() {
  const rootElement = document.getElementById("root");
  if (!rootElement) throw new Error("missing root");
  const root = createRoot(rootElement);
  let props: Parameters<typeof Composer>[0] = {
    running: false,
    collaborationMode: "normal" as CollaborationMode,
    toolApprovalMode: "ask" as ToolApprovalMode,
    tokenMode: "full" as TokenMode,
    goal: "",
    cwd: "/repo",
    modelLabel: "DeepSeek-R1",
    imageInputEnabled: true,
    tabId: "ime-tab",
    sessionKey: "session:project:/repo:ime:session",
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
  };
  const rerender = async (next: Partial<Parameters<typeof Composer>[0]> = {}) => {
    props = { ...props, ...next };
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ToastProvider>
            <Composer {...props} />
          </ToastProvider>
        </LocaleProvider>,
      );
      await flushTimers();
    });
  };
  await rerender();
  return { root, rerender };
}

function textarea(): HTMLTextAreaElement {
  const node = document.querySelector("#composer-input");
  if (!(node instanceof HTMLTextAreaElement)) throw new Error("composer textarea did not render");
  return node;
}

async function runProgrammaticReplacementScenario() {
  const dom = installDom();
  installBridgeApp();
  const { root, rerender } = await renderComposer();
  await rerender({ insertRequest: { id: 85931, text: "hello world", mode: "replace" } });
  await act(async () => {
    await drainFrames();
  });

  const input = textarea();
  input.focus();
  input.setSelectionRange(5, 5);
  const setter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, "value")?.set;
  if (!setter) throw new Error("textarea value setter is unavailable");
  await act(async () => {
    input.dispatchEvent(new window.CompositionEvent("compositionstart", { bubbles: true, data: "" }));
    setter.call(input, "helloni world");
    input.setSelectionRange(7, 7);
    input.dispatchEvent(new window.InputEvent("input", {
      bubbles: true,
      data: "ni",
      inputType: "insertCompositionText",
      isComposing: true,
    }));
    await flushTimers();
  });

  await rerender({ insertRequest: { id: 85932, text: "external replacement", mode: "replace" } });
  eq(textarea().value, "external replacement", "a programmatic replacement cancels stale IME state");

  await act(async () => {
    await drainFrames();
    root.unmount();
  });
  dom.window.close();
}

async function runDraftSwitchScenario() {
  const dom = installDom();
  installBridgeApp();
  const { root, rerender } = await renderComposer();
  const firstSession = "session:project:/repo:ime:session";
  await rerender({ insertRequest: { id: 85933, text: "hello world", mode: "replace" } });
  await act(async () => {
    await drainFrames();
  });

  const input = textarea();
  input.focus();
  input.setSelectionRange(5, 5);
  const setter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, "value")?.set;
  if (!setter) throw new Error("textarea value setter is unavailable");
  await act(async () => {
    input.dispatchEvent(new window.CompositionEvent("compositionstart", { bubbles: true, data: "" }));
    await flushTimers();
  });
  await act(async () => {
    setter.call(input, "helloni world");
    input.setSelectionRange(7, 7);
    input.dispatchEvent(new window.InputEvent("input", {
      bubbles: true,
      data: "ni",
      inputType: "insertCompositionText",
      isComposing: true,
    }));
    await flushTimers();
  });

  await rerender({ sessionKey: "session:project:/repo:ime:other", insertRequest: null });
  eq(textarea().value, "", "switching drafts clears the old active IME value");
  await rerender({ sessionKey: firstSession, insertRequest: null });
  eq(textarea().value, "helloni world", "switching back restores the composing draft snapshot");

  await act(async () => {
    await drainFrames();
    root.unmount();
  });
  dom.window.close();
}

async function main() {
  const dom = installDom();
  installBridgeApp();
  const { root, rerender } = await renderComposer();
  await rerender({ insertRequest: { id: 8593, text: "hello world", mode: "replace" } });
  await act(async () => {
    await drainFrames();
  });

  const input = textarea();
  input.focus();
  input.setSelectionRange(5, 5);
  await act(async () => {
    input.dispatchEvent(new window.CompositionEvent("compositionstart", { bubbles: true, data: "" }));
    await flushTimers();
  });

  const setter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, "value")?.set;
  if (!setter) throw new Error("textarea value setter is unavailable");
  setter.call(input, "helloni world");
  input.setSelectionRange(7, 7);
  await act(async () => {
    input.dispatchEvent(new window.InputEvent("input", {
      bubbles: true,
      data: "ni",
      inputType: "insertCompositionText",
      isComposing: true,
    }));
    await flushTimers();
  });

  // This prop update stands in for the unrelated renders that occur in the
  // desktop while an IME candidate window is open.
  await rerender({ goal: "updated while composing" });
  eq(textarea().value, "helloni world", "an unrelated render preserves the composing DOM value");
  eq(textarea().selectionStart, 7, "an unrelated render preserves the IME caret");

  // Chromium/WebView2 can briefly expose the pre-composition value at
  // compositionend. The last native composition value must win that race.
  setter.call(input, "hello world");
  input.setSelectionRange(5, 5);
  await act(async () => {
    textarea().dispatchEvent(new window.CompositionEvent("compositionend", { bubbles: true, data: "你" }));
    await flushTimers();
  });
  await act(async () => {
    await flushTimers();
    setter.call(textarea(), "hello world");
    textarea().setSelectionRange(5, 5);
    textarea().dispatchEvent(new window.InputEvent("input", {
      bubbles: true,
      data: "你",
      inputType: "insertText",
      isComposing: false,
    }));
    await flushTimers();
  });
  eq(textarea().value, "helloni world", "a stale post-composition DOM echo does not overwrite the candidate");
  await act(async () => {
    setter.call(textarea(), "helloni world");
    textarea().setSelectionRange(7, 7);
    textarea().dispatchEvent(new window.InputEvent("input", {
      bubbles: true,
      data: "你",
      inputType: "insertText",
      isComposing: false,
    }));
    await flushTimers();
  });
  eq(textarea().value, "helloni world", "a delayed final input keeps the committed IME candidate");
  await act(async () => {
    await drainFrames();
  });
  await rerender({ goal: "after composition" });
  eq(textarea().value, "helloni world", "compositionend commits the native value to the controlled model");
  eq(textarea().selectionStart, 7, "compositionend restores the caret after the committed text");

  await act(async () => {
    await drainFrames();
    root.unmount();
  });
  dom.window.close();
  await runProgrammaticReplacementScenario();
  await runDraftSwitchScenario();

  if (failed > 0) {
    process.stderr.write(`\n${failed} assertion(s) failed\n`);
    process.exitCode = 1;
  } else {
    process.stdout.write(`\n${passed} assertions passed\n`);
  }
}

void main();
