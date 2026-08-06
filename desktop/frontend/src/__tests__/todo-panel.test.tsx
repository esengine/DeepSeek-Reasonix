// Run: node --import tsx src/__tests__/todo-panel.test.tsx
//
// TodoPanel logic coverage: status normalization (normalizeTodoStatus), the
// label key mapping (todoStatusLabelKey), and the localStorage open-state
// persistence (loadOpenStates / saveOpenState — 80-key trim, boolean-only
// filtering, corrupt/missing payload handling). All internals are exercised
// through the exported TodoPanel component surface (render + header clicks),
// so no production exports were added.

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { TodoPanel } from "../components/TodoPanel";
import { LocaleProvider } from "../lib/i18n";
import { en } from "../locales/en";
import type { Todo } from "../lib/tools";

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nreasonix todo panel");

const STORAGE_KEY = "todoPanel:openStates";

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.localStorage = dom.window.localStorage;
Object.defineProperty(dom.window.HTMLElement.prototype, "scrollIntoView", {
  configurable: true,
  value(this: HTMLElement) {
    this.setAttribute("data-scrolled-into-view", "true");
  },
});

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);

async function flush() {
  await new Promise((resolve) => setTimeout(resolve, 30));
}

function seedOpenStates(entries: Record<string, unknown>): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(entries));
}

function stored(): Record<string, unknown> {
  return (JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "null") as Record<string, unknown> | null) ?? {};
}

const list = () => document.querySelector(".todobar__list");
const headerButton = () =>
  Array.from(document.querySelectorAll<HTMLButtonElement>(".prompt-shelf__header-button"))
    .find((button) => button.textContent === "Expand" || button.textContent === "Collapse");

async function clickHeaderAction(): Promise<void> {
  const button = headerButton();
  if (!button) throw new Error("missing expand/collapse header button");
  await act(async () => {
    button.click();
    await flush();
  });
}

let mountCounter = 0;
async function renderPanel(stateKey: string, todos: Todo[]): Promise<void> {
  mountCounter += 1;
  await act(async () => {
    root.render(
      <LocaleProvider>
        <TodoPanel key={mountCounter} stateKey={stateKey} todos={todos} onDismiss={() => {}} />
      </LocaleProvider>,
    );
    await flush();
  });
}

// Re-renders the same mounted instance (same key) so state and refs survive.
async function updatePanel(stateKey: string, todos: Todo[]): Promise<void> {
  await act(async () => {
    root.render(
      <LocaleProvider>
        <TodoPanel key={mountCounter} stateKey={stateKey} todos={todos} onDismiss={() => {}} />
      </LocaleProvider>,
    );
    await flush();
  });
}

// ---------------------------------------------------------------------------
// Status normalization and label keys (via rendered rows).
// ---------------------------------------------------------------------------

console.log("\nstatus normalization and label keys");

const statusTodos: Todo[] = [
  { content: "pending task", status: "pending" },
  { content: "in progress task", status: "in_progress", activeForm: "Working on the active form" },
  { content: "done task", status: "completed" },
  { content: "unknown status task", status: "waiting" },
  { content: "whitespace padded completed task", status: " completed " },
  { content: "uppercase completed task", status: "COMPLETED" },
  { content: "empty status task", status: "" },
  { content: "null status task", status: null as unknown as string },
  { content: "sub-step task", status: "pending", level: 1 },
];
const expectedStatuses = [
  "pending", "in_progress", "completed",
  "pending", "completed", "pending", "pending", "pending", "pending",
];
const labelKeyFor = (status: string): string => {
  if (status === "in_progress") return "todo.inProgress";
  if (status === "completed") return "todo.completed";
  return "todo.pending";
};

seedOpenStates({ statusView: true });
await renderPanel("statusView", statusTodos);

const items = Array.from(document.querySelectorAll<HTMLElement>(".todobar__item"));
ok(items.length === statusTodos.length, "all nine todos render as rows");
expectedStatuses.forEach((expected, index) => {
  const item = items[index];
  const statusSpan = item.querySelector(".todobar__status");
  const textSpan = item.querySelector(".todobar__text");
  ok(item.classList.contains(`todobar__item--${expected}`), `todo ${index + 1} normalizes to ${expected} (row class)`);
  ok(statusSpan?.classList.contains(`todobar__status--${expected}`) === true, `todo ${index + 1} normalizes to ${expected} (status class)`);
  eq(
    statusSpan?.textContent,
    en[labelKeyFor(expected) as keyof typeof en],
    `todo ${index + 1} maps to label key ${labelKeyFor(expected)}`,
  );
  ok(
    textSpan?.textContent === (statusTodos[index].activeForm ?? statusTodos[index].content),
    `todo ${index + 1} keeps its text`,
  );
});

