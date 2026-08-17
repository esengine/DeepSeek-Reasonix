// Run: tsx src/__tests__/transcript-layout-recovery.test.ts

import { initialState, reducer, type Item } from "../lib/useController";
import { estimateTranscriptRowSize, type TranscriptRow } from "../lib/transcriptRows";
import {
  chooseTranscriptLayoutAnchor,
  transcriptAnchorInitialLocation,
  transcriptViewportIsBlank,
} from "../lib/transcriptVirtuosoRecovery";

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

console.log("\ntranscript layout recovery");

const preview: Item = { kind: "assistant", id: "he:answer", text: "preview", reasoning: "", streaming: false };
const resolved: Item = { ...preview, text: "resolved ".repeat(2_000) };
const patched = reducer(
  { ...initialState, items: [preview] },
  { type: "history_items_patch", patches: { [preview.id]: resolved } },
);
check(patched.historyLayoutRevision === 1, "resolved history content bumps the transcript content version");
const ignored = reducer(
  patched,
  { type: "history_items_patch", patches: { missing: resolved } },
);
check(ignored === patched, "an unrelated content patch does not invalidate the transcript layout");

const visibleAnchor = chooseTranscriptLayoutAnchor(
  [
    { rowKey: "before", top: -240, bottom: -40 },
    { rowKey: "visible", top: 80, bottom: 240 },
    { rowKey: "after", top: 260, bottom: 420 },
  ],
  { top: 50, bottom: 350 },
  false,
);
check(
  visibleAnchor?.mode === "manual" && visibleAnchor.rowKey === "visible" && visibleAnchor.offset === 30,
  "manual recovery records the first visible logical row and pixel offset",
);

const nearestAnchor = chooseTranscriptLayoutAnchor(
  [
    { rowKey: "above", top: -400, bottom: -200 },
    { rowKey: "below", top: 900, bottom: 1_100 },
  ],
  { top: 0, bottom: 500 },
  false,
);
check(
  nearestAnchor?.mode === "manual" && nearestAnchor.rowKey === "above" && nearestAnchor.offset === 0,
  "blank-viewport recovery snaps the nearest logical row into view",
);

const location = transcriptAnchorInitialLocation(
  visibleAnchor,
  new Map([["visible", 7]]),
  1_000_000,
);
check(
  typeof location === "object" && location.index === 1_000_007 && location.align === "start" && location.offset === 30,
  "manual anchor becomes an absolute Virtuoso restore location",
);
const tailLocation = transcriptAnchorInitialLocation({ mode: "tail" }, new Map(), 1_000_000);
check(
  typeof tailLocation === "object" && tailLocation.index === "LAST" && tailLocation.align === "end",
  "tail recovery remains pinned to the last row",
);

check(
  transcriptViewportIsBlank(
    [{ rowKey: "above", top: -400, bottom: -200 }, { rowKey: "below", top: 600, bottom: 800 }],
    { top: 0, bottom: 500 },
    true,
  ),
  "a scrollable viewport with mounted rows only outside its bounds is blank",
);
check(
  !transcriptViewportIsBlank([{ rowKey: "visible", top: 100, bottom: 200 }], { top: 0, bottom: 500 }, true),
  "a row intersecting the viewport is not blank",
);

const shortRow: TranscriptRow = { kind: "answer", key: "a:short", item: preview };
const longRow: TranscriptRow = { kind: "answer", key: "a:long", item: resolved };
check(
  estimateTranscriptRowSize(longRow) > estimateTranscriptRowSize(shortRow) * 4,
  "resolved long-form answers receive a content-aware height estimate",
);

if (failed > 0) {
  console.error(`\n${failed} transcript layout recovery test(s) failed; ${passed} passed.`);
  process.exit(1);
}
console.log(`\n${passed} transcript layout recovery tests passed.`);
