// Run: tsx src/__tests__/topicbar-more-menu.test.tsx

import { JSDOM } from "jsdom";
import { registerHooks } from "node:module";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { TopicbarMoreMenu } from "../components/TopicbarMoreMenu";
import { LocaleProvider } from "../lib/i18n";

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.endsWith(".css")) {
      return nextResolve("./asset-stub-for-tests.ts", { ...context, parentURL: import.meta.url });
    }
    return nextResolve(specifier, context);
  },
});

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
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(async () => {
      await flushTimers();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
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
globalThis.Event = dom.window.Event;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.PointerEvent = dom.window.MouseEvent as unknown as typeof PointerEvent;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

function press(target: Element, key: string) {
  target.dispatchEvent(new dom.window.KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }));
}

function directMenuItems(menu: HTMLElement): HTMLButtonElement[] {
  return Array.from(menu.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'))
    .filter((item) => item.closest('[role="menu"]') === menu);
}

console.log("\ntopicbar more menu keyboard navigation");

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);

await act(async () => {
  root.render(
    <LocaleProvider>
      <TopicbarMoreMenu
        sessionHasContent
        getSessionMarkdown={() => "# Session"}
        exportSession={() => {}}
        openChangedDock={() => {}}
        toggleTerminal={() => {}}
        openSessionSummary={() => {}}
        tasksOpen={false}
      />
    </LocaleProvider>,
  );
  await flushTimers();
});

const trigger = rootElement.querySelector<HTMLButtonElement>('button[aria-haspopup="menu"]');
if (!trigger) throw new Error("missing more-menu trigger");

await act(async () => {
  trigger.click();
  await flushTimers();
});
await waitFor("lazy menu content", () => rootElement.querySelector('[role="menu"]') !== null);

let menu = rootElement.querySelector<HTMLElement>('.topicbar__more-menu[role="menu"]');
if (!menu) throw new Error("missing more menu");
let items = directMenuItems(menu);
ok(document.activeElement === items[0], "opening the menu moves focus to the first action");

await act(async () => {
  press(items[0]!, "ArrowDown");
});
ok(document.activeElement === items[1], "ArrowDown advances focus to the next action");

await act(async () => {
  press(items[1]!, "End");
});
ok(document.activeElement === items[items.length - 1], "End moves focus to the final action");

await act(async () => {
  press(items[items.length - 1]!, "Home");
});
ok(document.activeElement === items[0], "Home returns focus to the first action");

await act(async () => {
  items[1]?.focus();
  press(items[1]!, "ArrowRight");
  await flushTimers();
});
const exportMenu = rootElement.querySelector<HTMLElement>('.topicbar__more-export-menu[role="menu"]');
if (!exportMenu) throw new Error("missing export submenu");
const exportItems = directMenuItems(exportMenu);
ok(document.activeElement === exportItems[0], "ArrowRight opens the export submenu and focuses its first action");

await act(async () => {
  press(exportItems[0]!, "ArrowDown");
});
ok(document.activeElement === exportItems[1], "submenu ArrowDown stays within the submenu");

await act(async () => {
  press(exportItems[1]!, "ArrowLeft");
  await flushTimers();
});
ok(rootElement.querySelector(".topicbar__more-export-menu") === null, "ArrowLeft closes the export submenu");
ok(document.activeElement === items[1], "closing the export submenu restores focus to its trigger");

await act(async () => {
  press(items[1]!, "Escape");
  await flushTimers();
});
ok(rootElement.querySelector(".topicbar__more-menu") === null, "Escape closes the main menu");
ok(document.activeElement === trigger, "closing the main menu restores focus to the More trigger");

await act(async () => {
  press(trigger, "ArrowUp");
  await flushTimers();
});
await waitFor("keyboard-opened menu", () => rootElement.querySelector(".topicbar__more-menu") !== null);
menu = rootElement.querySelector<HTMLElement>('.topicbar__more-menu[role="menu"]');
if (!menu) throw new Error("missing keyboard-opened menu");
items = directMenuItems(menu);
ok(document.activeElement === items[items.length - 1], "ArrowUp on the trigger opens the menu at its final action");

await act(async () => {
  root.unmount();
  await flushTimers();
});

console.log(`\ntopicbar more menu: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
