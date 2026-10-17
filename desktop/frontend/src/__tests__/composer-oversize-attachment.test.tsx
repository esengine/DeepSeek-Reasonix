// Run: tsx src/__tests__/composer-oversize-attachment.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { Composer } from "../components/Composer";
import { LocaleProvider } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";
import type { CollaborationMode, ToolApprovalMode } from "../lib/types";
import { installDesktopHostStub, type DesktopHostStubOptions } from "./desktopHostStub";

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

function installBridgeApp(methods: Record<string, unknown>, options?: DesktopHostStubOptions) {
	const legacySave = methods.SavePastedImageForTarget as ((token: string, dataURL: string) => Promise<string>) | undefined;
	const legacyPreview = methods.AttachmentDataURLForTarget as ((token: string, path: string) => Promise<string>) | undefined;
	const unscopedPreview = methods.AttachmentDataURL as ((path: string) => Promise<string>) | undefined;
  return installDesktopHostStub({
    Commands: async () => [],
    Models: async () => [],
    ModelsForTab: async () => [],
		CaptureAttachmentTarget: async () => ({ token: "test-attachment-target", capabilities: ["attachments-v2"] }),
		ReleaseAttachmentTarget: async () => {},
		StageImageForTarget: async (token: string, _operationID: string, displayName: string, mime: string, dataURL: string) => ({
			draftId: "",
			path: legacySave ? await legacySave(token, dataURL) : ".reasonix/attachments/mock.png",
			displayName,
			mime,
			width: 1,
			height: 1,
			bytes: dataURL.length,
		}),
		ReadDraftImageForTarget: async () => "data:image/png;base64,iVBORw0KGgo=",
		AttachmentDataURLForTarget: legacyPreview ?? (async () => "data:image/png;base64,iVBORw0KGgo="),
		AttachmentDataURLForTab: async (_tabID: string, path: string) => unscopedPreview ? unscopedPreview(path) : "data:image/png;base64,iVBORw0KGgo=",
    ...methods,
  }, options);
}

