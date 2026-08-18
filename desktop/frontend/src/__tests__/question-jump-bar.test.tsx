// Run: tsx src/__tests__/question-jump-bar.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { QuestionJumpBar } from "../components/QuestionJumpBar";
import { LocaleProvider } from "../lib/i18n";
import type { QuestionAnchor } from "../lib/transcriptGrouping";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

function flushTimers(): Promise<void> {
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
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;
Object.defineProperty(dom.window.HTMLElement.prototype, "scrollIntoView", {
  configurable: true,
  value() {},
});

const questions: QuestionAnchor[] = [
  { id: "u1", text: "first question", turn: 0 },
  { id: "u2", text: "second question", turn: 1 },
  { id: "u3", text: "third question", turn: 2 },
];
const jumps: QuestionAnchor[] = [];
const root = createRoot(document.getElementById("root")!);

console.log("\nquestion jump bar");

await act(async () => {
  root.render(
    <LocaleProvider>
      <QuestionJumpBar questions={questions} onJump={(question) => jumps.push(question)} />
    </LocaleProvider>,
  );
  await flushTimers();
});

const rail = document.querySelector(".jump-scroll") as HTMLElement;
await act(async () => {
  // A real mouse activation emits both events. The rail must claim the
  // gesture only once or the second navigation can overwrite the first.
  rail.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true, clientY: 0 }));
  rail.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, clientY: 0, detail: 1 }));
  await flushTimers();
});
eq(jumps.length, 1, "one physical rail click emits one question jump");
eq(jumps[0]?.id, "u1", "the rail keeps the question selected on pointer down");

const second = document.querySelector('[data-turn="1"]') as HTMLButtonElement;
await act(async () => {
  second.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, detail: 0 }));
  await flushTimers();
});
eq(jumps.length, 2, "keyboard activation still emits one question jump");
eq(jumps[1]?.id, "u2", "keyboard activation jumps to the focused question");

await act(async () => root.unmount());
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
