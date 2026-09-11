// Run: tsx src/__tests__/todo-panel-lifecycle.test.tsx

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import React, { act } from "react";
import { JSDOM } from "jsdom";
import { createRoot } from "react-dom/client";

import { TodoPanel } from "../components/TodoPanel";
import { LocaleProvider } from "../lib/i18n";

const todoCss = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
assert.match(
  todoCss,
  /\.todo-exit\s*\{[^}]*animation:\s*shelf-in 240ms 900ms ease reverse both;/s,
  "the completion animation holds for 900ms before its 240ms fade",
);

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

interface ScheduledTimer {
  id: number;
  at: number;
  callback: () => void;
}

let now = 0;
let nextTimerId = 1;
let timers: ScheduledTimer[] = [];
Object.defineProperty(window, "setTimeout", {
  configurable: true,
  value: (callback: () => void, delay = 0) => {
    const id = nextTimerId++;
    timers.push({ id, at: now + Number(delay), callback });
    return id;
  },
});
Object.defineProperty(window, "clearTimeout", {
  configurable: true,
  value: (id: number) => {
    timers = timers.filter((timer) => timer.id !== id);
  },
});

function advanceTimersBy(ms: number): void {
  const target = now + ms;
  while (true) {
    timers.sort((a, b) => a.at - b.at || a.id - b.id);
    const timer = timers[0];
    if (!timer || timer.at > target) break;
    timers.shift();
    now = timer.at;
    timer.callback();
  }
  now = target;
}

const host = document.getElementById("root");
if (!host) throw new Error("missing test root");
const root = createRoot(host);
let dismissCount = 0;
const onDismiss = () => { dismissCount += 1; };

await act(async () => {
  root.render(
    <LocaleProvider>
      <TodoPanel
        key="restored"
        stateKey="session:restored\0batch"
        todos={[
          { content: "Inspect", status: "completed" },
          { content: "Verify", status: "completed" },
          { content: "Ship", status: "completed" },
        ]}
        running={false}
        pendingPrompt={false}
        onDismiss={onDismiss}
      />
    </LocaleProvider>,
  );
});
assert.equal(host.querySelector(".prompt-shelf"), null, "a restored completed batch does not reattach above the composer");
assert.equal(timers.length, 0, "a restored completed batch does not schedule a stale fade");

await act(async () => {
  root.render(
    <LocaleProvider>
      <TodoPanel
        key="live"
        stateKey="session:test\0batch"
        todos={[
          { content: "Inspect", status: "completed" },
          { content: "Verify", status: "in_progress" },
          { content: "Ship", status: "pending" },
        ]}
        running
        pendingPrompt={false}
        onDismiss={onDismiss}
      />
    </LocaleProvider>,
  );
});
assert.ok(host.querySelector(".prompt-shelf"), "an incomplete batch renders above the composer");

await act(async () => {
  root.render(
    <LocaleProvider>
      <TodoPanel
        key="live"
        stateKey="session:test\0batch"
        todos={[
          { content: "Inspect", status: "completed" },
          { content: "Verify", status: "completed" },
          { content: "Ship", status: "completed" },
        ]}
        running={false}
        pendingPrompt={false}
        onDismiss={onDismiss}
      />
    </LocaleProvider>,
  );
});
assert.equal(host.textContent?.includes("3/3"), true, "the final completed count remains visible during the hold");
assert.ok(host.querySelector(".todo-exit"), "the completed shelf owns the delayed fade animation");

await act(async () => advanceTimersBy(899));
assert.ok(host.querySelector(".prompt-shelf"), "completion does not exit before the 900ms hold");

await act(async () => advanceTimersBy(1));
assert.ok(host.querySelector(".prompt-shelf"), "fade starts before the completion exit");

await act(async () => advanceTimersBy(239));
assert.ok(host.querySelector(".prompt-shelf"), "completion exit waits for the 240ms fade");
await act(async () => advanceTimersBy(1));
assert.equal(host.querySelector(".prompt-shelf"), null, "the completed shelf exits after its 1.14s completion transition");
assert.equal(dismissCount, 0, "automatic completion does not persist a manual-dismissal record");

await act(async () => root.unmount());
console.log("todo panel lifecycle checks passed");
