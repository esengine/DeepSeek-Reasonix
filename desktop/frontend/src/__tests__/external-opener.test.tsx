// Run: tsx src/__tests__/external-opener.test.tsx

import { readFileSync } from "node:fs";
import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";

import { ExternalOpener, shouldMountExternalOpener, type ExternalOpenerBridge } from "../components/ExternalOpener";
import { LocaleProvider, t } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";
import type { ExternalOpenersView } from "../lib/types";

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

function flush(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });

const selected: string[] = [];
const opened: Array<[string, string]> = [];
const nativeIcon = "data:image/png;base64,iVBORw0KGgo=";
let discoveryCalls = 0;
const bridge: ExternalOpenerBridge = {
  async ExternalOpenersForTab() {
    discoveryCalls += 1;
    return {
      openers: [
        { id: "finder", name: "Finder", kind: "file-manager", iconDataUrl: nativeIcon },
        { id: "ghostty", name: "Ghostty", kind: "terminal" },
        ...(discoveryCalls > 1 ? [{ id: "xcode", name: "Xcode", kind: "editor" as const, iconDataUrl: nativeIcon }] : []),
      ],
      preferred: "finder",
      workspaceOpenable: true,
    };
  },
  async SetPreferredExternalOpener(id) {
    selected.push(id);
  },
  async OpenWorkspaceInExternalOpenerForTab(tabId, id) {
    opened.push([tabId, id]);
  },
};

console.log("\nexternal opener");

const stylesSource = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
const sharedControlRule = stylesSource.match(/(?:^|\n)\.external-opener\s*\{([^}]*)\}/)?.[1] ?? "";
const sharedSegmentRule = stylesSource.match(
  /\.external-opener__primary,\s*\.external-opener__menu-trigger\s*\{([^}]*)\}/,
)?.[1] ?? "";
const creationControlRule = stylesSource.match(
  /\.app--creation \.topicbar__actions \.external-opener,\s*:root\[data-theme-style\] \.app--creation \.topicbar__actions \.external-opener\s*\{([^}]*)\}/,
)?.[1] ?? "";
const sharedIconRule = stylesSource.match(/(?:^|\n)\.external-opener__app-icon\s*\{([^}]*)\}/)?.[1] ?? "";
const creationIconRule = stylesSource.match(
  /\.app--creation \.topicbar__actions \.external-opener__app-icon,\s*:root\[data-theme-style\] \.app--creation \.topicbar__actions \.external-opener__app-icon\s*\{([^}]*)\}/,
)?.[1] ?? "";
ok(
  /height:\s*28px;/.test(sharedControlRule)
    && /border-radius:\s*7px;/.test(sharedControlRule)
    && /box-shadow:\s*none;/.test(sharedControlRule),
  "uses the shared 28px / 7px topic-bar control geometry without an extra shadow",
);
ok(/height:\s*100%;/.test(sharedSegmentRule), "keeps both split-button segments within the shared control height");
ok(
  /height:\s*28px;/.test(creationControlRule) && /border-radius:\s*7px;/.test(creationControlRule),
  "keeps Creation topic bars on the same 28px / 7px geometry",
);
ok(
  /width:\s*18px;/.test(sharedIconRule) && /height:\s*18px;/.test(sharedIconRule),
  "fits Workbench application artwork to the compact topic-bar control",
);
ok(
  /width:\s*15px;/.test(creationIconRule) && /height:\s*15px;/.test(creationIconRule),
  "preserves the slimmer Creation application artwork size",
);

ok(shouldMountExternalOpener({ id: "tab-project", scope: "project" }, false), "mounts for a Project tab");
ok(shouldMountExternalOpener({ id: "tab-global", scope: "global" }, false), "mounts for a Global tab without guessing from scope");
ok(!shouldMountExternalOpener({ id: "tab-global", scope: "global" }, true), "stays hidden while an IM detail surface owns the header");

const container = document.getElementById("root")!;
const root = createRoot(container);
await act(async () => {
  root.render(
    <LocaleProvider>
      <ToastProvider>
        <ExternalOpener tabId="tab-global" dismissSignal={0} bridge={bridge} />
      </ToastProvider>
    </LocaleProvider>,
  );
  await flush();
});

const choose = container.querySelector<HTMLButtonElement>('button[aria-haspopup="menu"]');
ok(Boolean(choose), "renders a split-button menu trigger after discovery");
ok(container.querySelector<HTMLImageElement>(`img[src="${nativeIcon}"]`) != null, "renders the native application icon data URL");

