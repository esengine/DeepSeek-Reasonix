// Run: node --import tsx src/__tests__/transcript-scroll-diagnostics.test.ts

import assert from "node:assert/strict";
import {
  createTranscriptScrollDiagnostics,
  isTranscriptScrollDiagnosticsBuild,
} from "../lib/transcriptScrollDiagnostics";

let now = 1_000;
const diagnostics = createTranscriptScrollDiagnostics({
  maxEvents: 4,
  now: () => now,
  randomID: () => "0123456789abcdef0123456789abcdef",
  environment: () => ({
    buildCommit: "test-commit",
    buildChannel: "test",
    platform: "windows",
    userAgent: "Reasonix diagnostic test",
    devicePixelRatio: 1.25,
    viewportWidth: 1440,
    viewportHeight: 900,
    reducedMotion: false,
    transcriptWidth: 1180,
    contentWidth: 960,
    fontSize: 14,
    lineHeight: 23.52,
    processFoldPreference: "auto",
    reasoningDisplayMode: "summary",
  }),
});

diagnostics.record("scroll", { scrollTop: 10 });
assert.equal(diagnostics.getSnapshot().eventCount, 0, "idle recorder ignores events");

diagnostics.start();
now += 10;
diagnostics.record("wheel", { deltaY: -120 });
now += 10;
diagnostics.record("scroll", {
  scrollTop: 120,
  scrollHeight: 2_400,
  clientHeight: 800,
  bottomDistance: 1_480,
  transcriptText: "PRIVATE_TRANSCRIPT_CANARY",
  rowKey: "raw-session-row-key",
  path: "C:\\Users\\private-user\\secret",
  token: "TOKEN_CANARY",
} as never);
now += 10;
diagnostics.mark();
now += 10;
diagnostics.record("items-rendered", { mountedRows: 12, totalRows: 100, firstVisibleIndex: 44 });
now += 10;
diagnostics.record("scroll-write", { owner: "jump", writeKind: "scrollToIndex", targetIndex: 45 });

const active = diagnostics.getSnapshot();
assert.equal(active.status, "recording");
assert.equal(active.eventCount, 4, "ring buffer remains bounded");
assert.equal(active.droppedEventCount, 2, "ring buffer reports dropped events");
assert.equal(active.markerCount, 1, "issue marker is retained in summary");

const payload = diagnostics.stop();
assert.equal(payload.schemaVersion, 2);
assert.equal(payload.summary.eventCount, 4);
assert.equal(payload.summary.droppedEventCount, 3);
assert.equal(payload.summary.markerCount, 1);
assert.equal(payload.manifest.platform, "windows");
assert.equal(payload.events[payload.events.length - 1]?.type, "stop");

const serialized = JSON.stringify(payload);
for (const forbidden of [
  "PRIVATE_TRANSCRIPT_CANARY",
  "raw-session-row-key",
  "private-user",
  "TOKEN_CANARY",
  "transcriptText",
  "rowKey",
  "path",
  "token",
]) {
  assert.equal(serialized.includes(forbidden), false, `payload excludes ${forbidden}`);
}

const frozenCount = diagnostics.getSnapshot().eventCount;
now += 10;
diagnostics.record("scroll", { scrollTop: 999 });
assert.equal(diagnostics.getSnapshot().eventCount, frozenCount, "stopped recorder remains frozen");

diagnostics.start();
assert.equal(diagnostics.getSnapshot().eventCount, 1, "new recording clears the old trace");
diagnostics.reset();
assert.equal(diagnostics.getSnapshot().status, "idle");
assert.equal(diagnostics.getSnapshot().eventCount, 0);

const markerOverflow = createTranscriptScrollDiagnostics({
  maxEvents: 4,
  now: () => now,
  randomID: () => "fedcba9876543210fedcba9876543210",
  environment: () => ({
    buildCommit: "test-commit",
    buildChannel: "test",
    platform: "windows",
    userAgent: "Reasonix diagnostic test",
    devicePixelRatio: 1,
    viewportWidth: 1280,
    viewportHeight: 720,
    reducedMotion: false,
    transcriptWidth: 1040,
    contentWidth: 960,
    fontSize: 14,
    lineHeight: 23.52,
    processFoldPreference: "expanded",
    reasoningDisplayMode: "expanded",
  }),
});
markerOverflow.start();
markerOverflow.mark();
for (let i = 0; i < 4; i += 1) markerOverflow.record("scroll", { scrollTop: i });
const overflowPayload = markerOverflow.stop();
assert.equal(overflowPayload.summary.markerCount, 0, "summary only counts markers retained in the bounded trace");
assert.equal(overflowPayload.events.some((event) => event.type === "mark"), false, "evicted markers do not survive serialization");

