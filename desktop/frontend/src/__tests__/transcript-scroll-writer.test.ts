// Run: tsx src/__tests__/transcript-scroll-writer.test.ts

import { equal } from "node:assert/strict";
import { JSDOM } from "jsdom";
import type { VirtuosoHandle } from "react-virtuoso";
import { createTranscriptScrollWriter } from "../lib/transcriptScrollWriter";
import type { TranscriptScrollMode } from "../lib/transcriptScrollArbiter";
import type { TranscriptScrollWriteRecord } from "../lib/transcriptScrollProbe";

const dom = new JSDOM("<div id='scroll'></div>", { pretendToBeVisual: true });
globalThis.window = dom.window as unknown as Window & typeof globalThis;
const element = dom.window.document.getElementById("scroll") as HTMLDivElement;
Object.defineProperties(element, {
  scrollTop: { configurable: true, writable: true, value: 200 },
  scrollHeight: { configurable: true, value: 2_000 },
  clientHeight: { configurable: true, value: 500 },
});

const calls: string[] = [];
element.scrollTo = (options?: ScrollToOptions | number, y?: number) => {
  calls.push("nativeScrollTo");
  element.scrollTop = typeof options === "number" ? (y ?? element.scrollTop) : (options?.top ?? element.scrollTop);
};
const handle = {
  scrollTo: () => calls.push("scrollTo"),
  scrollBy: () => calls.push("scrollBy"),
  scrollToIndex: () => calls.push("scrollToIndex"),
} as unknown as VirtuosoHandle;
const generationRef = { current: 4 };
const ownershipEpochRef = { current: 7 };
const geometryRevisionRef = { current: 9 };
const modeRef = { current: "manual" as TranscriptScrollMode };
const records: TranscriptScrollWriteRecord[] = [];
dom.window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (record) => records.push(record);
const writer = createTranscriptScrollWriter({
  virtuosoRef: { current: handle },
  scrollRef: { current: element },
  modeRef,
  generationRef,
  ownershipEpochRef,
  geometryRevisionRef,
});

equal(writer.write({
  owner: "reader-stability",
  operation: "scrollBy",
  top: 120,
  reason: "reader-rebound",
  phase: "correct-offset",
  expectedSurfaceGeneration: 4,
  expectedOwnershipEpoch: 7,
  expectedGeometryRevision: 9,
}), true, "the current generation may write");
equal(calls.join(","), "scrollBy", "the gateway emits the requested operation once");
equal(records[0]?.generation, 4, "the diagnostic binds the write to its generation");
equal(records[0]?.geometryRevision, 9, "the diagnostic binds the write to its geometry revision");
equal(records[0]?.ownershipEpoch, 7, "the diagnostic binds the write to its ownership epoch");
equal(records[0]?.sequence, 1, "the gateway assigns a monotonic sequence");

equal(writer.write({
  owner: "recovery",
  operation: "scrollTo",
  top: 600,
  reason: "stale-recovery",
  expectedSurfaceGeneration: 3,
  expectedOwnershipEpoch: 7,
  expectedGeometryRevision: 9,
}), false, "a stale generation cannot write to the replacement surface");
equal(calls.length, 1, "a rejected stale write never reaches Virtuoso");
equal(records[1]?.rejectedReason, "stale-surface-generation", "stale writes record a content-free rejection reason");

equal(writer.write({
  owner: "recovery",
  operation: "scrollToIndex",
  index: 4,
  reason: "stale-epoch",
  phase: "mount-anchor",
  expectedSurfaceGeneration: 4,
  expectedOwnershipEpoch: 6,
  expectedGeometryRevision: 9,
}), false, "a stale ownership epoch cannot write");
equal(records[2]?.rejectedReason, "stale-ownership-epoch", "stale ownership is diagnosable");

equal(writer.write({
  owner: "recovery",
  operation: "scrollToIndex",
  index: 4,
  reason: "stale-revision",
  phase: "mount-anchor",
  expectedSurfaceGeneration: 4,
  expectedOwnershipEpoch: 7,
  expectedGeometryRevision: 8,
}), false, "a stale geometry revision cannot write");
equal(records[3]?.rejectedReason, "stale-geometry-revision", "stale geometry is diagnosable");

equal(writer.write({
  owner: "reader-stability",
  operation: "scrollBy",
  top: 40,
  reason: "duplicate-reader-correction",
  phase: "correct-offset",
  expectedSurfaceGeneration: 4,
  expectedOwnershipEpoch: 7,
  expectedGeometryRevision: 9,
}), false, "one owner phase writes at most once per geometry revision");
equal(records[4]?.rejectedReason, "duplicate-revision-phase", "duplicate phases are diagnosable");

geometryRevisionRef.current = 10;

modeRef.current = "native-thumb";
equal(writer.write({
  owner: "tail-follow",
  operation: "pinTail",
  top: 1_500,
  reason: "tail-settle",
  expectedSurfaceGeneration: 4,
  expectedOwnershipEpoch: 7,
  expectedGeometryRevision: 10,
}), false, "native-thumb ownership suppresses every imperative writer");
equal(calls.length, 1, "the native thumb remains browser-owned");

modeRef.current = "tail-follow";
geometryRevisionRef.current = 11;
equal(writer.write({
  owner: "tail-follow",
  operation: "pinTail",
  top: 1_500,
  reason: "tail-rebound",
  phase: "settle",
  expectedSurfaceGeneration: 4,
  expectedOwnershipEpoch: 7,
  expectedGeometryRevision: 11,
}), true, "tail pinning writes the current physical scroller extent");
equal(calls[calls.length - 1], "nativeScrollTo", "pinTail bypasses Virtuoso's stale size-tree lane");
equal(element.scrollTop, 1_500, "the physical tail write lands synchronously");

equal(writer.write({
  owner: "tail-follow",
  operation: "pinTail",
  top: 1_508,
  reason: "jump-bottom",
  phase: "settle",
  settleFrame: 1,
  expectedSurfaceGeneration: 4,
  expectedOwnershipEpoch: 7,
  expectedGeometryRevision: 11,
}), true, "the first bounded tail settle step may write within the same geometry revision");
equal(writer.write({
  owner: "tail-follow",
  operation: "pinTail",
  top: 1_508,
  reason: "jump-bottom",
  phase: "settle",
  settleFrame: 1,
  expectedSurfaceGeneration: 4,
  expectedOwnershipEpoch: 7,
  expectedGeometryRevision: 11,
}), false, "a duplicate bounded tail settle step remains fenced");
equal(writer.write({
  owner: "tail-follow",
  operation: "pinTail",
  top: 1_516,
  reason: "jump-bottom",
  phase: "settle",
  settleFrame: 2,
  expectedSurfaceGeneration: 4,
  expectedOwnershipEpoch: 7,
  expectedGeometryRevision: 11,
}), true, "the final bounded tail settle step has a distinct writer fence");

dom.window.close();
console.log("\ntranscript scroll writer tests passed");