await act(async () => {
  choose?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  await flush();
});
const menu = container.querySelector('[role="menu"]');
ok(Boolean(menu), "opens the installed-application menu");
ok(discoveryCalls === 2, "requests an installed-application refresh whenever the menu opens");
ok(menu?.querySelectorAll('[role="menuitemradio"]').length === 3, "renders applications discovered by the fresh scan");

const ghostty = Array.from(menu?.querySelectorAll<HTMLButtonElement>('button[role="menuitemradio"]') ?? [])
  .find((button) => button.textContent?.includes("Ghostty"));
await act(async () => {
  ghostty?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  await flush();
});
ok(selected.join(",") === "ghostty", "persists the selected application id");
ok(JSON.stringify(opened) === JSON.stringify([["tab-global", "ghostty"]]), "opens the exact Global workspace with the selection");

const primary = container.querySelector<HTMLButtonElement>('button.external-opener__primary');
await act(async () => {
  primary?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  await flush();
});
ok(selected.length === 1, "primary action reuses the preference without another settings write");
ok(opened.at(-1)?.[1] === "ghostty", "primary action uses the newly selected application");

const openedBeforeDoubleClick = opened.length;
await act(async () => {
  primary?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  primary?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  await flush();
});
ok(opened.length === openedBeforeDoubleClick + 1, "rapid primary clicks launch the workspace only once");

await act(async () => root.unmount());

let staleResolve: ((value: ExternalOpenersView) => void) | undefined;
let raceCalls = 0;
const raceBridge: ExternalOpenerBridge = {
  async ExternalOpenersForTab() {
    raceCalls += 1;
    if (raceCalls === 1) {
      return { openers: [{ id: "finder", name: "Finder", kind: "file-manager" }], preferred: "finder", workspaceOpenable: true };
    }
    if (raceCalls === 2) {
      return new Promise<ExternalOpenersView>((resolve) => {
        staleResolve = resolve;
      });
    }
    return { openers: [{ id: "xcode", name: "Xcode", kind: "editor" }], preferred: "xcode", workspaceOpenable: true };
  },
  async SetPreferredExternalOpener() {},
  async OpenWorkspaceInExternalOpenerForTab() {},
};
const raceContainer = document.createElement("div");
document.body.append(raceContainer);
const raceRoot = createRoot(raceContainer);
await act(async () => {
  raceRoot.render(
    <LocaleProvider>
      <ToastProvider>
        <ExternalOpener tabId="race-tab" dismissSignal={0} bridge={raceBridge} />
      </ToastProvider>
    </LocaleProvider>,
  );
  await flush();
});
const raceChoose = raceContainer.querySelector<HTMLButtonElement>('button[aria-haspopup="menu"]')!;
await act(async () => {
  raceChoose.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  await flush();
});
ok(
  raceContainer.querySelector('[role="menu"]') != null && raceContainer.textContent?.includes("Finder") === true,
  "opens immediately with the cached application list while refresh is still running",
);
await act(async () => {
  raceChoose.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  await flush();
});
await act(async () => {
  raceChoose.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  await flush();
});
ok(raceContainer.textContent?.includes("Xcode") === true, "the latest overlapping discovery result wins");
await act(async () => {
  staleResolve?.({ openers: [{ id: "stale", name: "Stale Editor", kind: "editor" }], preferred: "stale", workspaceOpenable: true });
  await flush();
});
ok(raceContainer.textContent?.includes("Xcode") === true && !raceContainer.textContent?.includes("Stale Editor"), "a stale discovery cannot replace the current menu");
await act(async () => raceRoot.unmount());
raceContainer.remove();

