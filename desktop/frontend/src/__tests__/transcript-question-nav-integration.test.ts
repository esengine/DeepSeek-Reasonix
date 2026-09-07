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

const race = await createTranscriptHarness({ deterministic: true, viewportHeight: 200, rowHeight: 80 });
try {
  const calls: string[] = [];
  const writes: unknown[] = [];
  window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => { writes.push(write); };
  const page = turns(8).slice(4);
  const base = { questionNavigator: true, hasOlderHistory: true, historyStartTurn: 5, historyTotalTurns: 8 };
  let finish!: (loaded: boolean) => void;
  const pending = () => new Promise<boolean>((resolve) => { finish = resolve; });
  const jumpHome = async () => {
    await race.waitFor(() => Boolean(race.container.querySelector('[role="slider"]')), "question rail module");
    await act(async () => race.container.querySelector('[role="slider"]')!
      .dispatchEvent(new KeyboardEvent("keydown", { key: "Home", bubbles: true })));
    await race.flush();
  };
  await race.render(page, { ...base, geometrySessionKey: "A", onLoadOlderHistory: (turn: number) => { calls.push(`A:${turn}`); return pending(); } });
  await race.settle();
  await jumpHome();
  ok(calls.join() === "A:1", "unloaded navigation requests its source session's target page");
  await race.render(page.map((item) => ({ ...item, id: `B-${item.id}` })), {
    ...base, geometrySessionKey: "B", onLoadOlderHistory: (turn: number) => { calls.push(`B:${turn}`); return false; },
  });
  await race.settle();
  const before = writes.length;
  await act(async () => { finish(false); });
  await race.settle();
  ok(!race.container.querySelector("[data-question-jump-mask]"), "A failure cannot leave B's navigation pending");
  ok(calls.join() === "A:1" && writes.length === before, "A completion neither requests its turn in B nor writes B's viewport");

  for (const gesture of ["wheel", "touchstart", "mousedown"] as const) {
    await race.render(page, { ...base, geometrySessionKey: gesture,
      onLoadOlderHistory: () => pending() });
    await race.settle();
    await jumpHome();
    const scroller = race.scrollElement();
    const count = writes.length;
    await act(async () => {
      scroller.dispatchEvent(gesture === "wheel" ? new WheelEvent("wheel", { deltaY: -40, bubbles: true })
        : gesture === "mousedown" ? new MouseEvent("mousedown", { clientX: 799, bubbles: true })
        : new Event("touchstart", { bubbles: true }));
      finish(true);
    });
    await race.render(turns(8), { ...base, geometrySessionKey: gesture, hasOlderHistory: false });
    await race.settle();
    ok(writes.length === count && !race.container.querySelector("[data-question-jump-mask]"),
      `${gesture} during paging accepts zero program writes after loaded target arrives`);
  }
  const painted: string[] = [];
  const onSurfacePaintReady = (token: string) => { painted.push(token); };
  const flushFrames = race.clock.flushFrames.bind(race.clock);
  race.clock.flushFrames = () => {};
  await race.render(turns(8), { geometrySessionKey: "paint-A", surfaceCommitToken: "paint-A", onSurfacePaintReady });
  const oldFrames = [...race.clock.frames.values()];
  const oldObservers = race.observers.map((observer) => observer.notify);
  await race.render(turns(8), { geometrySessionKey: "paint-B", surfaceCommitToken: "paint-B", onSurfacePaintReady });
  const beforeStale = writes.length;
  await act(async () => { oldFrames.forEach((callback) => callback(0)); oldObservers.forEach((notify) => notify()); });
  ok(!painted.includes("paint-A") && writes.length === beforeStale, "queued A paint and disconnected observers cannot commit A's surface or write B");
  race.clock.flushFrames = flushFrames;
  await race.settle();
  ok(painted.join() === "paint-B", "only the correctly painted current generation confirms its surface token");
} finally {
  await race.unmount();
  await race.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
