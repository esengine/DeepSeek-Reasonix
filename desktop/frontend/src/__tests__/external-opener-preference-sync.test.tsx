// Run: tsx src/__tests__/external-opener-preference-sync.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";

import { ExternalOpener, type ExternalOpenerBridge } from "../components/ExternalOpener";
import { LocaleProvider } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";
import type { ExternalOpenersView } from "../lib/types";

let passed = 0;
let failed = 0;
function ok(value: boolean, label: string) {
  process.stdout.write(`  ${value ? "PASS" : "FAIL"}  ${label}\n`);
  if (value) passed += 1;
  else failed += 1;
}
const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));
const dom = new JSDOM("<!doctype html><html><body></body></html>", {
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

const openers = [
  { id: "finder", name: "Finder", kind: "file-manager" as const },
  { id: "xcode", name: "Xcode", kind: "editor" as const },
];

function createHarness(bridge: ExternalOpenerBridge) {
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  const renderTab = async (tabId: string) => {
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ToastProvider>
            <ExternalOpener key={tabId} tabId={tabId} dismissSignal={0} bridge={bridge} />
          </ToastProvider>
        </LocaleProvider>,
      );
      await flush();
    });
  };
  const choose = async (name: string) => {
    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[aria-haspopup="menu"]')
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await flush();
    });
    await act(async () => {
      Array.from(container.querySelectorAll<HTMLButtonElement>('button[role="menuitemradio"]'))
        .find((button) => button.textContent?.includes(name))
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await flush();
    });
  };
  const primaryLabel = () => container.querySelector<HTMLButtonElement>(".external-opener__primary")?.ariaLabel;
  const unmount = async () => {
    await act(async () => root.unmount());
    container.remove();
  };
  return { container, renderTab, choose, primaryLabel, unmount };
}

console.log("\nexternal opener preference synchronization");

let preferred = "finder";
let resolveOlderLaunch: (() => void) | undefined;
const launches: string[] = [];
const completionBridge: ExternalOpenerBridge = {
  async ExternalOpenersForTab() { return { openers, preferred, workspaceOpenable: true }; },
  async SetPreferredExternalOpener(id) { preferred = id; },
  async OpenWorkspaceInExternalOpenerForTab(tabId, id) {
    launches.push(`${tabId}:${id}`);
    if (tabId === "older") await new Promise<void>((resolve) => { resolveOlderLaunch = resolve; });
  },
};
const completion = createHarness(completionBridge);
await completion.renderTab("older");
await completion.choose("Xcode");
await completion.renderTab("visible");
ok(completion.primaryLabel()?.includes("Finder") === true, "the visible tab initially discovers the old preference");
await act(async () => { resolveOlderLaunch?.(); await flush(); });
ok(preferred === "xcode" && completion.primaryLabel()?.includes("Xcode") === true, "an older tab completion synchronizes the visible control");
await act(async () => {
  completion.container.querySelector<HTMLButtonElement>(".external-opener__primary")
    ?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  await flush();
});
ok(launches.at(-1) === "visible:xcode", "the visible primary action uses the synchronized preference");
await completion.unmount();

preferred = "finder";
let resolveSecondOlderLaunch: (() => void) | undefined;
let resolveStaleDiscovery: ((view: ExternalOpenersView) => void) | undefined;
const staleDiscoveryBridge: ExternalOpenerBridge = {
  async ExternalOpenersForTab(tabId) {
    if (tabId !== "visible") return { openers, preferred, workspaceOpenable: true };
    return new Promise<ExternalOpenersView>((resolve) => { resolveStaleDiscovery = resolve; });
  },
  async SetPreferredExternalOpener(id) { preferred = id; },
  async OpenWorkspaceInExternalOpenerForTab(tabId) {
    if (tabId === "older") await new Promise<void>((resolve) => { resolveSecondOlderLaunch = resolve; });
  },
};
const staleDiscovery = createHarness(staleDiscoveryBridge);
await staleDiscovery.renderTab("older");
await staleDiscovery.choose("Xcode");
await staleDiscovery.renderTab("visible");
await act(async () => { resolveSecondOlderLaunch?.(); await flush(); });
await act(async () => {
  resolveStaleDiscovery?.({ openers, preferred: "finder", workspaceOpenable: true });
  await flush();
});
ok(staleDiscovery.primaryLabel()?.includes("Xcode") === true, "a discovery started before the write cannot restore the stale preference");
await staleDiscovery.unmount();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
