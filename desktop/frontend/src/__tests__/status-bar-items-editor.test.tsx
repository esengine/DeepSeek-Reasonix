// Run: tsx src/__tests__/status-bar-items-editor.test.tsx

import { JSDOM } from "jsdom";
import React, { useState } from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { StatusBarItemsEditor } from "../components/StatusBarItemsEditor";
import { LocaleProvider } from "../lib/i18n";
import { DEFAULT_STATUS_BAR_ITEMS, type StatusBarItemId } from "../lib/statusBarItems";

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

function flush() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

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
globalThis.HTMLButtonElement = dom.window.HTMLButtonElement;
globalThis.HTMLInputElement = dom.window.HTMLInputElement;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
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

let latestItems: StatusBarItemId[] = [];

function Harness() {
  const [items, setItems] = useState<StatusBarItemId[]>([...DEFAULT_STATUS_BAR_ITEMS]);
  latestItems = items;
  return (
    <LocaleProvider>
      <StatusBarItemsEditor
        items={items}
        busy={false}
        onChange={setItems}
        itemLabel={(id) => id}
      />
    </LocaleProvider>
  );
}

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);

console.log("\nstatus bar items editor");

await act(async () => {
  root.render(<Harness />);
  await flush();
});

const expand = document.querySelector<HTMLButtonElement>('button[aria-label="Expand status bar items"]');
ok(expand instanceof HTMLButtonElement, "collapsed editor exposes an accessible expand control");

await act(async () => {
  expand?.click();
  await flush();
});

ok(document.body.textContent?.includes("Shown · 16") === true, "expanded editor labels the visible zone with its count");
ok(document.body.textContent?.includes("Hidden · 0") === true, "expanded editor labels the hidden zone with its count");
ok(document.querySelectorAll('[data-statusbar-drop-zone="hidden"]').length === 1, "hidden zone is an explicit drag target");

const modelRow = document.querySelector<HTMLElement>('[data-statusbar-setting-item="model"]');
const modelToggle = modelRow?.querySelector<HTMLInputElement>('input[type="checkbox"]');
await act(async () => {
  modelToggle?.click();
  await flush();
});

ok(latestItems.length === 15 && !latestItems.includes("model"), "clearing a visible item removes it from the persisted order");
ok(document.body.textContent?.includes("Shown · 15") === true, "visible count updates after hiding an item");
ok(document.body.textContent?.includes("Hidden · 1") === true, "hidden count updates after hiding an item");
ok(document.querySelector('[data-statusbar-drop-zone="hidden"] [data-statusbar-setting-item="model"]') != null, "hidden item moves into the hidden zone");

const showAll = Array.from(document.querySelectorAll<HTMLButtonElement>("button")).find((button) => button.textContent === "Show all");
await act(async () => {
  showAll?.click();
  await flush();
});
ok(latestItems.length === 16 && latestItems.at(-1) === "model", "show all restores hidden items without discarding the current visible order");

const moveWorkspaceDown = document.querySelector<HTMLButtonElement>('button[aria-label="Move workspace down"]');
await act(async () => {
  moveWorkspaceDown?.click();
  await flush();
});
ok(latestItems[0] === "git_branch" && latestItems[1] === "workspace", "keyboard order controls update the visible order");

const restoreDefault = Array.from(document.querySelectorAll<HTMLButtonElement>("button")).find((button) => button.textContent === "Restore default");
await act(async () => {
  restoreDefault?.click();
  await flush();
});
ok(latestItems.every((id, index) => id === DEFAULT_STATUS_BAR_ITEMS[index]), "restore default returns all items to canonical order");

await act(async () => root.unmount());
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
