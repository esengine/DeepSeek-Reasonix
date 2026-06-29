// Run: tsx src/__tests__/composer-image-capability.test.tsx

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

async function waitFor(check: () => boolean, attempts = 10): Promise<void> {
  for (let i = 0; i < attempts; i++) {
    if (check()) return;
    await act(async () => {
      await flushTimers();
    });
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
    imageInputEnabled: true,
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

function sendButton(): HTMLButtonElement {
  const node = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  if (!node) throw new Error("send button did not render");
  return node;
}

function contextItemCount(): number {
  return document.querySelectorAll(".composer-context__item").length;
}

function toastText(): string {
  const items = Array.from(document.querySelectorAll(".toast__text"));
  return (items.at(-1)?.textContent ?? "").trim();
}

function imagePasteEvent(file: File): Event {
  const event = new Event("paste", { bubbles: true, cancelable: true });
  Object.defineProperty(event, "clipboardData", {
    configurable: true,
    value: {
      files: [file],
      items: [],
      types: [file.type],
      getData: () => "",
    },
  });
  return event;
}

console.log("\ncomposer image capability");

{
  const dom = installDom();
  let saveCalls = 0;
  installBridgeApp({
    SavePastedImage: async () => {
      saveCalls += 1;
      return ".reasonix/attachments/mock.png";
    },
    AttachmentDataURL: async () => "data:image/png;base64,iVBORw0KGgo=",
  });
  const { root } = await renderComposer({ imageInputEnabled: false });
  const file = new File(["img"], "photo.png", { type: "image/png", lastModified: 1 });

  await act(async () => {
    textarea().dispatchEvent(imagePasteEvent(file));
    await flushTimers();
    await flushTimers();
  });
  await waitFor(() => contextItemCount() === 1);

  eq(saveCalls, 1, "text-only model stores pasted image attachments as tool-readable refs");
  eq(contextItemCount(), 1, "text-only model keeps the pasted image attachment in the draft");
  eq(toastText(), "", "text-only image attach does not warn before send");
  eq(document.querySelector(".composer__prompt") === null, true, "image attach warning does not render inside the composer layout");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const sent: Array<{ display: string; submit?: string }> = [];
  installBridgeApp({
    SavePastedImage: async () => ".reasonix/attachments/mock.png",
    AttachmentDataURL: async () => "data:image/png;base64,iVBORw0KGgo=",
  });
  const { root, rerender } = await renderComposer({
    imageInputEnabled: true,
    onSend: (display, submit) => sent.push({ display, submit }),
  });
  const file = new File(["img"], "photo.png", { type: "image/png", lastModified: 1 });

  await act(async () => {
    textarea().dispatchEvent(imagePasteEvent(file));
    await flushTimers();
    await flushTimers();
  });
  await waitFor(() => contextItemCount() === 1);
  eq(contextItemCount(), 1, "vision-capable model keeps the pasted image attachment");

  await rerender({ imageInputEnabled: false, insertRequest: { id: 1, text: "describe this image", mode: "insert" } });
  eq(toastText(), "", "model switch alone does not show a warning toast");
  await act(async () => {
    sendButton().click();
    await flushTimers();
  });

  eq(sent.length, 1, "switching to a text-only model still sends the image ref for tool use");
  ok(toastText().includes("will not receive images directly"), "text-only send warns about direct image input without blocking");
  eq(document.querySelector(".composer__prompt") === null, true, "image-input warning does not render inside the composer layout");
  ok(sent[0]?.submit?.includes("@.reasonix/attachments/mock.png") === true, "submitted text retains the local image attachment ref");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
