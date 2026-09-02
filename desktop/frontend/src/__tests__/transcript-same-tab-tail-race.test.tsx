// Run: tsx src/__tests__/transcript-same-tab-tail-race.test.tsx

import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { VirtuosoHandle } from "react-virtuoso";
import { useTranscriptLayoutIntegrity } from "../lib/useTranscriptLayoutIntegrity";
import type { TranscriptRow } from "../lib/transcriptRows";
import { useTranscriptScrollArbiter } from "../lib/useTranscriptScrollArbiter";
import type { Item } from "../lib/useController";
import { installTranscriptRaceClock } from "./helpers/transcriptRaceClock";
import { installTranscriptRecoveryRaceDom } from "./helpers/transcriptRecoveryRaceDom";

let passed = 0;
let failed = 0;
const check = (condition: unknown, label: string) => {
  process.stdout.write(`  ${condition ? "PASS" : "FAIL"}  ${label}\n`);
  condition ? passed += 1 : failed += 1;
};

console.log("\ntranscript same-tab tail races");

const { dom, flushFrames } = installTranscriptRecoveryRaceDom();
const { advanceClock, restore: restoreClock } = installTranscriptRaceClock(dom.window as unknown as Window);
const scrollElement = dom.window.document.getElementById("scroll") as HTMLDivElement;
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 100 });
Object.defineProperty(scrollElement, "scrollHeight", { configurable: true, value: 1_000 });
Object.defineProperty(scrollElement, "scrollTop", { configurable: true, writable: true, value: 892 });

let blockedTailPlacements = 0;
let scrollToCalls = 0;
const applyScrollTo = ({ top = 0 }: { top?: number } = {}) => {
  scrollToCalls += 1;
  if (blockedTailPlacements > 0 && top >= scrollElement.scrollHeight - scrollElement.clientHeight) {
    blockedTailPlacements -= 1;
    return;
  }
  scrollElement.scrollTop = Math.max(0, Math.min(scrollElement.scrollHeight - scrollElement.clientHeight, top));
};
scrollElement.scrollTo = applyScrollTo;
const virtuosoHandle = {
  scrollBy: () => {},
  scrollToIndex: () => {},
  scrollTo: applyScrollTo,
} as unknown as VirtuosoHandle;

const item: Item = { kind: "assistant", id: "a", text: "answer", reasoning: "", streaming: false };
const rows: TranscriptRow[] = [{ kind: "answer", key: "row-a", item }];
const rowIndexByKey = new Map(rows.map((row, index) => [String(row.key), index]));
const readyRef = { current: true };
let arbiter: ReturnType<typeof useTranscriptScrollArbiter> | undefined;
let integrity: ReturnType<typeof useTranscriptLayoutIntegrity> | undefined;

function Probe({ surfaceKey }: { surfaceKey: string }) {
  const scroll = useTranscriptScrollArbiter();
  const layout = useTranscriptLayoutIntegrity({
    surfaceKey,
    rows,
    rowIndexByKey,
    scrollRef: scroll.scrollRef,
    pinnedRef: scroll.pinnedRef,
    readyRef,
    scrollToBottom: scroll.scrollToBottom,
    submitRecoveryRequest: scroll.submitRecoveryRequest,
    retryRecoveryRequest: scroll.retryRecoveryRequest,
    lastGoodAnchorRef: scroll.lastGoodAnchorRef,
    layoutTransientRef: scroll.layoutTransientRef,
    layoutWidth: 800,
  });
  arbiter = scroll;
  integrity = layout;
  return null;
}

const root = createRoot(dom.window.document.getElementById("root")!);
await act(async () => root.render(<Probe surfaceKey="surface-initial" />));
await act(async () => {
  (arbiter!.virtuosoRef as { current: VirtuosoHandle | null }).current = virtuosoHandle;
  arbiter!.scrollerRef(scrollElement);
});

const switchSurface = async (surfaceKey: string) => {
  await act(async () => root.render(<Probe surfaceKey={surfaceKey} />));
  await act(async () => { arbiter?.reset(); });
  await flushFrames();
};

// Same-tab adoption/reconnect changes only the reveal generation; it has no
// App navigation paint token. The first-items tail transaction must survive
// two late Virtuoso placements that restore the stale 8px gap.
blockedTailPlacements = 2;
scrollToCalls = 0;
await switchSurface("surface-same-tab-takeover");
await act(async () => integrity?.handleItemsRendered(1));
await flushFrames();
await advanceClock(480);
check(scrollToCalls === 3, `same-tab takeover receives exactly three bounded tail probes (${scrollToCalls})`);
check(
  arbiter?.modeRef.current === "tail-follow" && scrollElement.scrollTop === 900,
  "same-tab takeover converges after two late placements without a navigation token",
);

// Explicit reader intent owns the same generation and cancels both delayed
// confirmations before they can reclaim the tail.
scrollElement.scrollTop = 892;
blockedTailPlacements = 2;
scrollToCalls = 0;
await switchSurface("surface-same-tab-reader-takeover");
await act(async () => integrity?.handleItemsRendered(1));
await flushFrames();
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaX: 0,
  deltaY: -80,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
await advanceClock(600);
check(scrollToCalls === 1, `manual reader takeover cancels both delayed tail probes (${scrollToCalls})`);
check(
  arbiter?.modeRef.current === "manual" && scrollElement.scrollTop === 892,
  "a same-tab reveal never yanks an active manual reader",
);

await act(async () => root.unmount());
restoreClock();
dom.window.close();
if (failed > 0) {
  console.error(`\n${failed} failed, ${passed} passed`);
  process.exit(1);
}
console.log(`\n${passed} passed`);
