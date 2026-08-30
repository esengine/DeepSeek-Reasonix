// Run: node --import tsx src/__tests__/transcript-question-nav-integration.test.ts

import { createTranscriptHarness } from "./transcript-dom-harness";
import type { Item } from "../lib/useController";
import { act } from "react";

let passed = 0;
let failed = 0;

function ok(condition: unknown, label: string) {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function turns(count: number): Item[] {
  const items: Item[] = [];
  for (let i = 0; i < count; i += 1) {
    items.push({
      kind: "user",
      id: `u${i}`,
      text: `question ${i}`,
      historyTurn: i + 1,
      checkpointTurn: 1_000 + i,
    });
  }
  return items;
}

console.log("\ntranscript question navigation integration");

const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 20 });
try {
  HTMLElement.prototype.scrollIntoView = () => {};
  await harness.render(turns(8), { running: false, questionNavigator: true });
  await harness.settle();

  const transcript = harness.scrollElement();
  transcript.getBoundingClientRect = () => ({ top: 100, bottom: 300, left: 0, right: 800, height: 200, width: 800 } as DOMRect);
  const setAnchorPositions = (activeTurn: number) => {
    harness.container.querySelectorAll<HTMLElement>("[data-question-anchor]").forEach((anchor, turn) => {
      const top = turn <= activeTurn ? -20 - (activeTurn - turn) * 20 : 40 + (turn - activeTurn) * 20;
      anchor.getBoundingClientRect = () => ({ top: 100 + top, bottom: 120 + top, left: 0, right: 400, height: 20, width: 400 } as DOMRect);
    });
  };

  const dots = () => Array.from(harness.container.querySelectorAll<HTMLElement>(".jump-dot"));
  ok(dots().length === 8, "question navigator renders one marker per question");

  setAnchorPositions(5);
  await act(async () => {
    transcript.dispatchEvent(new Event("scroll"));
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
  ok(dots()[5]?.style.width === "18px", "manual scroll moves the active marker to the visible question");
  ok(dots()[7]?.style.width === "12px", "the old tail marker is no longer active after manual scroll");
  ok(
    harness.container.querySelector<HTMLElement>('.jump-item[data-turn="5"] .jump-dot')?.style.width === "18px",
    "scroll sync uses the absolute question index instead of the unrelated checkpoint turn",
  );
  await harness.settle();
} finally {
  await harness.unmount();
  await harness.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
