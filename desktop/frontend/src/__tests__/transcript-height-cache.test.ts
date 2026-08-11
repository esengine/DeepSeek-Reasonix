// Run: tsx src/__tests__/transcript-height-cache.test.ts
//
// Streaming rows must never be written into the height cache: their height is
// a moving target and cached mid-stream values would poison later estimates
// for the same row key (re-mounts would replay a transient height).

import { createTranscriptMeasureElement } from "../lib/transcriptHeightCache";
import type { TranscriptRow } from "../lib/transcriptRows";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function isStreamingRow(row: TranscriptRow | undefined): boolean {
  if (!row) return false;
  if (row.kind === "answer") return row.item.streaming;
  if (row.kind === "reasoning") return row.item.streaming && !row.item.reasoningComplete;
  return false;
}

ok(!isStreamingRow({ kind: "answer", key: "a:1", item: { streaming: false } } as TranscriptRow), "settled answer row is cached");
ok(isStreamingRow({ kind: "answer", key: "a:1", item: { streaming: true } } as TranscriptRow), "streaming answer row is skipped");
ok(isStreamingRow({ kind: "reasoning", key: "r:1", segmentKey: "s", item: { streaming: true, reasoningComplete: false } } as TranscriptRow), "streaming reasoning row is skipped");
ok(!isStreamingRow({ kind: "reasoning", key: "r:1", segmentKey: "s", item: { streaming: true, reasoningComplete: true } } as TranscriptRow), "completed reasoning row is cached");
ok(!isStreamingRow({ kind: "user", key: "u:1", item: {} } as TranscriptRow), "user row is cached");
ok(!isStreamingRow(undefined), "missing row is cached");

// Integration-level wiring: the measure element evaluates the skip predicate
// per element and only settles cache writes for non-streaming rows.
const rows: TranscriptRow[] = [
  { kind: "answer", key: "a:stream", item: { streaming: true } },
  { kind: "answer", key: "a:settled", item: { streaming: false } },
];
const cachedKeys: string[] = [];
const measure = createTranscriptMeasureElement({
  tabId: "tab",
  getLayoutSnapshot: () => ({ signature: "w:0", width: 0 }),
  cache: {
    set(_tabId: string, _sig: string, rowKey: string) {
      cachedKeys.push(rowKey);
    },
  } as never,
  skipCacheWriteWhen: (el) => {
    const rowKey = el.dataset.rowKey ?? "";
    const row = rows.find((candidate) => String(candidate.key) === rowKey);
    return row?.kind === "answer" && row.item.streaming;
  },
});

const fakeRowElement = (rowKey: string): HTMLDivElement =>
  ({
    dataset: { rowKey },
    getBoundingClientRect: () => ({ height: 90, width: 600, top: 0, left: 0, bottom: 90, right: 600, x: 0, y: 0, toJSON: () => ({}) }),
    offsetHeight: 90,
    clientHeight: 90,
    scrollHeight: 90,
  }) as unknown as HTMLDivElement;

const fakeInstance = {
  options: {},
  indexFromElement: () => 0,
  getItemKey: () => "k",
  measurementsCache: new Map(),
  getCurrentOffset: () => 0,
  getDistanceFromEnd: () => 0,
  getVirtualItems: () => [],
  measureElement: () => 90,
} as never;

measure(fakeRowElement("a:stream"), {} as ResizeObserverEntry, fakeInstance);
measure(fakeRowElement("a:settled"), {} as ResizeObserverEntry, fakeInstance);

ok(JSON.stringify(cachedKeys) === JSON.stringify(["a:settled"]), `only settled rows cached, got ${JSON.stringify(cachedKeys)}`);

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