const preferenceWrites: string[] = [];
const preferenceLaunches: string[] = [];
let resolveOlderLaunch: (() => void) | undefined;
const preferenceRaceBridge: ExternalOpenerBridge = {
  async ExternalOpenersForTab() {
    return {
      openers: [
        { id: "finder", name: "Finder", kind: "file-manager" },
        { id: "xcode", name: "Xcode", kind: "editor" },
      ],
      preferred: "finder",
      workspaceOpenable: true,
    };
  },
  async SetPreferredExternalOpener(id) {
    preferenceWrites.push(id);
  },
  async OpenWorkspaceInExternalOpenerForTab(tabId, id) {
    preferenceLaunches.push(`${tabId}:${id}`);
    if (tabId === "older-tab") {
      await new Promise<void>((resolve) => {
        resolveOlderLaunch = resolve;
      });
    }
  },
};
const preferenceContainer = document.createElement("div");
document.body.append(preferenceContainer);
const preferenceRoot = createRoot(preferenceContainer);
const renderPreferenceTab = async (
  root: ReturnType<typeof createRoot>,
  targetBridge: ExternalOpenerBridge,
  tabId: string,
) => {
  await act(async () => {
    root.render(
      <LocaleProvider>
        <ToastProvider>
          <ExternalOpener key={tabId} tabId={tabId} dismissSignal={0} bridge={targetBridge} />
        </ToastProvider>
      </LocaleProvider>,
    );
    await flush();
  });
};
const choosePreference = async (targetContainer: HTMLElement, name: string) => {
  await act(async () => {
    targetContainer.querySelector<HTMLButtonElement>('button[aria-haspopup="menu"]')
      ?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await flush();
  });
  await act(async () => {
    Array.from(targetContainer.querySelectorAll<HTMLButtonElement>('button[role="menuitemradio"]'))
      .find((button) => button.textContent?.includes(name))
      ?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await flush();
  });
};
await renderPreferenceTab(preferenceRoot, preferenceRaceBridge, "older-tab");
await choosePreference(preferenceContainer, "Xcode");
await renderPreferenceTab(preferenceRoot, preferenceRaceBridge, "newer-tab");
await choosePreference(preferenceContainer, "Finder");
ok(
  preferenceLaunches.join(",") === "older-tab:xcode,newer-tab:finder" && preferenceWrites.join(",") === "finder",
  "a newer tab selection persists even when it matches the discovered preference",
);
await act(async () => {
  resolveOlderLaunch?.();
  await flush();
});
ok(preferenceWrites.join(",") === "finder", "an older tab completion cannot overwrite the latest opener preference");
await act(async () => preferenceRoot.unmount());
preferenceContainer.remove();

const failedNewerWrites: string[] = [];
let resolveOlderSuccessfulLaunch: (() => void) | undefined;
const failedNewerBridge: ExternalOpenerBridge = {
  async ExternalOpenersForTab() {
    return {
      openers: [
        { id: "finder", name: "Finder", kind: "file-manager" },
        { id: "xcode", name: "Xcode", kind: "editor" },
      ],
      preferred: "finder",
      workspaceOpenable: true,
    };
  },
  async SetPreferredExternalOpener(id) {
    failedNewerWrites.push(id);
  },
  async OpenWorkspaceInExternalOpenerForTab(tabId) {
    if (tabId === "older-success") {
      await new Promise<void>((resolve) => {
        resolveOlderSuccessfulLaunch = resolve;
      });
      return;
    }
    throw new Error("newer launch failed");
  },
};
const failedNewerContainer = document.createElement("div");
document.body.append(failedNewerContainer);
const failedNewerRoot = createRoot(failedNewerContainer);
await renderPreferenceTab(failedNewerRoot, failedNewerBridge, "older-success");
await choosePreference(failedNewerContainer, "Xcode");
await renderPreferenceTab(failedNewerRoot, failedNewerBridge, "newer-failure");
await choosePreference(failedNewerContainer, "Finder");
await act(async () => {
  resolveOlderSuccessfulLaunch?.();
  await flush();
});
ok(
  failedNewerWrites.join(",") === "xcode",
  "a newer failed launch does not cancel an older successful selection",
);
await act(async () => failedNewerRoot.unmount());
failedNewerContainer.remove();

