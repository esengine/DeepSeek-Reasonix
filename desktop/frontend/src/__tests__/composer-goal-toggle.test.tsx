// Run: tsx src/__tests__/composer-goal-toggle.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { Composer, composerPickFileEntry } from "../components/Composer";
import { InvocationMetadataContext, UserMessage } from "../components/Message";
import { LocaleProvider } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";
import type { AppBindings } from "../lib/bridge";
import type { StructuredInvocationSubmit } from "../lib/invocationDisplay";
import type { CollaborationMode, CommandInfo, DirEntry, ToolApprovalMode, TokenMode } from "../lib/types";

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

function flushTimers(ms = 0): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
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

async function renderComposer(props: Partial<Parameters<typeof Composer>[0]> = {}) {
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const calls: {
    send: string[];
    submit: (string | undefined)[];
    structured: (StructuredInvocationSubmit | undefined)[];
    cancel: number;
    clearGoal: number;
    setCollaborationMode: CollaborationMode[];
  } = {
    send: [],
    submit: [],
    structured: [],
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
    tabId: "tab-a",
    modelLabel: "DeepSeek-R1",
    onSend: (displayText, submitText, _tabId, structured) => {
      calls.send.push(displayText);
      calls.submit.push(submitText);
      calls.structured.push(structured);
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
        SlashArgs: async () => ({ items: [], from: 0 }),
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

  let textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not render");

  await rerender({ insertRequest: { id: 1, text: "ship the release notes", mode: "replace" } });
  eq(textarea.value, "ship the release notes", "insert request populates the composer draft");
  // The insert queues a rAF that refocuses the textarea; drain that frame
  // before focusing the trigger, or the late refocus blurs the tooltip away.
  await act(async () => {
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    await flushTimers();
  });

  await rerender({ insertRequest: { id: 2, text: "/reviewer ", mode: "prefix" } });
  eq(textarea.value, "/reviewer ship the release notes", "prefix insert preserves the draft as a subagent task");
  eq(calls.send.length, 0, "prefix insert does not send the subagent task");

  const intentButton = document.querySelector(".composer-task-mode-trigger") as HTMLButtonElement | null;
  if (!intentButton) throw new Error("composer intent button did not render");
  eq(intentButton.textContent?.trim(), "Standard", "execution method trigger shows only the current method");
  eq(intentButton.getAttribute("aria-label"), "Execution method · Standard", "execution method trigger keeps its full accessible name");
  const intentTooltipTrigger = intentButton.closest(".tooltip-trigger");
  if (!intentTooltipTrigger) throw new Error("composer intent tooltip trigger did not render");
  await act(async () => {
    intentTooltipTrigger.dispatchEvent(new Event("focusin", { bubbles: true }));
    await flushTimers();
  });
  await waitFor("execution method tooltip", () => document.querySelector('[role="tooltip"]') !== null);
  eq(document.querySelector('[role="tooltip"]')?.textContent, "Execution method · Standard: Analyze and act as you go", "execution method tooltip combines category, value, and summary");
  await act(async () => {
    intentTooltipTrigger.dispatchEvent(new Event("focusout", { bubbles: true }));
    await flushTimers();
  });

  await act(async () => {
    intentButton.click();
    await flushTimers();
  });

  const taskModeItems = document.querySelectorAll(".composer-intent-menu__item");
  eq(taskModeItems.length, 3, "task method menu exposes three mutually exclusive choices");
  eq(document.querySelectorAll(".composer-intent-switch").length, 0, "task method menu does not present independent switches");
  const goalButton = taskModeItems[2] as HTMLButtonElement | undefined;
  if (!goalButton) throw new Error("composer goal menu item did not render");

  await act(async () => {
    goalButton.click();
    await flushTimers();
  });

  eq(calls.send.length, 0, "enabling goal mode with a draft does not send");
  eq(calls.setCollaborationMode.join(","), "goal", "enabling goal mode switches only the collaboration axis");
  eq(textarea.value, "/reviewer ship the release notes", "enabling goal mode preserves the prefixed draft text");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const { root, calls } = await renderComposer({
    collaborationMode: "goal",
    goal: "finish the migration",
  });

  const intentButton = document.querySelector(".composer-task-mode-trigger") as HTMLButtonElement | null;
  if (!intentButton) throw new Error("active goal task method trigger did not render");
  ok(intentButton.textContent?.includes("Goal") === true, "task method trigger exposes an active goal");

  await act(async () => {
    intentButton.click();
    await flushTimers();
  });

  const stopGoal = document.querySelector(".composer-intent-menu__stop") as HTMLButtonElement | null;
  if (!stopGoal) throw new Error("explicit stop goal action did not render");
  eq(stopGoal.textContent, "Stop goal", "active goal uses an explicit stop action");
  await act(async () => {
    stopGoal.click();
    await flushTimers();
  });
  eq(calls.clearGoal, 1, "explicit stop action clears the active goal");
  eq(calls.setCollaborationMode.length, 0, "stopping a goal does not race a second mode update");

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

  let textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
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

  const trailingNewline = composerPickFileEntry("ask @src/mai\n", "src/mai", "src/", { name: "main.go", isDir: false });
  eq(trailingNewline.text, "ask @src/main.go ", "file selection ignores an invisible trailing newline");
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
  const listDirTabs: string[] = [];
  mockApp({
    ListDirForTab: async (tabId) => {
      listDirTabs.push(tabId);
      listDirCalls += 1;
      return listDirCalls === 1 ? [fileEntry("cached-dir.txt")] : [fileEntry("fresh-dir.txt")];
    },
    SearchFileRefsForTab: async () => [],
  });
  const { root, rerender } = await renderComposer();

  await replaceComposerDraft(rerender, 101, "@");
  await waitFor("initial @ directory load", () => listDirCalls === 1);

  await replaceComposerDraft(rerender, 102, "");
  await replaceComposerDraft(rerender, 103, "@");
  await waitFor("@ directory revalidation call", () => listDirCalls === 2);

  eq(listDirCalls, 2, "@ directory cache hit still revalidates ListDir");
  ok(listDirTabs.every((tabId) => tabId === "tab-a"), "@ directory requests stay scoped to the composer tab");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  let listDirCalls = 0;
  mockApp({
    ListDirForTab: async () => {
      listDirCalls += 1;
      return listDirCalls === 1 ? [fileEntry("manual-refresh-stale.txt")] : [fileEntry("manual-refresh-fresh.txt")];
    },
    SearchFileRefsForTab: async () => [],
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
    ListDirForTab: async () => [],
    SearchFileRefsForTab: async () => {
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
    ListDirForTab: async () => {
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
    SearchFileRefsForTab: async () => [],
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

{
  const dom = installDom();
  const pending: Array<(entries: DirEntry[]) => void> = [];
  mockApp({
    ListDirForTab: async () => [],
    SearchFileRefsForTab: async () => new Promise<DirEntry[]>((resolve) => pending.push(resolve)),
  });
  const { root, rerender } = await renderComposer({ workspaceScopeKey: "session-a" });
  const textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not render");

  await replaceComposerDraft(rerender, 501, "@current");
  await waitFor("initial composer session scope request", () => pending.length === 1);
  await rerender({ workspaceScopeKey: "session-b" });
  await waitFor("next composer session scope request", () => pending.length === 2);
  await rerender({ workspaceScopeKey: "session-a" });
  await waitFor("revisited composer session scope request", () => pending.length === 3);

  await act(async () => {
    pending[2]([fileEntry("current-session-a.txt")]);
    await flushTimers();
  });

  await act(async () => {
    pending[0]([fileEntry("stale-initial-a.txt")]);
    pending[1]([fileEntry("stale-session-b.txt")]);
    await flushTimers();
    textarea.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers();
  });

  eq(textarea.value, "@current-session-a.txt ", "same-tab A→B→A keeps the current composer file-ref search cache");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  let commandsCalls = 0;
  const slashArgInputs: string[] = [];
  let availableCommands: CommandInfo[] = [
    { name: "mcp", description: "Manage MCP servers", kind: "builtin", group: "integrations" },
    { name: "explore", description: "Investigate the codebase", kind: "subagent" },
    { name: "superpowers:writing-plans", description: "Write a plan", kind: "skill", plugin: "superpowers" },
    { name: "toolbox:writing-plans", description: "Write another plan", kind: "skill", plugin: "toolbox" },
    { name: "superpowers:brainstorming", description: "Explore an idea", kind: "skill", plugin: "superpowers" },
  ];
  mockApp({
    Commands: async () => {
      commandsCalls += 1;
      return availableCommands;
    },
    ListDirForTab: async () => [],
    SearchFileRefsForTab: async () => [],
    SlashArgs: async (input) => {
      slashArgInputs.push(input);
      return input === "/mcp "
        ? { items: [{ label: "show", insert: "show", hint: "Show an MCP server", descend: false }], from: 5 }
        : { items: [], from: 0 };
    },
  });
  const { root, calls, rerender } = await renderComposer({ workspaceScopeKey: "runtime-0" });

  await waitFor("plugin commands loaded", () => commandsCalls > 0);
  await replaceComposerDraft(rerender, 1999, "/\n");
  await waitFor("slash menu before trailing newline", () => Boolean(document.querySelector(".slashmenu")));
  ok(document.querySelector(".slashmenu") !== null, "slash menu ignores an invisible trailing newline");

  await replaceComposerDraft(rerender, 1998, "@\n");
  await waitFor("file menu before trailing newline", () => Boolean(document.querySelector(".slashmenu")));
  ok(document.querySelector(".slashmenu") !== null, "file menu ignores an invisible trailing newline");

  await replaceComposerDraft(rerender, 1997, "/mcp \n");
  await act(async () => {
    await flushTimers(150);
  });
  await waitFor("slash argument menu before trailing newline", () => document.querySelector(".slashmenu")?.textContent?.includes("show") === true);
  ok(slashArgInputs.includes("/mcp "), "slash argument completion removes an invisible trailing newline before lookup");

  await replaceComposerDraft(rerender, 2000, "/m");
  await waitFor("initial skill command menu", () => Boolean(document.querySelector(".slashmenu")));
  ok(
    document.querySelector(".slashmenu")?.textContent?.includes("/my-formatter") === false,
    "new subagent command is absent before runtime refresh",
  );

  availableCommands = [
    ...availableCommands,
    { name: "my-formatter", description: "Formats code the way I like it", kind: "subagent", color: "amber" },
  ];
  const initialCommandsCalls = commandsCalls;
  await rerender({ workspaceScopeKey: "runtime-1" });
  await waitFor("commands refreshed after runtime rebuild", () => commandsCalls > initialCommandsCalls);
  ok(commandsCalls > initialCommandsCalls, "runtime rebuild refetches subagent slash commands");

  await replaceComposerDraft(rerender, 2001, "/writing-plans");
  await waitFor("qualified plugin skill menu", () => Boolean(document.querySelector(".slashmenu")));

  const menuSizer = document.querySelector<HTMLElement>(".slashmenu__sizer");
  eq(menuSizer?.style.height, "94px", "short skill query keeps one group heading and both matching plugin names");
  let textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not render");
  await act(async () => {
    textarea.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  ok(document.querySelector(".composer__rich-input") !== null, "selecting a plugin skill switches to the rich task input");
  ok(document.querySelector(".invocation-display--composer")?.textContent?.includes("Writing Plans") === true, "selected skill renders as composer context");
  ok(document.querySelector(".invocation-display--composer")?.textContent?.includes("superpowers") === true, "selected plugin skill keeps its source visible");
  ok(document.querySelector(".composer__rich-input .composer-invocation-token") !== null, "selected skill is an inline task entity");
  ok(document.querySelector(".composer__rich-input .composer-invocation-caret-anchor")?.textContent === "\u00A0", "selected skill keeps a caret anchor after the inline entity");

  const richInput = document.querySelector(".composer__rich-input") as HTMLDivElement | null;
  if (!richInput) throw new Error("rich composer did not render");
  const richContent = document.querySelector(".composer__content") as HTMLDivElement | null;
  if (!richContent) throw new Error("rich composer content area did not render");
  richInput.blur();
  await act(async () => {
    richContent.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true }));
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    await flushTimers();
  });
  ok(document.activeElement === richInput, "clicking blank rich-composer space focuses the editable task input");

  const invocationToken = richInput.querySelector(".composer-invocation-token");
  if (!invocationToken) throw new Error("rich invocation token did not render");
  const richRange = document.createRange();
  richRange.setStartAfter(invocationToken);
  richRange.collapse(true);
  document.getSelection()?.removeAllRanges();
  document.getSelection()?.addRange(richRange);
  await act(async () => {
    richInput.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Backspace", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  ok(document.querySelector(".invocation-display--composer") === null, "Backspace removes a selected skill from an empty task input");
  await act(async () => {
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    await flushTimers();
  });
  const textareaAfterEntityRemoval = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textareaAfterEntityRemoval) throw new Error("textarea did not return after removing the last entity");
  ok(
    document.activeElement === textareaAfterEntityRemoval,
    "removing the last entity hands focus to the textarea that replaces the rich input",
  );

  await replaceComposerDraft(rerender, 2002, "/writing-plans");
  await waitFor("skill menu after removal", () => Boolean(document.querySelector(".slashmenu")));
  textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not return after removing the skill");
  await act(async () => {
    textarea.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers();
  });

  let sendButton = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  if (!sendButton) throw new Error("composer send button did not render for skill-only invocation");
  ok(sendButton.disabled === false, "inline skill-only invocation enables submit");
  await act(async () => {
    sendButton?.click();
    await flushTimers();
  });
  eq(calls.submit[0], "/superpowers:writing-plans", "inline skill-only submission retains display metadata");
  eq(calls.structured[0]?.input, "", "inline skill-only submission sends an empty explicit task");
  eq(calls.structured[0]?.display, "/superpowers:writing-plans", "inline skill-only submission preserves reloadable invocation display metadata");
  eq(calls.structured[0]?.invocations[0]?.name, "superpowers:writing-plans", "inline skill-only submission sends a structured skill entity");

  await replaceComposerDraft(rerender, 20021, "/writing-plans");
  await waitFor("skill menu for task submission", () => Boolean(document.querySelector(".slashmenu")));
  textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not return after skill-only send");
  await act(async () => {
    textarea.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers();
  });

  await replaceComposerDraft(rerender, 2003, "Draft the release plan");
  sendButton = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  if (!sendButton) throw new Error("composer send button did not render");
  await act(async () => {
    sendButton.click();
    await flushTimers();
  });
  eq(calls.send[1], "Draft the release plan", "selected skill keeps the visible transcript text clean");
  eq(calls.submit[1], "/superpowers:writing-plans Draft the release plan", "selected skill preserves invocation display metadata");
  eq(calls.structured[1]?.input, "Draft the release plan", "selected skill sends task text separately from invocation metadata");
  ok(document.querySelector(".invocation-display--composer") === null, "selected skill clears after send");

  await replaceComposerDraft(rerender, 2004, "/mcp");
  await waitFor("builtin command menu", () => Boolean(document.querySelector(".slashmenu")));
  textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not render for management command");
  await act(async () => {
    textarea.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  eq(textarea.value, "/mcp ", "management commands keep the existing inline argument flow");
  ok(document.querySelector(".invocation-display--composer") === null, "management commands do not become selected abilities");

  await replaceComposerDraft(rerender, 2005, "/my-formatter");
  await waitFor("colored subagent command menu", () => Boolean(document.querySelector(".slashmenu")));
  textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not render for colored subagent");
  await act(async () => {
    textarea.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  ok(document.querySelector<HTMLElement>(".invocation-display--composer")?.style.getPropertyValue("--invocation-color") === "#d59a2f", "selected custom subagent uses its configured color");
  sendButton = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  ok(sendButton?.disabled === true, "subagent-only invocation remains blocked until a task is entered");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <UserMessage
          id="h1"
          text="Draft the release plan"
          submitText="/superpowers:writing-plans Draft the release plan"
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });
  ok(document.querySelector(".invocation-display--message")?.textContent?.includes("Writing Plans") === true, "restored history renders the selected skill header");
  ok(document.querySelector(".invocation-display--message")?.textContent?.includes("superpowers") === true, "restored history retains plugin source from the qualified command");
  ok(document.querySelector(".msg__rich-text")?.textContent?.endsWith("Draft the release plan") === true, "history message keeps slash syntax out of the task body");

  await act(async () => {
    root.render(
      <LocaleProvider>
        <InvocationMetadataContext.Provider value={{ "my-formatter": { kind: "subagent", color: "amber" } }}>
          <UserMessage
            id="h2"
            text="Format this file"
            submitText={"以下是用户引用的历史会话上下文：\n\n[会话：Earlier]\n...\n\n---\n\n当前用户问题：\n/my-formatter Format this file"}
          />
        </InvocationMetadataContext.Provider>
      </LocaleProvider>,
    );
    await flushTimers();
  });
  ok(document.querySelector(".invocation-display--message")?.textContent?.includes("My Formatter") === true, "history and trash previews recover selected abilities after referenced-session context");
  ok(document.querySelector(".invocation-display--subagent") !== null, "restored custom subagents keep their command type styling");
  ok(document.querySelector<HTMLElement>(".invocation-display--subagent")?.style.getPropertyValue("--invocation-color") === "#d59a2f", "restored custom subagents keep their configured color");

  await act(async () => {
    root.render(
      <LocaleProvider>
        <UserMessage
          id="h3"
          text={"Compare these commands\n/other-command"}
          submitText={"/reasonix-develop Compare these commands\n/other-command"}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });
  ok(document.querySelector(".invocation-display--message")?.textContent?.includes("Reasonix Develop") === true, "history recovery ignores slash-prefixed lines inside the task body");

  await act(async () => {
    root.render(
      <LocaleProvider>
        <UserMessage
          id="h4"
          text={"Compare these commands\n/other-command"}
          submitText={"以下是用户引用的历史会话上下文：\n\n[会话：Earlier]\n...\n\n---\n\n当前用户问题：\nCompare these commands\n/other-command"}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });
  ok(document.querySelector(".invocation-display--message") === null, "ordinary referenced-session text does not turn task slash lines into a skill header");

  await act(async () => root.unmount());
  dom.window.close();
}

{
  const dom = installDom();
  let savedFiles = 0;
  mockApp({
    Commands: async () => [{ name: "skill", description: "Manage skills", kind: "builtin" }],
    ListDirForTab: async () => [fileEntry("README.md")],
    SearchFileRefsForTab: async () => [],
    ListSessions: async () => [{ path: "/sessions/recent.jsonl", title: "Recent session", current: false }],
    SavePastedFile: async () => {
      savedFiles += 1;
      return ".reasonix/attachments/notes.txt";
    },
  });
  const { root, rerender } = await renderComposer();
  const textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("composer textarea did not render");

  await replaceComposerDraft(rerender, 3000, "Follow up #recent\n");
  await waitFor("typed hash recent-session picker", () => Boolean(document.querySelector(".slashmenu__search")));
  const typedSessionSearch = document.querySelector(".slashmenu__search") as HTMLInputElement | null;
  eq(typedSessionSearch?.value, "recent", "typing # opens recent sessions and carries the query across an invisible trailing newline");
  ok(document.activeElement !== typedSessionSearch, "the typed # flow leaves focus in the composer instead of the panel search box");
  await act(async () => {
    typedSessionSearch?.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  eq(textarea.value, "Follow up #recent\n", "Escape closes the typed recent-session picker and keeps the literal # text");
  ok(!document.querySelector(".slashmenu__search"), "Escape dismisses the typed recent-session panel until the query changes");

  await replaceComposerDraft(rerender, 3005, "issue#6310");
  await act(async () => {
    await flushTimers();
  });
  ok(!document.querySelector(".slashmenu__search"), "an embedded hash remains ordinary composer text");

  await replaceComposerDraft(rerender, 3006, "#\n");
  await waitFor("typed hash picker before session selection", () => Boolean(document.querySelector(".slashmenu__search")));
  const typedSessionButton = Array.from(document.querySelectorAll<HTMLButtonElement>(".slashmenu button"))
    .find((button) => button.textContent?.includes("Recent session"));
  if (!typedSessionButton) throw new Error("typed recent-session option did not render");
  await act(async () => {
    typedSessionButton.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true }));
    await flushTimers();
  });
  eq(textarea.value, "", "selecting a typed recent-session reference removes the # token");
  ok(document.querySelector(".composer-context__item--session")?.textContent?.includes("Recent session") === true, "selecting a typed recent-session reference adds its context card");
  const removeTypedSession = document.querySelector<HTMLButtonElement>(".composer-context__item--session button");
  await act(async () => {
    removeTypedSession?.click();
    await flushTimers();
  });

  const contentTrigger = document.querySelector(".composer-content-trigger") as HTMLButtonElement | null;
  if (!contentTrigger) throw new Error("content menu trigger did not render");
  await act(async () => {
    contentTrigger.click();
    await flushTimers();
  });
  ok(Boolean(document.querySelector(".composer-content-menu")), "plus trigger opens the add-content menu");
  const initialContentItems = Array.from(document.querySelectorAll<HTMLButtonElement>(".composer-content-menu__item"));
  eq(initialContentItems.length, 4, "add-content menu exposes four focused actions");
  const contentItemIcons = initialContentItems.map((item) => item.querySelector("svg")?.getAttribute("class") ?? "");
  ok(contentItemIcons[0]?.includes("lucide-file-plus"), "attachment action uses the file attachment icon");
  ok(contentItemIcons[1]?.includes("lucide-at-sign"), "workspace action uses the mention icon");
  ok(contentItemIcons[2]?.includes("lucide-hash"), "recent-session action uses the history reference icon");
  eq(initialContentItems[3]?.querySelector(".composer-content-menu__trigger-icon")?.textContent, "/", "command action uses the literal slash trigger icon");
  ok(!document.querySelector(".composer-content-menu__divider"), "add-content actions remain one unified group without a divider");
  ok(initialContentItems.every((item) => !item.querySelector("kbd")), "add-content actions do not duplicate their trigger icons on the right");

  const attachmentButton = initialContentItems[0];
  const fileInput = document.querySelector(".composer-content-file-input") as HTMLInputElement | null;
  if (!attachmentButton || !fileInput) throw new Error("attachment picker controls did not render");
  await act(async () => {
    attachmentButton.click();
    Object.defineProperty(fileInput, "files", { configurable: true, value: [new File(["notes"], "notes.txt", { type: "text/plain" })] });
    fileInput.dispatchEvent(new Event("change", { bubbles: true }));
    await flushTimers();
  });
  await waitFor("attachment chosen from add-content menu", () => savedFiles === 1);
  eq(savedFiles, 1, "attachment action reuses the existing file-save path");

  await replaceComposerDraft(rerender, 3001, "@");
  await waitFor("workspace menu before plus toggle", () => Boolean(document.querySelector(".slashmenu")));
  await act(async () => {
    contentTrigger.click();
    await flushTimers();
  });
  ok(!document.querySelector(".slashmenu"), "opening add-content closes the active suggestion panel");
  ok(Boolean(document.querySelector(".composer-content-menu")), "add-content remains the only open composer surface");

  const sessionButton = document.querySelectorAll<HTMLButtonElement>(".composer-content-menu__item")[2];
  if (!sessionButton) throw new Error("recent-session action did not render");
  await act(async () => {
    sessionButton.click();
    await flushTimers();
  });
  await waitFor("direct recent-session picker", () => Boolean(document.querySelector(".slashmenu__search")));
  eq(textarea.value, "@ #", "recent-session action inserts # at the remembered caret");
  ok(!document.querySelector(".composer-content-menu"), "recent-session picker replaces the add-content menu");
  const sessionSearch = document.querySelector(".slashmenu__search") as HTMLInputElement | null;
  if (!sessionSearch) throw new Error("recent-session search did not render");
  await act(async () => {
    sessionSearch.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  eq(textarea.value, "@ #", "Escape closes the direct recent-session picker and keeps the inserted # trigger");

  await replaceComposerDraft(rerender, 3002, "");
  await act(async () => {
    contentTrigger.click();
    await flushTimers();
  });
  const commandButton = document.querySelectorAll<HTMLButtonElement>(".composer-content-menu__item")[3];
  if (!commandButton) throw new Error("command action did not render");
  await act(async () => {
    commandButton.click();
    await flushTimers();
  });
  eq(textarea.value, "/", "command action inserts / at the caret");
  await waitFor("slash menu from add-content action", () => Boolean(document.querySelector(".slashmenu")));

  await replaceComposerDraft(rerender, 3003, "existing text");
  await act(async () => {
    contentTrigger.click();
    await flushTimers();
  });
  const disabledCommandButton = document.querySelectorAll<HTMLButtonElement>(".composer-content-menu__item")[3];
  if (!disabledCommandButton) throw new Error("command action did not render for non-empty input");
  ok(disabledCommandButton.disabled, "command action is disabled while the composer has text");
  await act(async () => {
    disabledCommandButton.click();
    await flushTimers();
  });
  eq(textarea.value, "existing text", "disabled command action does not insert / into existing text");

  await rerender({ running: true });
  await waitFor("content menu closes when a run starts", () => !document.querySelector(".composer-content-menu"));
  await rerender({ running: false });
  ok(!document.querySelector(".composer-content-menu"), "content menu stays closed after the run ends");

  await replaceComposerDraft(rerender, 3004, "");
  await act(async () => {
    contentTrigger.click();
    await flushTimers();
  });
  const runningSessionButton = document.querySelectorAll<HTMLButtonElement>(".composer-content-menu__item")[2];
  if (!runningSessionButton) throw new Error("recent-session action did not render before running");
  await act(async () => {
    runningSessionButton.click();
    await flushTimers();
  });
  await waitFor("recent-session picker before running", () => Boolean(document.querySelector(".slashmenu__search")));
  await rerender({ running: true });
  await waitFor("recent-session picker closes when a run starts", () => !document.querySelector(".slashmenu__search"));
  await rerender({ running: false });
  ok(!document.querySelector(".slashmenu__search"), "recent-session picker stays closed after the run ends");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  // An entity-only submit while a turn is running must queue as guidance,
  // rendered with its slash form — not be dropped silently while
  // clearSubmittedDraft wipes the composer.
  const dom = installDom();
  mockApp({
    Commands: async () => [
      { name: "superpowers:writing-plans", description: "Write a plan", kind: "skill", plugin: "superpowers" },
    ],
    ListDirForTab: async () => [],
    SearchFileRefsForTab: async () => [],
  });
  const { root, calls, rerender } = await renderComposer();
  await replaceComposerDraft(rerender, 4000, "/writing-plans");
  await waitFor("skill menu for the running-queue entity", () => Boolean(document.querySelector(".slashmenu")));
  const queueTextarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!queueTextarea) throw new Error("composer textarea did not render");
  await act(async () => {
    queueTextarea.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  const queueRichInput = document.querySelector(".composer__rich-input") as HTMLDivElement | null;
  if (!queueRichInput) throw new Error("rich composer did not render for the running-queue entity");

  await rerender({ running: true });
  await act(async () => {
    queueRichInput.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  eq(calls.send.length, 0, "an entity-only submit while running queues instead of sending");
  ok(
    document.querySelector(".composer-guidance-item__text")?.textContent?.includes("/superpowers:writing-plans") === true,
    "the queued guidance shows the entity's slash form instead of dropping it silently",
  );
  ok(document.querySelector(".composer__rich-input") === null, "queueing an entity-only submit clears the draft");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  // While an IME is composing, the rich input must neither resync the model
  // nor restore the DOM selection (removeAllRanges cancels or commits an
  // in-progress composition); compositionend performs the one authoritative
  // sync.
  const dom = installDom();
  mockApp({
    Commands: async () => [
      { name: "superpowers:writing-plans", description: "Write a plan", kind: "skill", plugin: "superpowers" },
    ],
    ListDirForTab: async () => [],
    SearchFileRefsForTab: async () => [],
  });
  const { root, calls, rerender } = await renderComposer();
  await replaceComposerDraft(rerender, 4100, "/writing-plans");
  await waitFor("skill menu for the composition guard", () => Boolean(document.querySelector(".slashmenu")));
  const compositionTextarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
  if (!compositionTextarea) throw new Error("composer textarea did not render");
  await act(async () => {
    compositionTextarea.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  const compositionRichInput = document.querySelector(".composer__rich-input") as HTMLDivElement | null;
  if (!compositionRichInput) throw new Error("rich composer did not render for the composition guard");

  // Drain the entity-pick flow's pending animation frames (imperative caret
  // restore) so the spy below counts only composition-window work.
  await act(async () => {
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    await flushTimers();
  });
  const domSelection = document.getSelection();
  if (!domSelection) throw new Error("document selection unavailable");
  let selectionStomps = 0;
  const originalRemoveAllRanges = domSelection.removeAllRanges.bind(domSelection);
  (domSelection as { removeAllRanges: () => void }).removeAllRanges = () => {
    selectionStomps += 1;
    originalRemoveAllRanges();
  };
  await act(async () => {
    compositionRichInput.dispatchEvent(new window.Event("compositionstart", { bubbles: true }));
    compositionRichInput.appendChild(document.createTextNode("拼"));
    compositionRichInput.dispatchEvent(new window.Event("input", { bubbles: true }));
    await flushTimers();
  });
  eq(selectionStomps, 0, "composition input neither resyncs the model nor restores the selection");
  await act(async () => {
    compositionRichInput.dispatchEvent(new window.Event("compositionend", { bubbles: true }));
    await flushTimers();
  });
  (domSelection as { removeAllRanges: () => void }).removeAllRanges = originalRemoveAllRanges;

  const compositionSendButton = document.querySelector(".composer__btn--send") as HTMLButtonElement | null;
  if (!compositionSendButton) throw new Error("send button did not render after composition");
  await act(async () => {
    compositionSendButton.click();
    await flushTimers();
  });
  eq(calls.structured[0]?.input, "拼", "compositionend commits the composed text to the model exactly once");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
