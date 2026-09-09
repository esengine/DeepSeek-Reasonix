// Run: tsx src/__tests__/topicbar-session-actions.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { TopicbarSessionActions } from "../components/TopicbarSessionActions";
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

console.log("\ntopicbar direct session actions and export navigation");
const rootElement = document.getElementById("root")!;
const root = createRoot(rootElement);
const calls: string[] = [];
Object.defineProperty(navigator, "clipboard", { configurable: true, value: {
  writeText: async (value: string) => { calls.push(`copy:${value}`); },
} });

async function render(terminalEnabled = true, sessionHasContent = true, tabID = "one") {
  await act(async () => {
    root.render(<LocaleProvider><TopicbarSessionActions
      key={tabID} sessionHasContent={sessionHasContent}
      getSessionMarkdown={async () => "# Session"}
      exportSession={(format) => { calls.push(`export:${format}`); }}
      toggleTerminal={() => { calls.push("terminal"); }} terminalOpen={false}
      terminalEnabled={terminalEnabled} prefetchTerminal={() => { calls.push("prefetch"); }}
      openSessionSummary={() => { calls.push("summary"); }} tasksOpen={false}
    /></LocaleProvider>);
  });
}
await render();
const buttons = Array.from(rootElement.querySelectorAll<HTMLButtonElement>("button"));
ok(buttons.length === 4 && !rootElement.querySelector('[role="menu"]'), "all four session actions are directly available without opening a menu");
const [copy, trigger, terminal, summary] = buttons as [HTMLButtonElement, HTMLButtonElement, HTMLButtonElement, HTMLButtonElement];
await act(async () => { copy.click(); terminal.click(); summary.click(); });
ok(calls.includes("copy:# Session") && calls.includes("terminal") && calls.includes("summary"), "one click invokes each direct action, including asynchronous copy");
await act(async () => { terminal.focus(); });
ok(calls.includes("prefetch"), "focusing the terminal action prefetches its panel");

async function openExport(key?: string) {
  await act(async () => { if (key) press(trigger, key); else trigger.click(); });
  await waitFor("export formats", () => rootElement.querySelector('[role="menu"]') !== null);
  return Array.from(rootElement.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'));
}
let items = await openExport();
ok(items.length === 4 && document.activeElement === items[0], "one export click opens all four formats and focuses the first");
await act(async () => { press(items[0]!, "ArrowUp"); });
ok(document.activeElement === items[3], "ArrowUp wraps to the last format");
await act(async () => { press(items[3]!, "Home"); });
ok(document.activeElement === items[0], "Home focuses the first format");
await act(async () => { press(items[0]!, "ArrowDown"); });
ok(document.activeElement === items[1], "ArrowDown advances to the next format");
await act(async () => { press(items[1]!, "End"); });
ok(document.activeElement === items[3], "End focuses the last format");
await act(async () => { press(items[3]!, "Escape"); });
ok(!rootElement.querySelector('[role="menu"]') && document.activeElement === trigger, "Escape closes export and restores trigger focus");
items = await openExport("ArrowUp");
ok(document.activeElement === items[3], "ArrowUp on the export trigger opens at the final format");
await act(async () => { items[3]!.click(); });
ok(calls.includes("export:image") && !rootElement.querySelector('[role="menu"]') && document.activeElement === trigger, "format selection dispatches export, closes the menu, and restores focus");
for (const [index, format] of ["markdown", "json", "pdf"].entries()) {
  items = await openExport("ArrowDown");
  await act(async () => { items[index]!.click(); });
  ok(calls.includes(`export:${format}`), `${format} export retains its callback`);
}
await openExport();
await act(async () => { summary.focus(); });
ok(!rootElement.querySelector('[role="menu"]') && document.activeElement === summary, "moving focus outside export dismisses it without stealing focus");
await openExport();
await act(async () => { document.body.dispatchEvent(new dom.window.MouseEvent("pointerdown", { bubbles: true })); });
ok(!rootElement.querySelector('[role="menu"]'), "pointer interaction outside dismisses export");
await openExport();
await render(true, true, "two");
ok(!rootElement.querySelector('[role="menu"]'), "switching sessions clears the previous export menu");
await render(false, false, "two");
const disabledButtons = Array.from(rootElement.querySelectorAll<HTMLButtonElement>("button"));
ok(disabledButtons[1]!.disabled && disabledButtons[2]!.disabled, "empty sessions disable export and remote surfaces disable terminal");
const callCount = calls.length;
await act(async () => { disabledButtons[1]!.click(); disabledButtons[2]!.click(); disabledButtons[2]!.focus(); });
ok(calls.length === callCount && !rootElement.querySelector('[role="menu"]'), "disabled actions neither run nor prefetch");
await act(async () => { root.unmount(); });
dom.window.close();
console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