const inFlightWrites: string[] = [];
let markOlderWriteStarted: (() => void) | undefined;
const olderWriteStarted = new Promise<void>((resolve) => {
  markOlderWriteStarted = resolve;
});
let resolveOlderWrite: (() => void) | undefined;
const inFlightWriteBridge: ExternalOpenerBridge = {
  async ExternalOpenersForTab() {
    return {
      openers: [
        { id: "finder", name: "Finder", kind: "file-manager" },
        { id: "xcode", name: "Xcode", kind: "editor" },
      ],
      preferred: "finder",
      workspaceOpenable: true,
    };
  },
  async SetPreferredExternalOpener(id) {
    inFlightWrites.push(id);
    markOlderWriteStarted?.();
    await new Promise<void>((resolve) => {
      resolveOlderWrite = resolve;
    });
  },
  async OpenWorkspaceInExternalOpenerForTab(tabId) {
    if (tabId === "newer-failure") throw new Error("newer launch failed");
  },
};
const inFlightContainer = document.createElement("div");
document.body.append(inFlightContainer);
const inFlightRoot = createRoot(inFlightContainer);
await renderPreferenceTab(inFlightRoot, inFlightWriteBridge, "older-write");
await choosePreference(inFlightContainer, "Xcode");
await olderWriteStarted;
await renderPreferenceTab(inFlightRoot, inFlightWriteBridge, "newer-failure");
await choosePreference(inFlightContainer, "Finder");
await act(async () => {
  resolveOlderWrite?.();
  await flush();
});
ok(
  inFlightWrites.join(",") === "xcode",
  "an in-flight older preference write has the same result when a newer launch fails",
);
await act(async () => inFlightRoot.unmount());
inFlightContainer.remove();

const failureLog: string[] = [];
let failOpen = true;
let failPersist = false;
const failureBridge: ExternalOpenerBridge = {
  async ExternalOpenersForTab() {
    return {
      openers: [
        { id: "finder", name: "Finder", kind: "file-manager" },
        { id: "xcode", name: "Xcode", kind: "editor" },
      ],
      preferred: "finder",
      workspaceOpenable: true,
    };
  },
  async SetPreferredExternalOpener(id) {
    failureLog.push(`persist:${id}`);
    if (failPersist) throw new Error("disk full");
  },
  async OpenWorkspaceInExternalOpenerForTab(_tabId, id) {
    failureLog.push(`open:${id}`);
    if (failOpen) throw new Error("spawn failed");
  },
};
const failureContainer = document.createElement("div");
document.body.append(failureContainer);
const failureRoot = createRoot(failureContainer);
await act(async () => {
  failureRoot.render(
    <LocaleProvider>
      <ToastProvider>
        <ExternalOpener tabId="fail-tab" dismissSignal={0} bridge={failureBridge} />
      </ToastProvider>
    </LocaleProvider>,
  );
  await flush();
});
const clickXcodeMenuItem = async () => {
  await act(async () => {
    failureContainer
      .querySelector<HTMLButtonElement>('button[aria-haspopup="menu"]')
      ?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await flush();
  });
  await act(async () => {
    Array.from(failureContainer.querySelectorAll<HTMLButtonElement>('button[role="menuitemradio"]'))
      .find((button) => button.textContent?.includes("Xcode"))
      ?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await flush();
  });
};
const toastTexts = () =>
  Array.from(failureContainer.querySelectorAll(".toast--error .toast__text")).map((node) => node.textContent);

await clickXcodeMenuItem();
ok(failureLog.join(",") === "open:xcode", "a failed launch never persists the preference");
ok(
  toastTexts().includes(t("externalOpener.failed", { name: "Xcode", error: "spawn failed" })),
  "a failed launch reports the launch error",
);

failOpen = false;
failPersist = true;
failureLog.length = 0;
await clickXcodeMenuItem();
ok(failureLog.join(",") === "open:xcode,persist:xcode", "the application launches before the preference write");
ok(
  toastTexts().includes(t("externalOpener.persistFailed", { name: "Xcode", error: "disk full" })),
  "a failed preference write reports the save error after opening",
);
await act(async () => failureRoot.unmount());
failureContainer.remove();

const unavailableContainer = document.createElement("div");
document.body.append(unavailableContainer);
const unavailableRoot = createRoot(unavailableContainer);
const unavailableBridge: ExternalOpenerBridge = {
  async ExternalOpenersForTab() {
    return {
      openers: [{ id: "finder", name: "Finder", kind: "file-manager" }],
      preferred: "finder",
      workspaceOpenable: false,
    };
  },
  async SetPreferredExternalOpener() {},
  async OpenWorkspaceInExternalOpenerForTab() {},
};
await act(async () => {
  unavailableRoot.render(
    <LocaleProvider>
      <ToastProvider>
        <ExternalOpener tabId="tab-missing-workspace" dismissSignal={0} bridge={unavailableBridge} />
      </ToastProvider>
    </LocaleProvider>,
  );
  await flush();
});
ok(unavailableContainer.querySelector(".external-opener") == null, "hides when the backend reports no local workspace capability");
await act(async () => unavailableRoot.unmount());
unavailableContainer.remove();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
