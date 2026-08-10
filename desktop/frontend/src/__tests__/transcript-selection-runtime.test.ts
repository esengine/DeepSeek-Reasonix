// Run: tsx src/__tests__/transcript-selection-runtime.test.ts

import { defaultRangeExtractor, type Range, type Virtualizer } from "@tanstack/react-virtual";
import {
  canTranscriptScrollOwnerWrite,
  type TranscriptScrollMode,
  type TranscriptScrollOwner,
} from "../lib/transcriptScrollController";
import { createSelectionRangeExtractor } from "../lib/transcriptSelectionRange";
import {
  EMPTY_TRANSCRIPT_LAYOUT_SNAPSHOT,
  TranscriptHeightCache,
  createTranscriptMeasureElement,
  estimateTranscriptRowHeightForLayout,
  estimateTranscriptContentHeight,
  readTranscriptLayoutSnapshot,
} from "../lib/transcriptHeightCache";

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


// Measurements are isolated by tab and layout signature, and the bounded LRU
// releases the least-recently-used transcript rather than growing forever.
{
  const cache = new TranscriptHeightCache({ maxTabs: 3, maxRowsPerTab: 2 });
  cache.set("a", "wide", "r1", 100);
  cache.set("a", "wide", "r2", 110);
  cache.set("a", "wide", "r3", 120);
  equal(cache.get("a", "wide", "r1"), undefined, "per-tab row LRU evicts the oldest measurement");
  equal(cache.get("a", "wide", "r3"), 120, "latest row measurement remains cached");
  equal(cache.get("a", "narrow", "r3"), undefined, "layout signatures do not share heights");
  cache.set("b", "wide", "r", 1);
  cache.set("c", "wide", "r", 1);
  cache.set("d", "wide", "r", 1);
  equal(cache.get("a", "wide", "r3"), undefined, "tab LRU keeps only the configured recent sessions");
  ok(estimateTranscriptContentHeight("user", "x".repeat(10_000), 320) <= 360, "user estimate respects its upper bound");
  ok(estimateTranscriptContentHeight("answer", "x".repeat(100_000), 320) <= 1200, "answer estimate respects its upper bound");

  const measuredCache = new TranscriptHeightCache();
  const measure = createTranscriptMeasureElement({
    tabId: "measured-tab",
    getLayoutSnapshot: () => EMPTY_TRANSCRIPT_LAYOUT_SNAPSHOT,
    cache: measuredCache,
  });
  const element = { dataset: { rowKey: "measured-row" } } as unknown as HTMLDivElement;
  const entry = { borderBoxSize: [{ blockSize: 246 }] } as unknown as ResizeObserverEntry;
  const instance = { options: { horizontal: false } } as unknown as Virtualizer<HTMLDivElement, HTMLDivElement>;
  equal(measure(element, entry, instance), 246, "official ResizeObserver measurements flow through the transcript adapter");
  equal(measuredCache.get("measured-tab", "w:0", "measured-row"), 246, "ResizeObserver row growth refreshes the height cache");
}

// Layout style reads happen once per layout epoch, not once per row while
// TanStack materializes an unmeasured long transcript.
{
  const original = globalThis.getComputedStyle;
  let styleReads = 0;
  Object.defineProperty(globalThis, "getComputedStyle", {
    configurable: true,
    value: () => {
      styleReads += 1;
      return {
        fontSize: "14px",
        fontFamily: "sans-serif",
        lineHeight: "20px",
        letterSpacing: "normal",
      } as CSSStyleDeclaration;
    },
  });
  const layout = readTranscriptLayoutSnapshot({ clientWidth: 641 } as HTMLElement);
  const cache = new TranscriptHeightCache();
  for (let index = 0; index < 10_000; index += 1) {
    estimateTranscriptRowHeightForLayout({
      cache,
      tabId: "long-tab",
      layout,
      rowKey: String(index),
      row: undefined,
      fallback: 100,
    });
  }
  equal(styleReads, 1, "10,000 cold row estimates reuse one layout snapshot");
  equal(layout.width, 640, "content estimates use the same width bucket as cached measurements");
  if (original) Object.defineProperty(globalThis, "getComputedStyle", { configurable: true, value: original });
  else Reflect.deleteProperty(globalThis, "getComputedStyle");
}

function equal(actual: unknown, expected: unknown, label: string) {
  ok(JSON.stringify(actual) === JSON.stringify(expected), `${label}: ${JSON.stringify(actual)}`);
}

console.log("\ntranscript selection runtime");

// Selection gestures own the viewport. No delayed stream, resize, footer, or
// virtualizer compensation may write scrollTop while a native/logical gesture
// is active.
{
  const blocked: TranscriptScrollOwner[] = ["stream", "container-resize", "footer-resize", "virtualizer"];
  const modes: TranscriptScrollMode[] = ["native-selecting", "logical-selecting"];
  for (const mode of modes) {
    ok(blocked.every((owner) => !canTranscriptScrollOwnerWrite(mode, owner)), `${mode} rejects passive scroll writers`);
    ok(canTranscriptScrollOwnerWrite(mode, "selection-edge-scroll"), `${mode} permits selection edge scrolling`);
  }
  ok(canTranscriptScrollOwnerWrite("tail-follow", "stream"), "tail-follow permits streaming pin");
  ok(!canTranscriptScrollOwnerWrite("manual", "stream"), "manual mode rejects streaming pin");
  ok(canTranscriptScrollOwnerWrite("manual", "jump"), "manual mode permits explicit jump");
}

// The native fallback retains one continuous row interval so Selection never
// points through an unmounted hole. Reversing endpoints produces the same set.
{
  const range: Range = { startIndex: 20, endIndex: 25, overscan: 2, count: 80 };
  const normal = defaultRangeExtractor(range);
  const forward = createSelectionRangeExtractor(() => ({ anchorIndex: 4, focusIndex: 23 }))(range);
  const backward = createSelectionRangeExtractor(() => ({ anchorIndex: 23, focusIndex: 4 }))(range);
  const expected = Array.from(new Set([...normal, ...Array.from({ length: 20 }, (_, i) => i + 4)])).sort((a, b) => a - b);
  equal(forward, expected, "forward selection retains every row between endpoints");
  equal(backward, expected, "backward selection retains every row between endpoints");
  equal(createSelectionRangeExtractor(() => null)(range), normal, "cleared selection immediately restores normal virtualization");
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
