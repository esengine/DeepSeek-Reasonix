// Run: tsx src/__tests__/question-jump-bar.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { QUESTION_JUMP_MAX_MARKERS, QuestionJumpBar, questionTurnFromRailY, sampledQuestionTurns } from "../components/QuestionJumpBar";
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

function rect(top: number, height: number, width: number): DOMRect {
  return { top, bottom: top + height, left: 0, right: width, height, width } as DOMRect;
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
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.localStorage = dom.window.localStorage;

const questions: QuestionAnchor[] = [
  { id: "u1", text: "first question", turn: 0, loaded: true },
  { id: "u2", text: "second question", turn: 1, loaded: true },
  { id: "u3", text: "third question", turn: 2, loaded: true },
];
const jumps: QuestionAnchor[] = [];
const root = createRoot(document.getElementById("root")!);

function ControlledJumpBar({
  loadedQuestions,
  totalQuestions,
}: {
  loadedQuestions: QuestionAnchor[];
  totalQuestions: number;
}) {
  const [activeTurn, setActiveTurn] = React.useState<number | null>(totalQuestions - 1);
  return (
    <QuestionJumpBar
      loadedQuestions={loadedQuestions}
      totalQuestions={totalQuestions}
      activeTurn={activeTurn}
      onJump={(question) => {
        jumps.push(question);
        setActiveTurn(question.turn);
      }}
    />
  );
}

console.log("\nquestion jump bar");

await act(async () => {
  root.render(
    <LocaleProvider>
      <ControlledJumpBar key="short" loadedQuestions={questions} totalQuestions={3} />
    </LocaleProvider>,
  );
  await flushTimers();
});

let bar = document.querySelector(".jump-bar") as HTMLElement;
let rail = document.querySelector(".jump-scroll") as HTMLElement;
bar.getBoundingClientRect = () => rect(0, 240, 56);
rail.getBoundingClientRect = () => rect(0, 240, 32);

await act(async () => {
  // A real mouse activation emits both events. Only mousedown owns the jump.
  rail.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true, button: 0, clientY: 1 }));
  rail.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, button: 0, clientY: 1, detail: 1 }));
  await flushTimers();
});
eq(jumps.length, 1, "one physical rail click emits one question jump");
eq(jumps[0]?.id, "u1", "the rail maps its top directly to the first question");

await act(async () => {
  rail.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, cancelable: true, key: "ArrowDown" }));
  await flushTimers();
});
eq(jumps.length, 2, "one slider keypress emits one question jump");
eq(jumps[1]?.id, "u2", "slider keyboard navigation advances by one exact question");
eq(rail.getAttribute("aria-valuenow"), "2", "the slider exposes its active absolute question");

const sampled = sampledQuestionTurns(10_000, 6_789);
eq(sampled.length, QUESTION_JUMP_MAX_MARKERS, "very long sessions keep a fixed marker count");
eq(sampled[0], 0, "aggregated markers retain the first question");
eq(sampled.at(-1), 9_999, "aggregated markers retain the last question");
eq(sampled.includes(6_789), true, "aggregated markers retain the exact active question");
eq(questionTurnFromRailY(120, 0, 240, 10_000), 5_000, "rail geometry maps to an absolute turn without reading marker layout");

const longQuestions: QuestionAnchor[] = [questions[0], { id: "u10000", text: "last loaded question", turn: 9_999, loaded: true }];
await act(async () => {
  root.render(
    <LocaleProvider>
      <ControlledJumpBar key="long" loadedQuestions={longQuestions} totalQuestions={10_000} />
    </LocaleProvider>,
  );
  await flushTimers();
});
bar = document.querySelector(".jump-bar") as HTMLElement;
rail = document.querySelector(".jump-scroll") as HTMLElement;
bar.getBoundingClientRect = () => rect(0, 240, 56);
rail.getBoundingClientRect = () => rect(0, 240, 32);
const markers = Array.from(document.querySelectorAll<HTMLElement>(".jump-item"));
eq(markers.length, QUESTION_JUMP_MAX_MARKERS, "10,000 questions render only the bounded marker set");
for (const marker of markers) {
  marker.getBoundingClientRect = () => { throw new Error("marker geometry must not be read"); };
}

const unloadedTurn = 6_789;
await act(async () => {
  const clientY = ((unloadedTurn + 0.5) / 10_000) * 240;
  rail.dispatchEvent(new MouseEvent("mousemove", { bubbles: true, clientY }));
  rail.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true, button: 0, clientY }));
  await flushTimers();
});
eq(jumps.at(-1)?.turn, unloadedTurn, "a long aggregated rail still targets the exact unloaded question");
eq(jumps.at(-1)?.loaded, false, "an unloaded aggregate target keeps the lazy-load contract");

await act(async () => root.unmount());
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
