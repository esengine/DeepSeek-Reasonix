// Run: tsx src/__tests__/command-palette-interactions.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { CommandPalette, type PaletteItem } from "../components/CommandPalette";
import { LocaleProvider } from "../lib/i18n";

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

async function flush() {
  await new Promise((resolve) => setTimeout(resolve, 0));
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
  globalThis.HTMLButtonElement = dom.window.HTMLButtonElement;
  globalThis.Event = dom.window.Event;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
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

async function renderPalette() {
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const calls = { close: 0, run: 0 };
  const items: PaletteItem[] = [{
    id: "cmd-new",
    group: "Commands",
    title: "New session",
    run: () => {
      calls.run += 1;
    },
  }];
  await act(async () => {
    root.render(
      <LocaleProvider>
        <CommandPalette
          open
          onClose={() => {
            calls.close += 1;
          }}
          items={items}
          placeholder="Search"
          emptyText="No results"
        />
      </LocaleProvider>,
    );
    await flush();
  });
  return { root, calls };
}

console.log("\ncommand palette interactions");

{
  const dom = installDom();
  const { root, calls } = await renderPalette();
  const esc = document.querySelector<HTMLButtonElement>(".palette__esc");
  ok(esc instanceof HTMLButtonElement, "header esc keycap is a real close button");

  await act(async () => {
    esc?.click();
    await flush();
  });
  ok(calls.close === 1, "clicking header esc closes the command palette");
  ok(calls.run === 0, "clicking header esc does not run the highlighted command");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const { root, calls } = await renderPalette();
  const esc = document.querySelector<HTMLButtonElement>(".palette__esc");
  if (!esc) throw new Error("missing header esc close button");

  await act(async () => {
    esc.focus();
    esc.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    esc.click();
    await flush();
  });

  ok(calls.close === 1, "Enter on focused header esc closes the command palette");
  ok(calls.run === 0, "Enter on focused header esc does not run the highlighted command");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
