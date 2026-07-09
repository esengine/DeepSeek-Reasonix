// Run: tsx src/__tests__/composer-goal-toggle.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { Composer, composerPickFileEntry } from "../components/Composer";
import { LocaleProvider } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";
import type { AppBindings } from "../lib/bridge";
import type { CollaborationMode, DirEntry, ToolApprovalMode, TokenMode } from "../lib/types";

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
  const calls: {
    send: string[];
    submit: (string | undefined)[];
    cancel: number;
    clearGoal: number;
    setCollaborationMode: CollaborationMode[];
  } = {
    send: [],
    submit: [],
    cancel: 0,
    clearGoal: 0,
    setCollaborationMode: [],
  };
  let currentProps: Parameters<typeof Composer>[0] = {
    running: false,
    collaborationMode: "normal",
    toolApprovalMode: "ask" as ToolApprovalMode,
    tokenMode: "full" as TokenMode,
    goal: "",
    cwd: "/repo",
    modelLabel: "DeepSeek-R1",
    onSend: (displayText, submitText) => {
      calls.send.push(displayText);
      calls.submit.push(submitText);
    },
    onCancel: () => {
      calls.cancel += 1;
      return undefined;
    },
    onCycleMode: () => {},
    onSetMode: () => {},
    onSetCollaborationMode: (mode) => calls.setCollaborationMode.push(mode),
    onSetToolApprovalMode: () => {},
    onToggleYoloApprovalMode: () => {},
    onClearGoal: () => {
      calls.clearGoal += 1;
    },
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
  return { root, calls, rerender: paint };
}

function mockApp(methods: Partial<AppBindings>) {
  window.go = {
    main: {
      App: {
        Commands: async () => [],
        Models: async () => [],
        ModelsForTab: async () => [],
        ...methods,
      } as Partial<AppBindings> as AppBindings,
    },
  };
}

function dispatchPasteFile(textarea: HTMLTextAreaElement, file: File) {
  const event = new Event("paste", { bubbles: true, cancelable: true });
  Object.defineProperty(event, "clipboardData", {
    configurable: true,
    value: {
      files: [file],
      items: [],
      types: ["Files"],
      getData: () => "",
    },
  });
  textarea.dispatchEvent(event);
}