async function renderComposer(props: Partial<Parameters<typeof Composer>[0]> = {}) {
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  let currentProps: Parameters<typeof Composer>[0] = {
    running: false,
    collaborationMode: "normal",
    toolApprovalMode: "ask" as ToolApprovalMode,

    goal: "",
    cwd: "/repo",
    modelLabel: "DeepSeek-R1",
    imageInputEnabled: true,
    tabId: "single-surface-tab",
    sessionKey: "session:project:/repo:topic-a:session-a",
    onSend: () => {},
    onCancel: async () => ({ discardedItemIds: [] }),
    onCycleMode: () => {},
    onSetMode: () => {},
    onSetCollaborationMode: (_mode: CollaborationMode) => {},
    onSetToolApprovalMode: () => {},
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
            <div className="chat-pane">
              <Composer {...currentProps} />
            </div>
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



function pasteEvent(file: File): Event {
  const event = new Event("paste", { bubbles: true, cancelable: true });
  Object.defineProperty(event, "clipboardData", {
    configurable: true,
    value: { files: [file], items: [], types: [file.type], getData: () => "" },
  });
  return event;
}

function oversized(name: string, type: string, bytes: number): File {
  const file = new File(["x"], name, { type, lastModified: 1 });
  Object.defineProperty(file, "size", { configurable: true, value: bytes });
  return file;
}

console.log("\ncomposer oversize attachment");

for (const [name, type, bytes] of [
  ["huge.png", "image/png", 64 * 1024 * 1024 + 1],
] as const) {
  const dom = installDom();
  let reads = 0;
  let digests = 0;
  let saves = 0;
  const origRead = dom.window.FileReader.prototype.readAsDataURL;
  dom.window.FileReader.prototype.readAsDataURL = function (this: FileReader, blob: Blob) {
    reads += 1;
    return origRead.call(this, blob);
  };
  Object.defineProperty(globalThis, "crypto", {
    configurable: true,
    value: { randomUUID: () => "id", subtle: { digest: async () => { digests += 1; return new ArrayBuffer(32); } } },
  });
  installBridgeApp({
    SavePastedFileForTarget: async () => { saves += 1; return ".reasonix/attachments/x.bin"; },
    StageImageForTarget: async () => { saves += 1; throw new Error("must not stage"); },
  });
  const { root } = await renderComposer({ imageInputEnabled: true });
  const file = oversized(name, type, bytes);
  const blobRead = file.arrayBuffer;
  let buffered = 0;
  file.arrayBuffer = () => { buffered += 1; return blobRead.call(file); };

  await act(async () => {
    textarea().dispatchEvent(pasteEvent(file));
    await flushTimers();
    await flushTimers();
  });
  await waitFor(() => toastText() !== "");
  eq(reads, 0, `${name}: FileReader never started`);
  eq(buffered, 0, `${name}: file bytes never buffered for hashing`);
  eq(digests, 0, `${name}: no digest computed`);
  eq(saves, 0, `${name}: nothing sent to the kernel`);
  eq(contextItemCount(), 0, `${name}: no attachment added`);
  ok(/too large/i.test(toastText()), `${name}: toast says the file is too large (got "${toastText()}")`);
  await act(async () => root.unmount());
  dom.window.close();
}

for (const entry of ["pick", "paste"] as const) {
  for (const nativePath of ["", "/repo/releases/large app.apk"]) {
  for (const [name, type, bytes] of [
    ["large app.apk", "application/vnd.android.package-archive", 900 * 1024 * 1024],
    ["pack.zip", "application/zip", 25 * 1024 * 1024 + 1],
    ["small.zip", "application/zip", 2],
  ] as const) {
  const dom = installDom();
  const label = `${entry} ${nativePath ? "native" : "pathless"} ${name}`;
  let reads = 0;
  let digests = 0;
  let buffered = 0;
  let legacySaves = 0;
  const nativeCalls: Array<{ token: string; path: string }> = [];
  const uploads: Array<{ input: string; init: RequestInit }> = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (input, init = {}) => {
    uploads.push({ input: String(input), init });
    return new Response(JSON.stringify({ path: ".reasonix/attachments/uploaded.apk" }), {
      status: 201, headers: { "content-type": "application/json" },
    });
  };
  const originalReader = dom.window.FileReader.prototype.readAsDataURL;
  dom.window.FileReader.prototype.readAsDataURL = function (this: FileReader, blob: Blob) {
    reads++;
    return originalReader.call(this, blob);
  };
  Object.defineProperty(globalThis, "crypto", {
    configurable: true,
    value: { randomUUID: () => "id", subtle: { digest: async () => { digests++; return new ArrayBuffer(32); } } },
  });
  installBridgeApp({
    SavePastedFileForTarget: async () => { legacySaves++; return ".reasonix/attachments/legacy.bin"; },
    AttachDroppedForTarget: async (token: string, path: string) => {
      nativeCalls.push({ token, path });
      return { kind: "workspace", path: "releases/large app.apk", isDir: false };
    },
  }, { getPathForFile: () => nativePath });
  const { root } = await renderComposer({ imageInputEnabled: true });
  const file = oversized(name, type, bytes);
  file.arrayBuffer = async () => { buffered++; return new ArrayBuffer(1); };
  await act(async () => {
    if (entry === "paste") textarea().dispatchEvent(pasteEvent(file));
    else {
      const input = document.querySelector<HTMLInputElement>("input[type=file]");
      if (!input) throw new Error("file picker did not render");
      Object.defineProperty(input, "files", { configurable: true, value: [file] });
      input.dispatchEvent(new Event("change", { bubbles: true }));
    }
    await flushTimers();
    await flushTimers();
  });
  await waitFor(() => contextItemCount() > 0);
  eq(contextItemCount(), 1, `${label}: appears in chat context`);
  eq(toastText(), "", `${label}: no size or upload error`);
  eq(reads, 0, `${label}: FileReader never starts`);
  eq(buffered, 0, `${label}: file bytes never buffered`);
  eq(digests, 0, `${label}: no full-file digest`);
  eq(legacySaves, 0, `${label}: no Base64 RPC`);
  eq(nativeCalls.length, nativePath ? 1 : 0, `${label}: native reference used when available`);
  eq(uploads.length, nativePath ? 0 : 1, `${label}: HTTP upload only for a pathless file`);
  if (nativeCalls[0]) {
    eq(nativeCalls[0].token, "test-attachment-target", `${label}: native reference bound to source target`);
    eq(nativeCalls[0].path, nativePath, `${label}: original native path preserved`);
    ok(document.body.textContent?.includes("large app.apk") === true, `${label}: workspace reference is visible`);
  }
  if (uploads[0]) {
    const uploaded = uploads[0];
    const url = new URL(uploaded.input, "http://localhost/");
    eq(url.pathname, "/__reasonix_attachment_upload", `${label}: raw upload endpoint`);
    eq(url.searchParams.get("target"), "test-attachment-target", `${label}: target token preserved`);
    eq(url.searchParams.get("name"), name, `${label}: filename preserved`);
    eq(uploaded.init.method, "POST", `${label}: POST upload`);
    eq(uploaded.init.body, file, `${label}: original File is the request body`);
    eq(new Headers(uploaded.init.headers).get("content-type"), "application/octet-stream", `${label}: raw binary content type`);
  }
  await act(async () => root.unmount());
  dom.window.close();
  globalThis.fetch = originalFetch;
  }
  }
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
