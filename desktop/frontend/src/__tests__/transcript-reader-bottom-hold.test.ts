// Run: tsx src/__tests__/transcript-reader-bottom-hold.test.ts

import {
  INITIAL_TRANSCRIPT_SCROLL_STATE,
  reduceTranscriptScroll,
  type TranscriptScrollEvent,
  type TranscriptScrollState,
} from "../lib/transcriptScrollArbiter";
import {
  createTranscriptReaderBottomHold,
  MAX_TAIL_MOUNT_HOLD_MS,
  MAX_TAIL_MOUNT_CHECKS,
} from "../lib/transcriptReaderBottomHold";

let passed = 0;
let failed = 0;
const check = (condition: unknown, label: string) => {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
};

console.log("\ntranscript reader bottom hold");

let nextFrame = 1;
const frames = new Map<number, FrameRequestCallback>();
globalThis.requestAnimationFrame = (callback) => {
  const id = nextFrame++;
  frames.set(id, callback);
  return id;
};
globalThis.cancelAnimationFrame = (id) => { frames.delete(id); };

const element = {
  scrollTop: 500,
  scrollHeight: 1_000,
  clientHeight: 500,
  dataset: { transcriptRowCount: "10", transcriptFirstItemIndex: "100" },
  querySelector: () => null,
  querySelectorAll: () => [],
} as unknown as HTMLDivElement;
const stateRef = {
  current: reduceTranscriptScroll(
    { ...INITIAL_TRANSCRIPT_SCROLL_STATE, scrollable: true },
    { type: "NATIVE_SCROLLBAR_BEGIN" },
  ).state,
};
stateRef.current = reduceTranscriptScroll(stateRef.current, { type: "NATIVE_SCROLLBAR_END", canClaimTail: true }).state;
const scrollRef = { current: element };
const generationRef = { current: 7 };
const commands: string[] = [];
const dispatch = (event: TranscriptScrollEvent) => {
  const result = reduceTranscriptScroll(stateRef.current, event);
  stateRef.current = result.state;
  commands.push(...result.commands.map((command) => command.type));
};
const deliverScrollRef: { current: ((target?: HTMLDivElement) => void) | null } = { current: null };
const [, deliver] = createTranscriptReaderBottomHold({
  scrollRef,
  stateRef: stateRef as { current: TranscriptScrollState },
  generationRef,
  deliverScrollRef,
  dispatch,
});
deliverScrollRef.current = () => deliver(element);

deliver(element, true);
for (let index = 0; index <= MAX_TAIL_MOUNT_CHECKS; index += 1) {
  const pending = [...frames.values()];
  frames.clear();
  pending.forEach((callback) => callback(index));
}

check(stateRef.current.mode === "tail-follow", "an acknowledged native bottom cannot remain manual when LAST mount stalls");
check(commands.filter((command) => command === "SCROLL_TO_LAST").length === 1,
  "a stalled LAST mount hands off to exactly one arbiter-owned tail transaction");

// A mounted tail whose native extent changes every frame must spend the same
// total budget, even after the expanding range moves the proven native bottom
// outside the threshold. Otherwise a released thumb can remain manual forever.
let unstableExtent = 1_000;
const unstableTop = 500;
const tailRow = { dataset: { itemIndex: "109" } };
const unstableElement = {
  get scrollTop() { return unstableTop; },
  get scrollHeight() { return unstableExtent; },
  clientHeight: 500,
  dataset: { transcriptRowCount: "10", transcriptFirstItemIndex: "100" },
  querySelector: () => null,
  querySelectorAll: () => [tailRow],
} as unknown as HTMLDivElement;
const unstableStateRef = {
  current: reduceTranscriptScroll(
    { ...INITIAL_TRANSCRIPT_SCROLL_STATE, scrollable: true },
    { type: "NATIVE_SCROLLBAR_BEGIN" },
  ).state,
};
unstableStateRef.current = reduceTranscriptScroll(unstableStateRef.current, { type: "NATIVE_SCROLLBAR_END", canClaimTail: true }).state;
const unstableCommands: string[] = [];
const unstableDispatch = (event: TranscriptScrollEvent) => {
  const result = reduceTranscriptScroll(unstableStateRef.current, event);
  unstableStateRef.current = result.state;
  unstableCommands.push(...result.commands.map((command) => command.type));
};
const unstableDeliverRef: { current: ((target?: HTMLDivElement) => void) | null } = { current: null };
const [, deliverUnstable] = createTranscriptReaderBottomHold({
  scrollRef: { current: unstableElement },
  stateRef: unstableStateRef as { current: TranscriptScrollState },
  generationRef,
  deliverScrollRef: unstableDeliverRef,
  dispatch: unstableDispatch,
});
unstableDeliverRef.current = () => deliverUnstable(unstableElement);
deliverUnstable(unstableElement, true);
for (let index = 0; index <= MAX_TAIL_MOUNT_CHECKS; index += 1) {
  unstableExtent += 2;
  const pending = [...frames.values()];
  frames.clear();
  pending.forEach((callback) => callback(index));
}
check(unstableStateRef.current.mode === "tail-follow", "a perpetually revising mounted tail cannot strand native-thumb release");
check(unstableCommands.filter((command) => command === "SCROLL_TO_LAST").length === 1,
  "the unstable mounted tail also hands off to one bounded jump-tail transaction");

