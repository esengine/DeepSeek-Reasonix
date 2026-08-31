// Run: tsx src/__tests__/transcript-tail-clamp-race.test.ts

import type { RefObject } from "react";
import type { VirtuosoHandle } from "react-virtuoso";
import type { TranscriptScrollMode } from "../lib/transcriptScrollArbiter";
import { tailTop } from "../lib/transcriptScrollGeometry";
import { createTranscriptScrollWriter } from "../lib/transcriptScrollWriter";
import { createTranscriptTailSettle } from "../lib/transcriptTailSettle";

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

console.log("\ntranscript tail clamp races");

let nextTimer = 1;
const timers = new Map<number, () => void>();
const scheduleTimer = (callback: () => void) => {
  const id = nextTimer;
  nextTimer += 1;
  timers.set(id, callback);
  return id;
};
const cancelTimer = (id: number) => void timers.delete(id);
const runNextTimer = () => {
  const entry = timers.entries().next().value as [number, () => void] | undefined;
  if (!entry) return false;
  timers.delete(entry[0]);
  entry[1]();
  return true;
};

let nextFrame = 1;
const frames = new Map<number, FrameRequestCallback>();
globalThis.requestAnimationFrame = (callback) => {
  const id = nextFrame;
  nextFrame += 1;
  frames.set(id, callback);
  return id;
};
globalThis.cancelAnimationFrame = (id) => void frames.delete(id);
globalThis.window = {
  setTimeout: (callback: TimerHandler) => scheduleTimer(callback as () => void),
  clearTimeout: cancelTimer,
} as unknown as Window & typeof globalThis;

const pinTargets: number[] = [];
let pinAttempts = 0;
const element = {
  clientHeight: 100,
  scrollHeight: 1_000,
  scrollTop: 892,
  scrollTo: ({ top }: { top?: number }) => {
    pinAttempts += 1;
    pinTargets.push(top ?? -1);
    // Model two late Virtuoso range commits that restore the prior 8px gap.
    // The final bounded jump confirmation reaches the real browser tail.
    if (pinAttempts >= 3) element.scrollTop = top ?? element.scrollTop;
  },
} as HTMLDivElement;
const scrollRef = { current: element } as RefObject<HTMLDivElement | null>;
const modeRef = { current: "tail-follow" } as RefObject<TranscriptScrollMode>;
const generationRef = { current: 1 };
const ownershipEpochRef = { current: 1 };
const geometryRevisionRef = { current: 1 };
const layoutTransientRef = { current: false };
const writer = createTranscriptScrollWriter({
  virtuosoRef: { current: { scrollToIndex: () => {} } as unknown as VirtuosoHandle },
  scrollRef,
  modeRef,
  generationRef,
  ownershipEpochRef,
  geometryRevisionRef,
});

const settle = createTranscriptTailSettle({
  writer,
  scrollRef,
  modeRef,
  generationRef,
  ownershipEpochRef,
  geometryRevisionRef,
  layoutTransientRef,
});

settle.scrollToTail("auto", { source: "jump-bottom", phase: "initial" });
settle.schedule(true, "jump-bottom");
check(runNextTimer(), "explicit jump schedules its first wall-clock confirmation");
check(runNextTimer(), "explicit jump schedules its final wall-clock confirmation");
check(pinAttempts === 3, `late range restoration receives exactly three bounded tail probes (${pinAttempts})`);
check(pinTargets.every((top) => top === tailTop(element)), "every explicit tail probe targets the theoretical native extent");
check(element.scrollHeight - element.scrollTop - element.clientHeight === 0, "the final confirmation clears a transient 8px false bottom");

settle.cancel();
if (failed > 0) {
  console.error(`\n${failed} failed, ${passed} passed`);
  process.exit(1);
}
console.log(`\n${passed} passed`);
