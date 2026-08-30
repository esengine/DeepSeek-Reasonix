// Run: tsx src/__tests__/transcript-live-turn-stability.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import {
  buildTranscriptRows,
  buildTurnModels,
  EMPTY_FOLDS,
  type BuildRowsOptions,
} from "../lib/transcriptRows";
import { useTranscriptLiveTurnStability } from "../lib/useTranscriptLiveTurnStability";
import { useTranscriptScrollArbiter } from "../lib/useTranscriptScrollArbiter";
import type { Item } from "../lib/useController";

let passed = 0;
let failed = 0;

function check(condition: unknown, label: string) {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\ntranscript live-turn stability lifecycle");

const dom = new JSDOM(`<!doctype html><html><body>
  <div id="root"></div><div id="resize-root"></div>
  <div id="scroll">
    <div class="transcript__live-region"><div class="transcript__live-content"></div></div>
  </div>
</body></html>`, { pretendToBeVisual: true });
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Element = dom.window.Element;
globalThis.Node = dom.window.Node;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

const scrollElement = document.getElementById("scroll") as HTMLElement;
const liveRegion = scrollElement.querySelector<HTMLElement>(".transcript__live-region")!;
const liveContent = scrollElement.querySelector<HTMLElement>(".transcript__live-content")!;
let naturalHeight = 0;
const rect = (height: number): DOMRect => ({
  x: 0, y: 0, top: 0, right: 800, bottom: height, left: 0, width: 800, height, toJSON: () => ({}),
});
liveContent.getBoundingClientRect = () => rect(naturalHeight);
liveRegion.getBoundingClientRect = () => rect(naturalHeight);

const options: BuildRowsOptions = {
  folds: EMPTY_FOLDS,
  foldPreference: "auto",
  hasOlderHistory: false,
  creationMode: false,
  turnForUser: () => 0,
};

function fixture(answerCount: number) {
  const items: Item[] = [{ kind: "user", id: "u1", text: "question" }];
  for (let index = 0; index < answerCount; index += 1) {
    items.push({
      kind: "assistant",
      id: `a${index + 1}`,
      text: `answer ${index + 1}`,
      reasoning: "",
      streaming: index === 0,
    });
  }
  const live = { id: "a1", hasAnswerText: true, hasReasoning: false };
  const turnModels = buildTurnModels(items, live, true, false);
  return { turnModels, rows: buildTranscriptRows(turnModels, options) };
}

const tailOwnedRef = { current: false };
function Probe({ surfaceKey, userResizeRevision, answerCount }: {
  surfaceKey: string;
  userResizeRevision: number;
  answerCount: number;
}) {
  const { turnModels, rows } = fixture(answerCount);
  const { liveMinHeight } = useTranscriptLiveTurnStability({
    turnModels,
    rows,
    liveId: "a1",
    running: true,
    stabilityKey: `${surfaceKey}:${userResizeRevision}`,
    scrollElement,
    hydrating: false,
    tailOwnedRef,
    pinLiveTailBeforePaint: () => false,
  });
  return <output data-floor={liveMinHeight ?? "none"} />;
}

const root = createRoot(document.getElementById("root")!);
async function render(surfaceKey: string, userResizeRevision: number, answerCount: number, height: number) {
  naturalHeight = height;
  await act(async () => root.render(
    <Probe surfaceKey={surfaceKey} userResizeRevision={userResizeRevision} answerCount={answerCount} />,
  ));
  return document.querySelector("output")?.getAttribute("data-floor");
}

await render("tab-a:1", 0, 1, 640);
check(await render("tab-a:1", 0, 2, 240) === "640", "row growth holds the previous live height");
check(await render("tab-b:1", 0, 1, 360) === "none", "an active tab switch drops the previous surface floor");
check(await render("tab-b:1", 0, 2, 180) === "360", "the new surface establishes its own independent floor");
check(await render("tab-b:1", 1, 1, 140) === "none", "an explicit user collapse releases the held floor immediately");

let resizeIntent: ReturnType<typeof useTranscriptScrollArbiter> | undefined;
function ResizeIntentProbe() {
  resizeIntent = useTranscriptScrollArbiter();
  return <output id="resize-revision" data-revision={resizeIntent.userResizeRevision} />;
}
const resizeRoot = createRoot(document.getElementById("resize-root")!);
await act(async () => resizeRoot.render(<ResizeIntentProbe />));
await act(async () => resizeIntent?.beginUserResize());
check(
  document.getElementById("resize-revision")?.getAttribute("data-revision") === "1",
  "the shared user-resize owner advances the live-height revision",
);

await act(async () => root.unmount());
await act(async () => resizeRoot.unmount());
dom.window.close();

if (failed > 0) {
  console.error(`\n${failed} live-turn stability lifecycle test(s) failed; ${passed} passed.`);
  process.exit(1);
}
console.log(`\n${passed} live-turn stability lifecycle tests passed.`);
