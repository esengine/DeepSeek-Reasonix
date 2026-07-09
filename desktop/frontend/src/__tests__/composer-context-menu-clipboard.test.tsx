// Run: tsx src/__tests__/composer-context-menu-clipboard.test.tsx
//
// Regression coverage for the composer edit context menu's clipboard guards:
// - Cut must not delete the selection when every clipboard path failed.
// - Paste must not replace the selection when the clipboard has no text.
// - Shortcut hints must use the platform modifier (Ctrl outside macOS).

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
    go: undefined,
    main: {
      App: {
        Commands: async () => [],
        Models: async () => [],
        ModelsForTab: async () => [],
        ...methods,
      },
    },
  } as never;
}

async function renderComposer(props: Partial<Parameters<typeof Composer>[0]> = {}) {
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const currentProps: Parameters<typeof Composer>[0] = {
    running: false,
    collaborationMode: "normal",
    toolApprovalMode: "ask" as ToolApprovalMode,
    tokenMode: "full" as TokenMode,
    goal: "",
    cwd: "/repo",
    modelLabel: "DeepSeek-R1",
    imageInputEnabled: true,
    tabId: "context-menu-tab",
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
  await act(async () => {
    root.render(
      <LocaleProvider>
        <ToastProvider>
          <div className="chat-pane">
            <Composer {...currentProps} />
          </div>
        </ToastProvider>
      </LocaleProvider>,
    );
    await flushTimers();
  });
  return { root };
}

function textarea(): HTMLTextAreaElement {
  const node = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!node) throw new Error("composer textarea did not render");
  return node;
}

// textPasteEvent seeds composer text through its own paste handler — the same
// pattern composer-session-draft.test.tsx uses — because the controlled
// textarea's state is not reachable through synthetic input events here.
function textPasteEvent(text: string): Event {
  const event = new window.Event("paste", { bubbles: true, cancelable: true });
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

async function typeAndSelect(value: string, from: number, to: number) {
  const node = textarea();
  await act(async () => {
    node.focus();
    node.setSelectionRange(0, node.value.length);
    node.dispatchEvent(textPasteEvent(value));
    await flushTimers();
  });
  if (textarea().value !== value) throw new Error(`composer text = ${JSON.stringify(textarea().value)}, want ${JSON.stringify(value)}`);
  // Drain the paste handler's deferred focusInputRange (rAF + timers) before
  // pinning the selection under test, or it would reset the caret afterwards.
  await act(async () => {
    await new Promise((resolve) => window.requestAnimationFrame(() => resolve(null)));
    await flushTimers();
  });
  textarea().focus();
  textarea().setSelectionRange(from, to);
}

async function openInputMenu(): Promise<HTMLButtonElement[]> {
  await act(async () => {
    textarea().dispatchEvent(
      new window.MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 20, clientY: 20 }),
    );
    await flushTimers();
  });
  const items = Array.from(document.querySelectorAll(".context-menu__item")) as HTMLButtonElement[];
  if (items.length !== 4) throw new Error(`expected 4 edit menu items, got ${items.length}`);
  return items;
}

async function clickMenuItem(item: HTMLButtonElement) {
  await act(async () => {
    item.dispatchEvent(new window.MouseEvent("click", { bubbles: true, cancelable: true }));
    await flushTimers();
    await flushTimers();
  });
}

function stubClipboard(overrides: Partial<{ writeText: unknown; readText: unknown; read: unknown }>) {
  Object.defineProperty(window.navigator, "clipboard", {
    configurable: true,
    value: {
      writeText: () => Promise.reject(new Error("clipboard denied")),
      readText: () => Promise.resolve(""),
      read: () => Promise.reject(new Error("clipboard.read unsupported")),
      ...overrides,
    },
  });
}

async function main() {
  installDom();
  installBridgeApp({
    // The empty-paste path probes the native clipboard for an image; a reject
    // must stay silent (notifyOnError=false) and never touch the draft text.
    SaveClipboardImage: async () => {
      throw new Error("no native clipboard image");
    },
  });

  // JSDOM reports a non-mac platform, so the menu must advertise Ctrl, not ⌘.
  await renderComposer();

  // --- shortcut hints use the platform modifier ---
  {
    await typeAndSelect("hello world", 0, 5);
    const items = await openInputMenu();
    const hints = items.map((item) => item.querySelector(".context-menu__shortcut")?.textContent ?? "");
    eq(hints[0], "Ctrl+X", "cut hint uses the platform modifier");
    eq(hints[2], "Ctrl+V", "paste hint uses the platform modifier");
    ok(hints.every((hint) => !hint.includes("⌘")), "no hardcoded mac glyph on a non-mac platform");
    // Close the menu without acting.
    await act(async () => {
      document.body.dispatchEvent(new window.MouseEvent("pointerdown", { bubbles: true }));
      await flushTimers();
    });
  }

  // --- cut keeps the draft when every clipboard path fails ---
  {
    stubClipboard({});
    (document as Document & { execCommand?: () => boolean }).execCommand = () => false;
    await typeAndSelect("hello world", 0, 5);
    const items = await openInputMenu();
    await clickMenuItem(items[0]);
    eq(textarea().value, "hello world", "failed cut must not delete the selection");
  }

  // --- cut removes the selection once a clipboard path succeeds ---
  {
    stubClipboard({ writeText: () => Promise.resolve() });
    await typeAndSelect("hello world", 0, 6);
    const items = await openInputMenu();
    await clickMenuItem(items[0]);
    eq(textarea().value, "world", "successful cut removes the selected text");
  }

  // --- paste with an empty clipboard keeps the selection ---
  {
    stubClipboard({ readText: () => Promise.resolve("") });
    await typeAndSelect("hello world", 0, 5);
    const items = await openInputMenu();
    await clickMenuItem(items[2]);
    eq(textarea().value, "hello world", "empty-clipboard paste must not erase the selection");
  }

  // --- paste with text replaces the selection ---
  {
    stubClipboard({ readText: () => Promise.resolve("bye") });
    await typeAndSelect("hello world", 0, 5);
    const items = await openInputMenu();
    await clickMenuItem(items[2]);
    eq(textarea().value, "bye world", "text paste replaces the selection");
  }

  process.stdout.write(`\n${passed} passed, ${failed} failed, ${passed + failed} total\n`);
  if (failed > 0) process.exit(1);
}

void main();