// A wheel can touch the current native bottom while Virtuoso is still
// expanding its measured range. That bottom belongs only to the old extent;
// it must not survive the growth and force LAST through an unmounted window.
let wheelExtent = 1_000;
const wheelElement = {
  scrollTop: 500,
  get scrollHeight() { return wheelExtent; },
  clientHeight: 500,
  dataset: { transcriptRowCount: "10", transcriptFirstItemIndex: "100" },
  querySelector: () => null,
  querySelectorAll: () => [],
} as unknown as HTMLDivElement;
const wheelStateRef = {
  current: reduceTranscriptScroll(
    { ...INITIAL_TRANSCRIPT_SCROLL_STATE, scrollable: true },
    { type: "USER_SCROLL_INTENT", canClaimTail: true },
  ).state,
};
const wheelCommands: string[] = [];
const wheelDispatch = (event: TranscriptScrollEvent) => {
  const result = reduceTranscriptScroll(wheelStateRef.current, event);
  wheelStateRef.current = result.state;
  wheelCommands.push(...result.commands.map((command) => command.type));
};
const wheelDeliverRef: { current: ((target?: HTMLDivElement) => void) | null } = { current: null };
const [, deliverWheel] = createTranscriptReaderBottomHold({
  scrollRef: { current: wheelElement },
  stateRef: wheelStateRef as { current: TranscriptScrollState },
  generationRef,
  deliverScrollRef: wheelDeliverRef,
  dispatch: wheelDispatch,
});
wheelDeliverRef.current = () => deliverWheel(wheelElement);
deliverWheel(wheelElement);
wheelExtent += 128;
for (let index = 0; index <= MAX_TAIL_MOUNT_CHECKS; index += 1) {
  const pending = [...frames.values()];
  frames.clear();
  pending.forEach((callback) => callback(index));
}
check(wheelStateRef.current.mode === "reader-gesture", "an expanding wheel extent discards its stale physical-bottom proof");
check(wheelCommands.includes("SCROLL_TO_LAST") === false, "extent growth cannot force an unmounted wheel range to LAST");

// A loaded native host can deliver rAF far below 60fps. The release budget is
// also wall-clock bounded so 120 nominal frames cannot turn into a permanent
// manual state under runner contention.
const slowStateRef = {
  current: reduceTranscriptScroll(
    { ...INITIAL_TRANSCRIPT_SCROLL_STATE, scrollable: true },
    { type: "NATIVE_SCROLLBAR_BEGIN" },
  ).state,
};
slowStateRef.current = reduceTranscriptScroll(slowStateRef.current, { type: "NATIVE_SCROLLBAR_END", canClaimTail: true }).state;
const slowCommands: string[] = [];
const slowDispatch = (event: TranscriptScrollEvent) => {
  const result = reduceTranscriptScroll(slowStateRef.current, event);
  slowStateRef.current = result.state;
  slowCommands.push(...result.commands.map((command) => command.type));
};
const provedElement = {
  scrollTop: 400,
  scrollHeight: 1_000,
  clientHeight: 500,
  dataset: { transcriptRowCount: "10", transcriptFirstItemIndex: "100" },
  querySelector: () => null,
  querySelectorAll: () => [],
} as unknown as HTMLDivElement;
const slowDeliverRef: { current: ((target?: HTMLDivElement) => void) | null } = { current: null };
const [, deliverSlow] = createTranscriptReaderBottomHold({
  scrollRef: { current: provedElement },
  stateRef: slowStateRef as { current: TranscriptScrollState },
  generationRef,
  deliverScrollRef: slowDeliverRef,
  dispatch: slowDispatch,
});
slowDeliverRef.current = () => deliverSlow(provedElement);
const originalDateNow = Date.now;
let fakeNow = 50_000;
Date.now = () => fakeNow;
try {
  deliverSlow(provedElement, true);
  for (let index = 0; index < 6; index += 1) {
    fakeNow += MAX_TAIL_MOUNT_HOLD_MS / 4;
    const pending = [...frames.values()];
    frames.clear();
    pending.forEach((callback) => callback(index));
  }
} finally {
  Date.now = originalDateNow;
}
check(slowStateRef.current.mode === "tail-follow", "a pre-release bottom proof survives an off-bottom extent change");
check(slowCommands.filter((command) => command === "SCROLL_TO_LAST").length === 1,
  "the wall-clock fallback emits exactly one arbiter-owned tail transaction");

if (failed > 0) {
  console.error(`\n${failed} transcript reader bottom hold test(s) failed; ${passed} passed.`);
  process.exit(1);
}
console.log(`\n${passed} transcript reader bottom hold tests passed.`);
