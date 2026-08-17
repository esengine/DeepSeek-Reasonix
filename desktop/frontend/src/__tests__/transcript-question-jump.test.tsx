// Run: tsx src/__tests__/transcript-question-jump.test.tsx

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
    items.push({ kind: "user", id: `u${i}`, text: `question ${i}` });
    items.push({ kind: "assistant", id: `a${i}`, text: `answer ${i}`, reasoning: "", streaming: false });
  }
  return items;
}

console.log("\ntranscript question jump");

{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    HTMLElement.prototype.scrollIntoView = () => {};
    await harness.render(turns(40), { running: false, questionNavigator: true });
    await harness.settle();

    const jumpItems = harness.container.querySelectorAll<HTMLButtonElement>(".jump-item");
    ok(jumpItems.length === 40, "long transcript exposes every question marker");
    const jumpBar = harness.container.querySelector<HTMLElement>(".jump-bar");
    const jumpScroll = harness.container.querySelector<HTMLElement>(".jump-scroll");
    if (!jumpBar || !jumpScroll) throw new Error("question jump bar is not mounted");
    jumpBar.getBoundingClientRect = () => ({ top: 0, bottom: 240, left: 0, right: 56, height: 240, width: 56 } as DOMRect);
    jumpItems.forEach((item, index) => {
      item.getBoundingClientRect = () => ({ top: index * 20, bottom: index * 20 + 14, left: 0, right: 32, height: 14, width: 32 } as DOMRect);
    });

    const targetIndices = [3, 31, 7, 36, 12, 28];
    for (const targetIndex of targetIndices) {
      await act(async () => {
        jumpScroll.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, button: 0, clientY: targetIndex * 20 + 7 }));
        jumpScroll.dispatchEvent(new MouseEvent("click", { bubbles: true, button: 0, clientY: targetIndex * 20 + 7 }));
        harness.container.querySelector<HTMLElement>(".transcript")?.dispatchEvent(new Event("scroll", { bubbles: true }));
      });
      await harness.settle();
      const target = harness.container.querySelector(`#question-anchor-u${targetIndex}`);
      ok(Boolean(target), `jump ${targetIndex + 1} mounts the selected question`);
    }
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
