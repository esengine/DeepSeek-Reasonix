import assert from "node:assert/strict";
import {
  createFrontendDiagnostics,
  isFrontendDiagnosticsBuild,
} from "../lib/frontendDiagnostics";

let now = 10_000;
const diagnostics = createFrontendDiagnostics({
  maxEvents: 32,
  now: () => now,
  randomID: () => "0123456789abcdef0123456789abcdef",
  environment: () => ({
    buildCommit: "test-commit",
    buildChannel: "canary",
    platform: "windows",
    userAgent: "Reasonix test",
    devicePixelRatio: 1.25,
    viewportWidth: 1440,
    viewportHeight: 900,
    language: "zh-CN",
    reducedMotion: false,
  }),
});

diagnostics.record("app", "app.surface", { tabId: "private-tab", transcriptText: "PRIVATE_TEXT" });
assert.equal(diagnostics.getSnapshot().eventCount, 0, "idle recorder ignores events");

diagnostics.start();
now += 10;
diagnostics.record("browser", "click", {
  target: "button",
  path: "C:\\Users\\private-user\\secret",
  token: "PRIVATE_TOKEN",
  text: "PRIVATE_TEXT",
});
now += 10;
diagnostics.record("transcript", "transcript.row-measure", {
  rowIndex: 44,
  rowKind: "answer",
  layoutVersion: "1:42",
  layoutVariant: "text-flow",
  estimateSource: "static",
  estimatedSize: 1_800,
  measuredSize: 420,
  sizeDelta: -1_380,
  contentRevision: 3,
});
diagnostics.record("history", "history.items-patch", {
  patchCount: 2,
  contentRevision: 4,
});
diagnostics.record("transcript", "transcript.scroll-write", {
  source: "recovery-end",
  owner: "recovery",
  writeKind: "scrollToIndex",
  targetIndex: "LAST",
  targetTop: 720,
  previousMode: "manual",
  canClaimTail: false,
  tailCommand: true,
});
diagnostics.mark();
for (let index = 0; index < 40; index += 1) {
  now += 1;
  diagnostics.record("browser", "scroll", { scrollTop: index * 10, scrollHeight: 2_400, clientHeight: 800 });
}
diagnostics.record("transcript", "transcript.scroll-write", {
  source: "recovery-end",
  owner: "recovery",
  writeKind: "scrollToIndex",
  targetIndex: "LAST",
  targetTop: 720,
});
diagnostics.record("transcript", "transcript.row-measure", {
  layoutVersion: "1:42",
  layoutVariant: "text-flow",
  estimateSource: "static",
  measuredSize: 420,
});
diagnostics.record("history", "history.items-patch", { patchCount: 2, contentRevision: 4 });
diagnostics.record("workspace", "workspace.session-directory", {
  workspaceSessions: 7,
  visibleSessions: 5,
  hiddenSessions: 2,
  hiddenByCollapsed: 1,
  hiddenByTruncation: 1,
  runtimeOnlySessions: 1,
  recoveryCopies: 2,
  changeReason: "recovery-copy",
  workspaceRoot: "C:\\Users\\private-user\\workspace",
  topicId: "private-topic-id",
  label: "PRIVATE_TITLE",
});

const active = diagnostics.getSnapshot();
assert.equal(active.status, "recording");
assert.equal(active.eventCount, 32, "ring buffer remains bounded");
assert.ok(active.droppedEventCount > 0, "overflow is visible in the summary");
assert.equal(active.markerCount, 0, "evicted markers are not counted as retained");

const payload = diagnostics.stop();
assert.equal(payload.schemaVersion, 1);
assert.equal(payload.manifest.platform, "windows");
assert.equal(payload.events[payload.events.length - 1]?.type, "stop");
const serialized = JSON.stringify(payload);
assert.equal(serialized.includes("scrollToIndex"), true, "scroll writer details remain available for analysis");
assert.equal(serialized.includes("targetIndex"), true, "scroll target remains available for analysis");
assert.equal(serialized.includes("recovery-end"), true, "scroll transition source remains available for analysis");
assert.equal(serialized.includes("history.items-patch"), true, "history patch event remains available for analysis");
assert.equal(serialized.includes("layoutVersion"), true, "row layout version remains available for analysis");
assert.equal(serialized.includes("layoutVariant"), true, "row layout variant remains available for analysis");
assert.equal(serialized.includes("estimateSource"), true, "height estimate source remains available for analysis");
assert.equal(serialized.includes("workspace.session-directory"), true, "sidebar directory event remains available for analysis");
assert.equal(serialized.includes("workspaceSessions"), true, "sidebar session counts remain available for analysis");
for (const forbidden of ["PRIVATE_TEXT", "PRIVATE_TOKEN", "private-user", "private-tab", "path", "token", "tabId"]) {
  assert.equal(serialized.includes(forbidden), false, `payload excludes ${forbidden}`);
}

const frozenCount = diagnostics.getSnapshot().eventCount;
diagnostics.record("browser", "scroll", { scrollTop: 999 });
assert.equal(diagnostics.getSnapshot().eventCount, frozenCount, "stopped recorder remains frozen");

assert.equal(isFrontendDiagnosticsBuild("test", false), true);
assert.equal(isFrontendDiagnosticsBuild("canary", false), true);
assert.equal(isFrontendDiagnosticsBuild("preview", false), true);
assert.equal(isFrontendDiagnosticsBuild("stable", true), true);
assert.equal(isFrontendDiagnosticsBuild("stable", false), false);

console.log("frontend diagnostics tests passed");