const detailDiagnostics = createTranscriptScrollDiagnostics({
  now: () => now,
  randomID: () => "abcdef0123456789abcdef0123456789",
  environment: () => ({
    buildCommit: "detail-test",
    buildChannel: "test",
    platform: "windows",
    userAgent: "Reasonix detail diagnostic test",
    devicePixelRatio: 1.38,
    viewportWidth: 1920,
    viewportHeight: 1080,
    reducedMotion: true,
    transcriptWidth: 1600,
    contentWidth: 960,
    fontSize: 15,
    lineHeight: 25.2,
    processFoldPreference: "auto",
    reasoningDisplayMode: "legacy-collapsed",
  }),
});
detailDiagnostics.start();
detailDiagnostics.record("row-measure", {
  rowIndex: 44,
  rowKind: "reasoning",
  layoutVariant: "reasoning-summary",
  estimateSource: "static",
  estimatedSize: 64,
  previousSize: 64,
  measuredSize: 64,
  sizeDelta: 0,
  contentRevision: 3,
  foldState: "closed",
  disclosureCount: 1,
  rowKey: "PRIVATE_ROW_KEY",
  transcriptText: "PRIVATE_TRANSCRIPT_CANARY",
});
detailDiagnostics.record("geometry-contract-violation", {
  rowIndex: 45,
  rowKind: "tool",
  layoutVariant: "tool-collapsed",
  estimateSource: "static",
  estimatedSize: 37,
  measuredSize: 55,
  sizeDelta: 18,
  relativeError: 18 / 37,
  rowKey: "PRIVATE_ROW_KEY_2",
});
detailDiagnostics.record("scroll-state", {
  source: "jump-bottom",
  previousMode: "manual",
  mode: "tail-follow",
  atBottom: true,
  scrollable: true,
  readerIntent: false,
  canClaimTail: false,
  tailCommand: true,
  scrollTop: 4_000,
  scrollHeight: 5_000,
  clientHeight: 800,
  bottomDistance: 200,
});
detailDiagnostics.record("scroll-write", {
  owner: "tail-follow",
  writeKind: "scrollTo",
  source: "jump-bottom",
  phase: "settle",
  targetTop: 5_000,
  settleFrame: 2,
  offBottomFrames: 2,
  stagnantFrames: 0,
});
detailDiagnostics.record("geometry-revision", {
  geometryRevision: 12,
  sources: ["row-measure", "items-rendered", "private-source"],
  scrollHeight: 5_400,
  footerHeight: 220,
  viewport: 800,
  mounted: 40,
  total: 420,
  transient: true,
});
detailDiagnostics.record("scroll-write", {
  owner: "anchor-compensation",
  writeKind: "scrollBy",
  source: "reader-stability",
  phase: "correct-offset",
  sequence: 13,
  generation: 4,
  ownershipEpoch: 8,
  geometryRevision: 12,
  transactionId: 3,
  rejectedReason: "duplicate-revision-phase",
});
const detailPayload = detailDiagnostics.stop();
const rowMeasurement = detailPayload.events.find((event) => event.type === "row-measure");
assert.deepEqual(rowMeasurement, {
  t: 0,
  type: "row-measure",
  rowIndex: 44,
  estimatedSize: 64,
  previousSize: 64,
  measuredSize: 64,
  sizeDelta: 0,
  contentRevision: 3,
  disclosureCount: 1,
  rowKind: "reasoning",
  layoutVariant: "reasoning-summary",
  estimateSource: "static",
  foldState: "closed",
});
const geometryViolation = detailPayload.events.find((event) => event.type === "geometry-contract-violation");
assert.deepEqual(geometryViolation, {
  t: 0,
  type: "geometry-contract-violation",
  rowIndex: 45,
  estimatedSize: 37,
  measuredSize: 55,
  sizeDelta: 18,
  relativeError: 0.49,
  rowKind: "tool",
  layoutVariant: "tool-collapsed",
  estimateSource: "static",
});
const geometryRevision = detailPayload.events.find((event) => event.type === "geometry-revision");
assert.deepEqual(geometryRevision?.sources, ["row-measure", "items-rendered"], "geometry sources keep only fixed, content-free enums");
const anchorWrite = detailPayload.events.find((event) => event.owner === "anchor-compensation");
assert.equal(anchorWrite?.owner, "anchor-compensation", "known anchor writers do not degrade to other");
assert.equal(anchorWrite?.ownershipEpoch, 8, "writer ownership epochs survive sanitization");
assert.equal(anchorWrite?.rejectedReason, "duplicate-revision-phase", "writer rejection reasons survive sanitization");
const detailSerialized = JSON.stringify(detailPayload);
assert.equal(detailSerialized.includes("PRIVATE_ROW_KEY"), false, "row measurements exclude stable row keys");
assert.equal(detailSerialized.includes("PRIVATE_TRANSCRIPT_CANARY"), false, "row measurements exclude transcript text");
assert.equal(detailSerialized.includes("PRIVATE_ROW_KEY_2"), false, "geometry violations exclude stable row keys");

assert.equal(isTranscriptScrollDiagnosticsBuild("test", false), true, "test builds expose diagnostics");
assert.equal(isTranscriptScrollDiagnosticsBuild("preview", false), true, "preview builds expose diagnostics");
assert.equal(isTranscriptScrollDiagnosticsBuild("canary", false), true, "canary builds expose diagnostics");
assert.equal(isTranscriptScrollDiagnosticsBuild("stable", true), true, "development server exposes diagnostics");
assert.equal(isTranscriptScrollDiagnosticsBuild("stable", false), false, "stable production hides diagnostics");

console.log("transcript scroll diagnostics tests passed");
