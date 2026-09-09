// Run: tsx src/__tests__/pinned-files.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { PinnedFilesShelf } from "../components/PinnedFilesShelf";
import { WorkspaceTreeMenu } from "../components/WorkspaceTreeMenu";
import { LocaleProvider } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";
import type { AppBindings } from "../lib/bridge";
import type { PinnedFileInfo } from "../lib/pinnedContextBridge";

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

function flushTimers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 10));
}

function installDom() {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(dom.window.navigator, "language", { configurable: true, value: "en-US" });
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.CustomEvent = dom.window.CustomEvent;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.localStorage = dom.window.localStorage;
  return dom;
}

async function run() {
  installDom();
  const rootElement = document.getElementById("root");
  if (!rootElement) throw new Error("root missing");
  const root = createRoot(rootElement);

  let unpinnedPath = "";
  let openedPath = "";
  let pinnedViaMenu = "";
  let shouldFailPin = false;

  (window as unknown as { go: { main: { App: Partial<AppBindings> } } }).go = {
    main: {
      App: {
        UnpinFileForTab: async (_tabId: string, path: string) => {
          unpinnedPath = path;
        },
        OpenWorkspacePathForTab: async (_tabId: string, path: string) => {
          openedPath = path;
        },
        GetPinnedFilesForTab: async (_tabId: string) => {
          return [{ path: "already_pinned.md", sizeBytes: 500, tokenEstimate: 125 }];
        },
        PinFileForTab: async (_tabId: string, path: string) => {
          if (shouldFailPin) {
            throw new Error("file size exceeds maximum pinned file limit");
          }
          pinnedViaMenu = path;
          return { path, sizeBytes: 100, tokenEstimate: 25 };
        },
        ResolveWorkspacePathForTab: async (_tabId: string, path: string) => {
          return `/abs/${path}`;
        },
        RevealWorkspacePathForTab: async () => {},
      },
    },
  };

  // Test 1: PinnedFilesShelf empty rendering
  await act(async () => {
    root.render(
      <LocaleProvider initialLocale="en">
        <ToastProvider>
          <PinnedFilesShelf tabId="tab-1" pinnedFiles={[]} />
        </ToastProvider>
      </LocaleProvider>,
    );
    await flushTimers();
  });
  ok(document.querySelector(".pinned-files-shelf") === null, "PinnedFilesShelf renders null when empty");

  // Test 2: PinnedFilesShelf with files
  const sampleFiles: PinnedFileInfo[] = [
    { path: "docs/api.md", sizeBytes: 1200, tokenEstimate: 300 },
    { path: "schema.sql", sizeBytes: 400, tokenEstimate: 100 },
  ];

  await act(async () => {
    root.render(
      <LocaleProvider initialLocale="en">
        <ToastProvider>
          <PinnedFilesShelf tabId="tab-1" pinnedFiles={sampleFiles} />
        </ToastProvider>
      </LocaleProvider>,
    );
    await flushTimers();
  });

  const shelf = document.querySelector(".pinned-files-shelf");
  ok(shelf !== null, "PinnedFilesShelf renders container when files are present");
  const chips = document.querySelectorAll(".pinned-files-shelf .group");
  ok(chips.length === 2, "PinnedFilesShelf renders correct number of chips");
  ok(chips[0]?.textContent?.includes("api.md") === true, "First chip renders filename");

  // Test unpin click
  const unpinBtn = chips[0]?.querySelector("button");
  if (unpinBtn) {
    await act(async () => {
      unpinBtn.dispatchEvent(new window.MouseEvent("click", { bubbles: true, cancelable: true }));
      await flushTimers();
    });
    ok(unpinnedPath === "docs/api.md", "Clicking unpin button calls app.UnpinFileForTab with correct path");
  }

  // Test open file click
  if (chips[1]) {
    await act(async () => {
      (chips[1] as HTMLElement).dispatchEvent(new window.MouseEvent("click", { bubbles: true, cancelable: true }));
      await flushTimers();
    });
    ok(openedPath === "schema.sql", "Clicking chip opens file via app.OpenWorkspacePathForTab");
  }

  // A file that grew beyond the backend budget remains pinned and surfaces
  // its omission reason directly on the chip.
  const grownError = "file size exceeds the 65536-byte limit";
  await act(async () => {
    root.render(
      <LocaleProvider initialLocale="en">
        <ToastProvider>
          <PinnedFilesShelf
            tabId="tab-1"
            pinnedFiles={[{ path: "grown.log", sizeBytes: 70000, tokenEstimate: 17500, error: grownError }]}
          />
        </ToastProvider>
      </LocaleProvider>,
    );
    await flushTimers();
  });
  const errorChip = document.querySelector<HTMLElement>(".pinned-files-shelf .group");
  ok(errorChip?.title === grownError, "Pinned file chip exposes the backend read error");
  ok(errorChip?.querySelector(`[aria-label="${grownError}"]`) !== null, "Pinned file chip renders a warning icon");

  // Test 3: WorkspaceTreeMenu Pin action for unpinned file
  await act(async () => {
    root.render(
      <LocaleProvider initialLocale="en">
        <ToastProvider>
          <WorkspaceTreeMenu
            target={{ x: 10, y: 10, path: "unpinned_file.ts", isDir: false }}
            workspaceTabId="tab-1"
            isScopeCurrent={() => true}
            onClose={() => {}}
            onAddReference={() => {}}
            onAddFile={() => {}}
          />
        </ToastProvider>
      </LocaleProvider>,
    );
    await flushTimers();
  });

  const menuButtons = Array.from(document.querySelectorAll<HTMLButtonElement>(".workspace-tree-menu button"));
  const pinBtn = menuButtons.find((btn) => btn.textContent?.includes("Pin to Session Context"));
  ok(pinBtn !== undefined, "WorkspaceTreeMenu shows Pin to Session Context for unpinned file");

  if (pinBtn) {
    await act(async () => {
      pinBtn.dispatchEvent(new window.MouseEvent("click", { bubbles: true, cancelable: true }));
      await flushTimers();
    });
    ok(pinnedViaMenu === "unpinned_file.ts", "Clicking Pin calls app.PinFileForTab with target path");
  }

  // Test 4: WorkspaceTreeMenu Pin error surfaces Toast
  shouldFailPin = true;
  await act(async () => {
    root.render(
      <LocaleProvider initialLocale="en">
        <ToastProvider>
          <WorkspaceTreeMenu
            target={{ x: 10, y: 10, path: "too_large.dat", isDir: false }}
            workspaceTabId="tab-1"
            isScopeCurrent={() => true}
            onClose={() => {}}
            onAddReference={() => {}}
            onAddFile={() => {}}
          />
        </ToastProvider>
      </LocaleProvider>,
    );
    await flushTimers();
  });

  const menuButtonsError = Array.from(document.querySelectorAll<HTMLButtonElement>(".workspace-tree-menu button"));
  const pinBtnError = menuButtonsError.find((btn) => btn.textContent?.includes("Pin to Session Context"));
  if (pinBtnError) {
    await act(async () => {
      pinBtnError.dispatchEvent(new window.MouseEvent("click", { bubbles: true, cancelable: true }));
      await flushTimers();
    });
    const toast = document.querySelector(".toast--error");
    ok(toast !== null, "Pinning error displays error toast");
    ok(toast?.textContent?.includes("exceeds maximum") === true, "Toast includes error message");
  }
  shouldFailPin = false;

  // Test 5: WorkspaceTreeMenu Unpin action for already pinned file
  await act(async () => {
    root.render(
      <LocaleProvider initialLocale="en">
        <ToastProvider>
          <WorkspaceTreeMenu
            target={{ x: 10, y: 10, path: "already_pinned.md", isDir: false }}
            workspaceTabId="tab-1"
            isScopeCurrent={() => true}
            onClose={() => {}}
            onAddReference={() => {}}
            onAddFile={() => {}}
          />
        </ToastProvider>
      </LocaleProvider>,
    );
  });
  await act(async () => {
    await flushTimers();
  });

  const menuButtons2 = Array.from(document.querySelectorAll<HTMLButtonElement>(".workspace-tree-menu button"));
  const unpinBtn2 = menuButtons2.find((btn) => btn.textContent?.includes("Unpin from Context"));
  ok(unpinBtn2 !== undefined, "WorkspaceTreeMenu shows Unpin from Context for already pinned file");

  await act(async () => {
    root.unmount();
    await flushTimers();
  });

  if (failed > 0) {
    process.exit(1);
  }
}

void run();