function nativeFileDropEvent(): Event {
  const drop = new window.Event("drop", { bubbles: true, cancelable: true });
  Object.defineProperty(drop, "dataTransfer", {
    configurable: true,
    value: {
      types: ["Files"],
      files: [{}],
      items: [
        {
          kind: "file",
          webkitGetAsEntry: () => ({ isFile: true }),
        },
      ],
    },
  });
  return drop;
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    await act(async () => {
      await flushTimers();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

type RenderedComposer = Awaited<ReturnType<typeof renderComposer>>;

function fileEntry(name: string): DirEntry {
  return { name, isDir: false };
}

async function replaceComposerDraft(rerender: RenderedComposer["rerender"], id: number, text: string) {
  await rerender({ insertRequest: { id, text, mode: "replace" } });
}

console.log("\ncomposer goal toggle");

{
  const dom = installDom();
  const { root, calls, rerender } = await renderComposer();

  const textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not render");

  await rerender({ insertRequest: { id: 1, text: "ship the release notes", mode: "replace" } });
  eq(textarea.value, "ship the release notes", "insert request populates the composer draft");

  const intentButton = document.querySelector(".composer-action-trigger") as HTMLButtonElement | null;
  if (!intentButton) throw new Error("composer intent button did not render");

  await act(async () => {
    intentButton.click();
    await flushTimers();
  });

  const goalButton = document.querySelectorAll(".composer-intent-menu__item")[1] as HTMLButtonElement | undefined;
  if (!goalButton) throw new Error("composer goal menu item did not render");

  await act(async () => {
    goalButton.click();
    await flushTimers();
  });

  eq(calls.send.length, 0, "enabling goal mode with a draft does not send");
  eq(calls.setCollaborationMode.join(","), "goal", "enabling goal mode switches only the collaboration axis");
  eq(textarea.value, "ship the release notes", "enabling goal mode preserves the draft text");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const { root, calls } = await renderComposer({
    running: true,
    collaborationMode: "goal",
    goal: "finish the migration",
    turnStartAt: Date.now(),
  });

  const stopButton = document.querySelector(".composer__btn--stop") as HTMLButtonElement | null;
  if (!stopButton) throw new Error("composer stop button did not render");

  await act(async () => {
    stopButton.click();
    await flushTimers();
  });

  eq(calls.cancel, 1, "goal-mode stop cancels the running turn");
  eq(calls.clearGoal, 1, "goal-mode stop clears the active goal");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  mockApp({
    SavePastedFile: async () => {
      throw new Error("/Users/example/private.pdf: permission denied");
    },
  });
  const { root, rerender } = await renderComposer();
  await rerender({ insertRequest: { id: 2, text: "keep this draft", mode: "replace" } });

  const textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not render");
  const sendButton = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  if (!sendButton) throw new Error("composer send button did not render");

  await act(async () => {
    dispatchPasteFile(textarea, new File(["hello"], "notes.txt", { type: "text/plain" }));
    await flushTimers();
  });
  await waitFor("pasted file failure toast", () => document.body.textContent?.includes("File attach failed") === true);

  ok(document.body.textContent?.includes("File attach failed") === true, "SavePastedFile rejection shows a visible error");
  eq(textarea.value, "keep this draft", "failed pasted file attach preserves composer text");
  ok(sendButton.disabled === false, "failed pasted file attach clears the pending state");
  ok(document.body.textContent?.includes("/Users/example") === false, "pasted file failure toast does not expose the local path");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  mockApp({
    SavePastedFile: async () => ".reasonix/attachments/notes.txt",
  });
  const { root } = await renderComposer();

  const textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not render");

  await act(async () => {
    dispatchPasteFile(textarea, new File(["hello"], "notes.txt", { type: "text/plain" }));
    await flushTimers();
  });
  await waitFor("pasted file attachment", () => document.body.textContent?.includes("notes.txt") === true);

  ok(document.body.textContent?.includes("notes.txt") === true, "successful pasted file attach still renders the attachment");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  let droppedCallback: ((x: number, y: number, paths: string[]) => void) | undefined;
  window.runtime = {
    EventsOn: () => () => {},
    BrowserOpenURL: () => {},
    OnFileDrop: (cb) => {
      droppedCallback = cb;
    },
    OnFileDropOff: () => {},
  };
  mockApp({
    AttachDropped: async () => {
      throw new Error("/Users/example/secret.pdf: permission denied");
    },
  });
  const { root, rerender } = await renderComposer();
  await rerender({ insertRequest: { id: 3, text: "drop draft", mode: "replace" } });

  const textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not render");
  const sendButton = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  if (!sendButton) throw new Error("composer send button did not render");
  if (!droppedCallback) throw new Error("native file drop handler did not register");

  await act(async () => {
    droppedCallback?.(0, 0, ["/Users/example/secret.pdf"]);
    await flushTimers();
  });
  await waitFor("dropped file failure toast", () => document.body.textContent?.includes("Dropped file attach failed") === true);

  ok(document.body.textContent?.includes("Dropped file attach failed") === true, "AttachDropped rejection shows a visible error");
  eq(textarea.value, "drop draft", "failed dropped file attach preserves composer text");
  ok(sendButton.disabled === false, "failed dropped file attach clears the pending state");
  ok(document.body.textContent?.includes("/Users/example") === false, "dropped file failure toast does not expose the local path");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  let droppedCallback: ((x: number, y: number, paths: string[]) => void) | undefined;
  window.runtime = {
    EventsOn: () => () => {},
    BrowserOpenURL: () => {},
    OnFileDrop: (cb) => {
      droppedCallback = cb;
    },
    OnFileDropOff: () => {},
  };
  mockApp({
    AttachDropped: async () => ({
      kind: "attachment",
      path: ".reasonix/attachments/report.pdf",
    }),
  });
  const { root } = await renderComposer();
  if (!droppedCallback) throw new Error("native file drop handler did not register");

  await act(async () => {
    droppedCallback?.(0, 0, ["/Users/example/report.pdf"]);
    await flushTimers();
  });
  await waitFor("dropped file attachment", () => document.body.textContent?.includes("report.pdf") === true);

  ok(document.body.textContent?.includes("report.pdf") === true, "successful dropped file attach still renders the attachment");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  let droppedCallback: ((x: number, y: number, paths: string[]) => void) | undefined;
  window.runtime = {
    EventsOn: () => () => {},
    BrowserOpenURL: () => {},
    OnFileDrop: (cb) => {
      droppedCallback = cb;
    },
    OnFileDropOff: () => {},
  };
  mockApp({
    AttachDropped: async () => ({
      kind: "workspace",
      path: "__reasonix_external_folder/mock/Folder-With-Spaces",
      isDir: true,
      displayPath: "/Users/example/Folder With Spaces",
    }),
  });
  const { root, calls, rerender } = await renderComposer();
  await rerender({ insertRequest: { id: 4, text: "inspect", mode: "replace" } });
  if (!droppedCallback) throw new Error("native file drop handler did not register");

  await act(async () => {
    droppedCallback?.(0, 0, ["/Users/example/Folder With Spaces"]);
    await flushTimers();
  });
  await waitFor("dropped external folder chip", () => document.body.textContent?.includes("Folder With Spaces/") === true);

  ok(document.body.textContent?.includes("Folder With Spaces/") === true, "dropped external folder renders as a folder context chip");

  const sendButton = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  if (!sendButton) throw new Error("composer send button did not render");
  await act(async () => {
    sendButton.click();
    await flushTimers();
  });

  eq(calls.send.join(","), "inspect @/Users/example/Folder With Spaces/", "external folder display text uses the real folder path");
  eq(calls.submit.join(","), "inspect @__reasonix_external_folder/mock/Folder-With-Spaces/", "external folder submit text uses the session ref token");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const externalToken = "__reasonix_external_folder/mock/Folder-With-Spaces/src/outside.txt";
  const externalDisplayPath = "/Users/example/Folder With Spaces/src/outside.txt";
  const picked = composerPickFileEntry("ask @outside", "outside", "", {
    name: "src/outside.txt",
    path: externalToken,
    isDir: false,
    displayName: "Folder With Spaces/src/outside.txt",
    displayPath: externalDisplayPath,
  });
  eq(picked.text, "ask ", "external search selection removes the token fragment from the draft");
  eq(picked.workspaceRef?.path, externalToken, "external search selection submits the session ref token");
  eq(picked.workspaceRef?.displayPath, externalDisplayPath, "external search selection keeps the real display path");

  const localFile = composerPickFileEntry("ask @src/mai", "src/mai", "src/", { name: "main.go", isDir: false });
  eq(localFile.text, "ask @src/main.go ", "local file selection still completes inline text");

  const localDir = composerPickFileEntry("ask @sr", "sr", "", { name: "src", isDir: true });
  eq(localDir.text, "ask @src/", "local dir selection still keeps the menu-open slash");
}

{
  const dom = installDom();
  const { root: dropNavRoot } = await renderComposer();
  const composer = document.querySelector(".composer") as HTMLElement | null;
  if (!composer) throw new Error("composer did not render");

  const drop = nativeFileDropEvent();
  await act(async () => {
    composer.dispatchEvent(drop);
    await flushTimers();
  });
  ok(drop.defaultPrevented, "native file drop prevents browser image navigation");

  await act(async () => {
    dropNavRoot.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const { root: dropWrapRoot } = await renderComposer();
  const wrap = document.querySelector(".composer-wrap") as HTMLElement | null;
  if (!wrap) throw new Error("composer wrap did not render");

  const drop = nativeFileDropEvent();
  await act(async () => {
    wrap.dispatchEvent(drop);
    await flushTimers();
  });
  ok(drop.defaultPrevented, "outer native file drop target prevents browser image navigation");

  await act(async () => {
    dropWrapRoot.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  let rejectSubmit: (err: Error) => void = () => {};
  const rejectedSubmit = new Promise<void>((_, reject) => {
    rejectSubmit = reject;
  });
  rejectedSubmit.catch(() => {});
  const { root, calls, rerender } = await renderComposer({
    onSend: (displayText) => {
      calls.send.push(displayText);
      return rejectedSubmit;
    },
  });

  const textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not render");

  await rerender({ insertRequest: { id: 2, text: "keep this draft", mode: "replace" } });
  const sendButton = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  if (!sendButton) throw new Error("composer send button did not render");

  await act(async () => {
    sendButton.click();
    rejectSubmit(new Error("workspace is still starting"));
    await flushTimers();
  });

  eq(calls.send.join(","), "keep this draft", "rejected submit attempts the send once");
  eq(textarea.value, "keep this draft", "rejected submit preserves the composer draft");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const { root, calls, rerender } = await renderComposer({
    onSend: (displayText) => {
      calls.send.push(displayText);
      return Promise.resolve();
    },
  });

  const textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not render");

  await rerender({ insertRequest: { id: 3, text: "send this draft", mode: "replace" } });
  const sendButton = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  if (!sendButton) throw new Error("composer send button did not render");

  await act(async () => {
    sendButton.click();
    await flushTimers();
  });

  eq(calls.send.join(","), "send this draft", "successful submit attempts the send once");
  eq(textarea.value, "", "successful submit clears the composer draft");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const { root, rerender } = await renderComposer({
    running: true,
    guidanceQueuePreviewItems: ["confirm the send lifecycle", "keep steer protocol unchanged", "add a hanging submit regression"],
  });

  let guidanceItems = Array.from(document.querySelectorAll(".composer-guidance-item"));
  eq(guidanceItems.length, 2, "running guidance preview shows a compact queue preview");
  ok(guidanceItems[0]?.textContent?.includes("confirm the send lifecycle") === true, "guidance preview shows the first seeded item");
  ok(guidanceItems[1]?.textContent?.includes("keep steer protocol unchanged") === true, "guidance preview shows the second seeded item");
  eq(document.querySelectorAll(".composer-guidance-item__guide").length, 2, "guidance preview exposes a guide action for each visible item");
  let guidanceMore = document.querySelector(".composer-guidance-more") as HTMLButtonElement | null;
  ok(guidanceMore?.textContent?.includes("1 more queued") === true, "guidance preview summarizes overflow items");
  eq(guidanceMore?.getAttribute("aria-expanded"), "false", "guidance overflow starts collapsed");

  if (!guidanceMore) throw new Error("guidance overflow button did not render");
  await act(async () => {
    guidanceMore.click();
    await flushTimers();
  });
  guidanceItems = Array.from(document.querySelectorAll(".composer-guidance-item"));
  eq(guidanceItems.length, 3, "guidance overflow expands the remaining queued items");
  ok(guidanceItems[2]?.textContent?.includes("add a hanging submit regression") === true, "expanded guidance preview shows the hidden item");
  guidanceMore = document.querySelector(".composer-guidance-more") as HTMLButtonElement | null;
  ok(guidanceMore?.textContent?.includes("Collapse") === true, "expanded guidance overflow can be collapsed");
  eq(guidanceMore?.getAttribute("aria-expanded"), "true", "guidance overflow reports expanded state");

  if (!guidanceMore) throw new Error("guidance collapse button did not render");
  await act(async () => {
    guidanceMore.click();
    await flushTimers();
  });
  guidanceItems = Array.from(document.querySelectorAll(".composer-guidance-item"));
  eq(guidanceItems.length, 2, "guidance overflow collapses back to the compact preview");

  await rerender({ guidanceQueuePreviewItems: ["only the latest preview seed"] });
  guidanceItems = Array.from(document.querySelectorAll(".composer-guidance-item"));
  eq(guidanceItems.length, 1, "guidance preview refreshes when the seed changes");
  ok(guidanceItems[0]?.textContent?.includes("only the latest preview seed") === true, "guidance preview renders the refreshed seed");

  await rerender({ running: false });
  ok(document.querySelector(".composer-guidance-item") === null, "guidance preview clears when the mock turn stops");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const { root, calls, rerender } = await renderComposer({
    running: true,
    onSend: (displayText, submitText) => {
      calls.send.push(displayText);
      calls.submit.push(submitText);
    },
  });

  const textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not render");
  const sendButton = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  if (!sendButton) throw new Error("running composer send button did not render");

  eq(textarea.placeholder, "Running — type guidance, Enter adds it to the queue", "running composer explains queued guidance input");
  ok(sendButton.classList.contains("composer__btn--steer"), "running composer marks send button as steer");
  ok(sendButton.disabled === true, "running steer button stays disabled without input");

  await rerender({ insertRequest: { id: 4, text: "keep the files small", mode: "replace" } });
  ok(sendButton.disabled === false, "running steer button enables after text input");

  await act(async () => {
    sendButton.click();
    await flushTimers();
  });

  eq(calls.send.join(","), "", "running composer queues guidance without sending it immediately");
  eq(textarea.value, "", "queued running guidance clears the composer draft");
  const guidanceItem = document.querySelector(".composer-guidance-item") as HTMLElement | null;
  if (!guidanceItem) throw new Error("running guidance chip did not render");
  ok(guidanceItem.textContent?.includes("keep the files small") === true, "running guidance chip shows queued text");
  ok(document.querySelector(".composer-guidance-head")?.textContent?.includes("Queued guidance 1") === true, "running guidance shelf shows queued count");

  const guideButton = guidanceItem.querySelector(".composer-guidance-item__guide") as HTMLButtonElement | null;
  if (!guideButton) throw new Error("running guidance guide button did not render");
  await act(async () => {
    guideButton.click();
    await flushTimers();
  });
  eq(calls.send.join(","), "keep the files small", "queued guidance sends through onSend when guided");
  ok(document.querySelector(".composer-guidance-item") === null, "queued guidance clears after being guided");

  await rerender({ insertRequest: { id: 5, text: "prefer the smaller diff", mode: "replace" } });
  await act(async () => {
    sendButton.click();
    await flushTimers();
  });
  const dismissibleGuidanceItem = document.querySelector(".composer-guidance-item") as HTMLElement | null;
  if (!dismissibleGuidanceItem) throw new Error("dismissible guidance chip did not render");
  const dismissButton = dismissibleGuidanceItem.querySelector(".composer-guidance-item__action") as HTMLButtonElement | null;
  if (!dismissButton) throw new Error("running guidance dismiss button did not render");
  await act(async () => {
    dismissButton.click();
    await flushTimers();
  });
  ok(document.querySelector(".composer-guidance-item") === null, "running guidance chip can be dismissed");

  await rerender({ insertRequest: { id: 6, text: "prefer the smaller diff", mode: "replace" } });
  await act(async () => {
    sendButton.click();
    await flushTimers();
  });
  ok(document.querySelector(".composer-guidance-item") !== null, "running guidance chip renders again after another queued item");

  await rerender({ guidanceConsumedKey: "s1", guidanceConsumedText: "prefer the smaller diff" });
  ok(document.querySelector(".composer-guidance-item") === null, "running guidance chip clears when steer is consumed");

  await rerender({ insertRequest: { id: 7, text: "then stop showing the chip", mode: "replace" } });
  await act(async () => {
    sendButton.click();
    await flushTimers();
  });
  ok(document.querySelector(".composer-guidance-item") !== null, "running guidance chip renders before turn stop");

  await rerender({ running: false });
  ok(document.querySelector(".composer-guidance-item") === null, "running guidance chip clears when the turn stops");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const { root, calls, rerender } = await renderComposer({
    running: true,
    submitDisabled: true,
    onSend: (displayText) => {
      calls.send.push(displayText);
    },
  });

  const sendButton = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  if (!sendButton) throw new Error("running composer send button did not render");

  await rerender({ insertRequest: { id: 7, text: "steer while activating", mode: "replace" } });
  ok(sendButton.disabled === false, "running guidance queue ignores controller submitDisabled");

  await act(async () => {
    sendButton.click();
    await flushTimers();
  });

  eq(calls.send.join(","), "", "running guidance queues while controllerReady is false");
  const guideButton = document.querySelector(".composer-guidance-item__guide") as HTMLButtonElement | null;
  if (!guideButton) throw new Error("running guidance guide button did not render");
  await act(async () => {
    guideButton.click();
    await flushTimers();
  });

  eq(calls.send.join(","), "steer while activating", "queued guidance can be guided while controllerReady is false");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  // Reproduces #6210: a message queued while a turn is running, without the
  // explicit "guide" steer click, must not vanish when the turn ends on its
  // own — it is the user's next turn, so it should send automatically.
  const dom = installDom();
  const { root, calls, rerender } = await renderComposer({
    running: true,
    onSend: (displayText, submitText) => {
      calls.send.push(displayText);
      calls.submit.push(submitText);
      return Promise.resolve();
    },
  });

  await rerender({ insertRequest: { id: 8, text: "keep going after this finishes", mode: "replace" } });
  const sendButton = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  if (!sendButton) throw new Error("running composer send button did not render");

  await act(async () => {
    sendButton.click();
    await flushTimers();
  });

  eq(calls.send.length, 0, "queuing while running does not send immediately");
  ok(document.querySelector(".composer-guidance-item") !== null, "queued message shows in the guidance shelf");

  await rerender({ running: false });
  await waitFor("queued guidance auto-sent on natural completion", () => calls.send.length === 1);

  eq(calls.send.join(","), "keep going after this finishes", "queued guidance is sent automatically once the turn ends naturally, not discarded");
  eq(calls.submit.join(","), "keep going after this finishes", "auto-sent guidance submits the same text it was queued with");
  ok(document.querySelector(".composer-guidance-item") === null, "guidance shelf clears once the queued message is sent");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  // #6210 follow-up: if the turn ends naturally while the controller is
  // still activating/hydrating (submitDisabled), onSend would silently
  // no-op — auto-send must wait for submitDisabled to clear instead of
  // firing into that window and losing the queued message anyway.
  const dom = installDom();
  const { root, calls, rerender } = await renderComposer({
    running: true,
    submitDisabled: false,
    onSend: (displayText, submitText) => {
      calls.send.push(displayText);
      calls.submit.push(submitText);
      return Promise.resolve();
    },
  });

  await rerender({ insertRequest: { id: 9, text: "keep going once ready", mode: "replace" } });
  const sendButton = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  if (!sendButton) throw new Error("running composer send button did not render");

  await act(async () => {
    sendButton.click();
    await flushTimers();
  });
  ok(document.querySelector(".composer-guidance-item") !== null, "queued message shows in the guidance shelf");

  // Turn ends, but the controller is still not ready to accept a submit —
  // matches a rebuild/hydration window right after the turn finishes.
  await rerender({ running: false, submitDisabled: true });
  await act(async () => {
    await flushTimers();
  });
  eq(calls.send.length, 0, "auto-send does not fire while the controller is still activating");
  ok(document.querySelector(".composer-guidance-item") !== null, "queued message stays on the shelf while not ready");

  await rerender({ submitDisabled: false });
  await waitFor("queued guidance auto-sent once the controller becomes ready", () => calls.send.length === 1);

  eq(calls.send.join(","), "keep going once ready", "queued guidance sends once submitDisabled clears, instead of being lost");
  ok(document.querySelector(".composer-guidance-item") === null, "guidance shelf clears once the delayed send completes");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  let listDirCalls = 0;
  mockApp({
    ListDir: async () => {
      listDirCalls += 1;
      return listDirCalls === 1 ? [fileEntry("cached-dir.txt")] : [fileEntry("fresh-dir.txt")];
    },
    SearchFileRefs: async () => [],
  });
  const { root, rerender } = await renderComposer();

  await replaceComposerDraft(rerender, 101, "@");
  await waitFor("initial @ directory load", () => listDirCalls === 1);

  await replaceComposerDraft(rerender, 102, "");
  await replaceComposerDraft(rerender, 103, "@");
  await waitFor("@ directory revalidation call", () => listDirCalls === 2);

  eq(listDirCalls, 2, "@ directory cache hit still revalidates ListDir");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  let listDirCalls = 0;
  mockApp({
    ListDir: async () => {
      listDirCalls += 1;
      return listDirCalls === 1 ? [fileEntry("manual-refresh-stale.txt")] : [fileEntry("manual-refresh-fresh.txt")];
    },
    SearchFileRefs: async () => [],
  });
  const { root, rerender } = await renderComposer({ fileRefRefreshKey: "0" });

  await replaceComposerDraft(rerender, 201, "@");
  await waitFor("initial @ directory load before refresh key", () => listDirCalls === 1);

  await rerender({ fileRefRefreshKey: "1" });
  await waitFor("@ directory reload after refresh key", () => listDirCalls === 2);

  eq(listDirCalls, 2, "fileRefRefreshKey refreshes @ directory cache while the menu is open");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const realDateNow = Date.now;
  let now = 1000;
  let searchCalls = 0;
  Date.now = () => now;
  mockApp({
    ListDir: async () => [],
    SearchFileRefs: async () => {
      searchCalls += 1;
      return searchCalls === 1 ? [fileEntry("alpha-old.ts")] : [fileEntry("alpha-new.ts")];
    },
  });
  const { root, rerender } = await renderComposer();

  try {
    await replaceComposerDraft(rerender, 301, "@alpha");
    await waitFor("initial @ search request", () => searchCalls === 1);
    eq(searchCalls, 1, "@ search fetches the first query");

    await replaceComposerDraft(rerender, 302, "");
    now = 2000;
    await replaceComposerDraft(rerender, 303, "@alpha");
    await act(async () => {
      await flushTimers();
    });
    eq(searchCalls, 1, "@ search cache is reused inside the TTL");

    await replaceComposerDraft(rerender, 304, "");
    now = 7001;
    await replaceComposerDraft(rerender, 305, "@alpha");
    await waitFor("expired @ search cache refresh", () => searchCalls === 2);
    eq(searchCalls, 2, "@ search cache revalidates after the TTL");
  } finally {
    Date.now = realDateNow;
  }

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  let staleListDirResolve: ((entries: DirEntry[]) => void) | undefined;
  let thirdListDirResolve: ((entries: DirEntry[]) => void) | undefined;
  let listDirCalls = 0;
  mockApp({
    ListDir: async () => {
      listDirCalls += 1;
      if (listDirCalls === 1) {
        return new Promise<DirEntry[]>((resolve) => {
          staleListDirResolve = resolve;
        });
      }
      if (listDirCalls === 2) return [fileEntry("cache-live.txt")];
      return new Promise<DirEntry[]>((resolve) => {
        thirdListDirResolve = resolve;
      });
    },
    SearchFileRefs: async () => [],
  });
  const { root, rerender } = await renderComposer({ fileRefRefreshKey: "0" });

  const textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not render");

  await replaceComposerDraft(rerender, 401, "@cache");
  await waitFor("initial stale @ directory request", () => listDirCalls === 1);

  await rerender({ fileRefRefreshKey: "1" });
  await waitFor("fresh @ directory request after refresh key", () => listDirCalls === 2);
  await act(async () => {
    await flushTimers();
  });

  staleListDirResolve?.([fileEntry("cache-stale.txt")]);
  await act(async () => {
    await flushTimers();
  });

  await replaceComposerDraft(rerender, 402, "");
  await replaceComposerDraft(rerender, 403, "@cache");
  await waitFor("second fresh @ directory request", () => listDirCalls === 3);
  await act(async () => {
    textarea.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  eq(textarea.value, "@cache-live.txt ", "stale @ directory request cannot repopulate cache after refresh");
  thirdListDirResolve?.([fileEntry("cache-later.txt")]);

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