ok(items[1].querySelector(".todobar__text")?.textContent === "Working on the active form", "in-progress row prefers activeForm over content");
ok(items[8].classList.contains("todobar__item--sub"), "level-1 todo renders the sub-step marker");
eq(document.querySelector(".prompt-shelf__badges")?.textContent, "1/9", "badge counts only raw completed status (whitespace-padded stays uncounted)");
eq(document.querySelector(".prompt-shelf__meta")?.textContent, "Working on the active form", "shelf meta summarizes the in-progress activeForm");
ok(
  document.querySelector(".todobar__item--in_progress")?.getAttribute("data-scrolled-into-view") === "true",
  "current in-progress row scrolls into view on open",
);

await renderPanel("emptyTodos", []);
ok(list() === null && document.querySelector(".prompt-shelf") === null, "empty todo list renders nothing");

// ---------------------------------------------------------------------------
// loadOpenStates: missing / corrupt / wrong-shaped / non-boolean / over-80.
// ---------------------------------------------------------------------------

console.log("\nopen-state loading");

const oneTodo: Todo[] = [{ content: "single task", status: "pending" }];

async function loadCase(raw: string | null, stateKey: string, label: string, expectOpen: boolean): Promise<void> {
  localStorage.clear();
  if (raw !== null) localStorage.setItem(STORAGE_KEY, raw);
  await renderPanel(stateKey, oneTodo);
  ok(list() !== null === expectOpen, label);
}

await loadCase(null, "s", "missing stored value falls back to closed", false);
await loadCase("{oops", "s", "corrupt JSON falls back to closed", false);
await loadCase('"just a string"', "s", "top-level string payload is rejected", false);
await loadCase("42", "s", "top-level number payload is rejected", false);
await loadCase("null", "s", "top-level null payload is rejected", false);
await loadCase("[1,2,3]", "s", "top-level array payload is rejected", false);
await loadCase('{"s":true}', "s", "boolean true entry opens the panel", true);
await loadCase('{"s":false}', "s", "boolean false entry keeps the panel closed", false);
await loadCase('{"s":"yes"}', "s", "string entry value is filtered out", false);
await loadCase('{"s":true,"a":"x","n":5,"z":null}', "s", "mixed entries keep only boolean values", true);
await loadCase('{"a":true,"s":"yes"}', "s", "non-boolean target entry is dropped even when other booleans exist", false);

const ninetyEntries = Object.fromEntries(Array.from({ length: 90 }, (_, i) => [`k${i}`, true]));
localStorage.clear();
seedOpenStates(ninetyEntries);
await renderPanel("k89", oneTodo);
ok(list() !== null, "over-80 stored entries still load the newest key (no read cap)");
await renderPanel("k0", oneTodo);
ok(list() !== null, "over-80 stored entries still load the oldest key");
await renderPanel("missing", oneTodo);
ok(list() === null, "unknown key falls back to the closed default");

// ---------------------------------------------------------------------------
// saveOpenState: round-trip, 80-key trim, no duplicate keys, all-done collapse.
// ---------------------------------------------------------------------------

console.log("\nopen-state saving");

localStorage.clear();
await renderPanel("s1", oneTodo);
ok(list() === null, "fresh panel starts collapsed");
await clickHeaderAction();
eq(stored(), { s1: true }, "expanding persists the open state");
await renderPanel("s1", oneTodo);
ok(list() !== null, "persisted open state round-trips on remount");
await clickHeaderAction();
eq(stored(), { s1: false }, "collapsing persists the closed state");
await renderPanel("s1", oneTodo);
ok(list() === null, "persisted closed state round-trips on remount");

localStorage.clear();
seedOpenStates(Object.fromEntries(Array.from({ length: 85 }, (_, i) => [`k${i}`, true])));
await renderPanel("panel", oneTodo);
ok(list() === null, "unlisted state key starts collapsed despite a full store");
await clickHeaderAction();
eq(Object.keys(stored()).length, 80, "saving trims the store to the newest 80 keys");
eq(stored().panel, true, "the just-saved key survives the trim");
eq(stored().k84, true, "the newest pre-existing key survives the trim");
ok("k6" in stored(), "the 75th-newest pre-existing key survives the trim");
ok(!("k0" in stored()), "the oldest pre-existing key is dropped by the trim");
ok(!("k5" in stored()), "the 80th-oldest key is dropped by the trim");
await renderPanel("panel", oneTodo);
ok(list() !== null, "trimmed payload still round-trips on remount");
await clickHeaderAction();
eq(stored().panel, false, "re-saving the same key updates it");
eq(Object.keys(stored()).length, 80, "re-saving the same key does not duplicate the entry");

localStorage.clear();
seedOpenStates({ s2: true });
await renderPanel("s2", [{ content: "phase one", status: "pending" }]);
ok(list() !== null, "mixed batch renders open from stored state");
await updatePanel("s2", [
  { content: "phase one", status: "completed" },
  { content: "phase two", status: "completed" },
]);
ok(list() === null, "completing the last todo auto-collapses the panel");
eq(stored().s2, false, "auto-collapse persists the closed state");
await renderPanel("s2", [
  { content: "phase one", status: "completed" },
  { content: "phase two", status: "completed" },
]);
ok(list() === null, "auto-collapsed state stays closed on remount");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
