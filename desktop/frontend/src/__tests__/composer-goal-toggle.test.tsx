// Run: tsx src/__tests__/composer-goal-toggle.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { Composer, composerPickFileEntry } from "../components/Composer";
import { LocaleProvider } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";
import type { AppBindings } from "../lib/bridge";
import type { CollaborationMode, ToolApprovalMode, TokenMode } from "../lib/types";

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

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    await act(async () => {
      await flushTimers();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
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

  const stopButton = document.querySelector(".composer-runstatus__stop") as HTMLButtonElement | null;
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

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
