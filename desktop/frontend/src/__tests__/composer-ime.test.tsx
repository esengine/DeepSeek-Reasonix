// Run: tsx src/__tests__/composer-ime.test.tsx
//
// The plain composer textarea must survive CJK IME composition (#8593/#8409).
// While a composition is active the textarea renders uncontrolled so no
// unrelated re-render (autosize, run-strip ticker, selection tracking) can
// write node.value and cancel the in-flight composition; compositionend is
// the authoritative resync point, and a programmatic setText landing
// mid-composition forces a resync instead of being swallowed by the freeze.
//
// jsdom note: these suites install the DOM after react-dom has loaded, so
// React boots with canUseDOM=false and falls back to its keyup-driven
// change-detection polyfill — the input event feeds onInputCapture
// (inputType) while the trailing keyup is what delivers onChange. The
// composition listeners in the IME guard hook are native, so plain
// compositionstart/compositionend Events reach them directly.

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

async function renderComposer(props: Partial<Parameters<typeof Composer>[0]> = {}) {
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const sent: string[] = [];
  let currentProps: Parameters<typeof Composer>[0] = {
    running: false,
    collaborationMode: "normal" as CollaborationMode,
    toolApprovalMode: "ask" as ToolApprovalMode,
    goal: "",
    cwd: "/repo",
    modelLabel: "DeepSeek-R1",
    onSend: (text) => {
      sent.push(text);
    },
    onCancel: () => {},
    onCycleMode: () => {},
    onSetMode: () => {},
    onSetCollaborationMode: () => {},
    onSetToolApprovalMode: () => {},
    onToggleYoloApprovalMode: () => {},
    onClearGoal: () => {},
    onSwitchModel: () => {},
    onSetEffort: () => {},
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
  return { root, sent, rerender: paint };
}

// React's value tracker only reports a change (and fires onChange) when the
// DOM value moved outside its patched setter, so IME mutations must go
// through the native prototype setter — exactly how the browser itself
// applies provisional and committed composition text.
function setNativeValue(ta: HTMLTextAreaElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, "value")?.set;
  if (!setter) throw new Error("native textarea value setter unavailable");
  setter.call(ta, value);
}

function dispatchInput(ta: HTMLTextAreaElement, data: string, inputType: string, isComposing: boolean) {
  ta.dispatchEvent(new window.InputEvent("input", {
    bubbles: true,
    data,
    inputType,
    isComposing,
  }));
  // The change-detection polyfill delivers onChange on keyup.
  ta.dispatchEvent(new window.KeyboardEvent("keyup", { key: "Process", bubbles: true }));
}

function pressEnter(ta: HTMLTextAreaElement) {
  ta.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
}

function composerTextarea(): HTMLTextAreaElement {
  const ta = document.querySelector<HTMLTextAreaElement>("#composer-input");
  if (!ta) throw new Error("plain composer textarea did not render");
  return ta;
}

console.log("\ncomposer plain textarea IME freeze");

// compositionstart → provisional DOM text → unrelated re-render → the
// provisional text must survive (pre-fix, React's updateTextarea compares
// the controlled value against the live DOM value on every commit and
// writes the stale state back, cancelling the composition — the swallowed
// first keystroke). Commit then resyncs exactly once and leaves a sane caret.
{
  const dom = installDom();
  const { root, sent, rerender } = await renderComposer();
  const ta = composerTextarea();

  await act(async () => {
    ta.focus();
    ta.dispatchEvent(new Event("compositionstart", { bubbles: true }));
    // WebView2 ordering: the provisional character is already in the DOM
    // before the first input event reaches React.
    setNativeValue(ta, "n");
    await flushTimers();
  });

  // An unrelated re-render (parent prop change, run-strip tick) lands while
  // the composition is in flight and no input event has fired yet.
  await rerender({ modelLabel: "DeepSeek-V4" });
  eq(ta.value, "n", "unrelated re-render mid-composition keeps the provisional text");

  await act(async () => {
    setNativeValue(ta, "ni");
    dispatchInput(ta, "i", "insertCompositionText", true);
    await flushTimers();
  });
  eq(ta.value, "ni", "composition input keeps flowing without a DOM clobber");

  await act(async () => {
    pressEnter(ta);
    await flushTimers();
  });
  eq(sent.length, 0, "Enter during composition does not send");

  await act(async () => {
    // Commit: the IME replaces the provisional run, fires the commit input
    // (Chromium fires it before compositionend) and then compositionend.
    setNativeValue(ta, "你");
    ta.setSelectionRange(1, 1);
    dispatchInput(ta, "你", "insertCompositionText", false);
    ta.dispatchEvent(new Event("compositionend", { bubbles: true }));
    await flushTimers();
  });
  eq(ta.value, "你", "committed IME text is intact after compositionend");
  eq(ta.selectionStart, 1, "caret sits after the committed character");

  await act(async () => {
    pressEnter(ta);
    await flushTimers();
  });
  eq(sent.length, 0, "Enter inside the post-composition grace window does not send");

  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 150));
    pressEnter(ta);
    await flushTimers();
  });
  eq(sent.length, 1, "Enter after the grace window sends");
  eq(sent[0], "你", "the sent text is the committed IME text");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// IME cancel: compositionstart, provisional text, then compositionend with
// the DOM restored to the pre-composition value (no commit input). State
// must follow the authoritative DOM, and typing afterwards must keep working.
{
  const dom = installDom();
  const { root } = await renderComposer();
  const ta = composerTextarea();

  await act(async () => {
    ta.focus();
    ta.dispatchEvent(new Event("compositionstart", { bubbles: true }));
    setNativeValue(ta, "tmp");
    dispatchInput(ta, "p", "insertCompositionText", true);
    await flushTimers();
  });
  eq(ta.value, "tmp", "provisional text is live during composition");

  await act(async () => {
    setNativeValue(ta, "");
    ta.dispatchEvent(new Event("compositionend", { bubbles: true }));
    await flushTimers();
  });
  eq(ta.value, "", "IME cancel restores the pre-composition text");

  await act(async () => {
    setNativeValue(ta, "x");
    ta.setSelectionRange(1, 1);
    dispatchInput(ta, "x", "insertText", false);
    await flushTimers();
  });
  eq(ta.value, "x", "typing after an IME cancel keeps working");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Programmatic setText mid-composition (history recall, menu inserts, draft
// switches) bypasses the textarea's onChange, so it must force an
// authoritative resync through the freeze instead of being swallowed.
{
  const dom = installDom();
  const { root, sent, rerender } = await renderComposer();
  const ta = composerTextarea();

  await act(async () => {
    ta.focus();
    ta.dispatchEvent(new Event("compositionstart", { bubbles: true }));
    setNativeValue(ta, "n");
    ta.setSelectionRange(1, 1);
    dispatchInput(ta, "n", "insertCompositionText", true);
    await flushTimers();
  });
  eq(ta.value, "n", "composition is in flight before the programmatic insert");

  await rerender({ insertRequest: { id: 7, text: "snippet body" } });
  eq(
    ta.value,
    "n\n\nsnippet body\n",
    "programmatic insert forces a resync and reaches the DOM mid-composition",
  );

  await act(async () => {
    pressEnter(ta);
    await flushTimers();
  });
  eq(sent.length, 1, "the programmatic resync ends the freeze so Enter sends");
  ok(sent[0]?.includes("snippet body") === true, "the sent text carries the programmatic insert");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
