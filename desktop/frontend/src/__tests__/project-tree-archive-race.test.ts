import assert from "node:assert/strict";
import {
  enqueueProjectTreeArchive,
  projectTreeSessionArchiveTargetKey,
  projectTreeTopicArchiveTargetKey,
  projectTreeTrashingTopics,
  runProjectTreeArchiveJob,
} from "../lib/projectTreeArchive";
import {
  invalidateProjectTreeTopicLoads,
  projectTreeFolderKeyForSession,
  projectTreeWithoutTopics,
} from "../lib/projectTreeTopic";
import type { ProjectNode } from "../lib/types";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

async function testLatePreArchivePageCannotReinsertTopic() {
  const sequences: Record<string, number> = { project: 1 };
  const capturedSequence = sequences.project;
  const latePage = deferred<ProjectNode[]>();
  const applied = latePage.promise.then((items) =>
    sequences.project === capturedSequence ? items : [],
  );

  invalidateProjectTreeTopicLoads(sequences, ["project"]);
  latePage.resolve([{ key: "topic-a", kind: "topic", label: "A", topicId: "topic-a" }]);
  assert.deepEqual(await applied, []);
}

async function testPendingTombstonesFilterEveryIncomingPage() {
  const incoming: ProjectNode[] = [
    { key: "topic-a", kind: "topic", label: "A", topicId: "topic-a" },
    { key: "topic-b", kind: "topic", label: "B", topicId: "topic-b" },
    { key: "topic-c", kind: "topic", label: "C", topicId: "topic-c" },
  ];
  assert.deepEqual(
    projectTreeWithoutTopics(incoming, new Set(["topic-a", "topic-b"])).map((node) => node.topicId),
    ["topic-c"],
  );
}

async function testPostCommitRestoreCanWinWhilePendingIndicatorFinishes() {
  const pending = projectTreeTrashingTopics(new Set(), "topic-a", true);
  const tombstones = projectTreeTrashingTopics(new Set(), "topic-a", true);
  const sequences: Record<string, number> = { project: 2 };

  // Starting the post-commit canonical load fences every pre-commit response,
  // then releases only the tombstone. The visible pending state may remain
  // until that load finishes without hiding a legitimate later restore.
  invalidateProjectTreeTopicLoads(sequences, ["project"]);
  const releasedTombstones = projectTreeTrashingTopics(tombstones, "topic-a", false);
  const restored = projectTreeWithoutTopics(
    [{ key: "topic-a", kind: "topic", label: "Restored A", topicId: "topic-a" }],
    releasedTombstones,
  );
  assert.equal(pending.has("topic-a"), true);
  assert.equal(restored[0]?.topicId, "topic-a");
}

async function testConcurrentArchivesReachBackendSerially() {
  const firstGate = deferred<void>();
  const secondGate = deferred<void>();
  const firstStarted = deferred<void>();
  const secondStarted = deferred<void>();
  const calls: string[] = [];
  let tail = Promise.resolve();

  const first = enqueueProjectTreeArchive(tail, async () => {
    calls.push("a:start");
    firstStarted.resolve();
    await firstGate.promise;
    calls.push("a:end");
  });
  tail = first;
  const second = enqueueProjectTreeArchive(tail, async () => {
    calls.push("b:start");
    secondStarted.resolve();
    await secondGate.promise;
    calls.push("b:end");
  });
  tail = second;

  await firstStarted.promise;
  assert.deepEqual(calls, ["a:start"]);
  firstGate.resolve();
  await first;
  await secondStarted.promise;
  assert.deepEqual(calls, ["a:start", "a:end", "b:start"]);
  secondGate.resolve();
  await tail;
  assert.deepEqual(calls, ["a:start", "a:end", "b:start", "b:end"]);
}

async function testPendingEndsOnlyAfterCanonicalReload() {
  const reloadGate = deferred<void>();
  let pending = true;
  const job = runProjectTreeArchiveJob({
    archive: async () => {},
    commit: () => {},
    reload: () => reloadGate.promise,
    finishPending: () => { pending = false; },
    recover: async () => {},
  });

  await Promise.resolve();
  assert.equal(pending, true);
  reloadGate.resolve();
  assert.equal(await job, true);
  assert.equal(pending, false);
}

async function testFailedArchiveRestoresVisibilityBeforeRecoveryReload() {
  const backendGate = deferred<void>();
  let pending = true;
  let tombstones = new Set<string>();
  let recoveryObservedPending: boolean | null = null;
  const job = runProjectTreeArchiveJob({
    archive: () => backendGate.promise,
    commit: () => { tombstones = projectTreeTrashingTopics(tombstones, "topic-a", true); },
    reload: async () => {},
    finishPending: () => { pending = false; },
    recover: async () => { recoveryObservedPending = pending; },
  });

  await Promise.resolve();
  assert.equal(pending, true);
  assert.equal(tombstones.has("topic-a"), false, "pending backend work must not hide the topic");
  assert.equal(projectTreeWithoutTopics(
    [{ key: "topic-a", kind: "topic", label: "A", topicId: "topic-a" }],
    tombstones,
  ).length, 1);
  backendGate.reject(new Error("busy"));
  assert.equal(await job, false);
  assert.equal(tombstones.has("topic-a"), false, "rejected archives never commit a tombstone");
  assert.equal(recoveryObservedPending, false);
}

function testArchiveTargetsDoNotCollideAcrossRows() {
  assert.notEqual(
    projectTreeTopicArchiveTargetKey("project", "/a", "shared"),
    projectTreeTopicArchiveTargetKey("project", "/b", "shared"),
    "same-id topics in different projects keep independent confirmations",
  );
  assert.notEqual(
    projectTreeSessionArchiveTargetKey("/a/one.jsonl"),
    projectTreeSessionArchiveTargetKey("/a/two.jsonl"),
    "sessions in one topic keep independent confirmations",
  );
}

function testSessionArchiveReloadsItsOwningFolder() {
  const tree: ProjectNode[] = [{
    key: "project-a",
    kind: "project",
    label: "Project A",
    children: [{
      key: "topic-a",
      kind: "topic",
      label: "Topic A",
      children: [{
        key: "session-a",
        kind: "session",
        label: "Session A",
        sessionPath: " /sessions/a.jsonl ",
      }],
    }],
  }];
  assert.equal(projectTreeFolderKeyForSession(tree, "/sessions/a.jsonl"), "project-a");
  assert.equal(projectTreeFolderKeyForSession(tree, "/sessions/missing.jsonl"), "");
}

function testProjectTreeWiresEveryRaceGuard() {
  const source = readFileSync(
    join(dirname(fileURLToPath(import.meta.url)), "../components/ProjectTree.tsx"),
    "utf8",
  );
  const archiveSource = readFileSync(
    join(dirname(fileURLToPath(import.meta.url)), "../lib/projectTreeArchive.ts"),
    "utf8",
  );
  const sessionMenuSource = readFileSync(
    join(dirname(fileURLToPath(import.meta.url)), "../components/ProjectTreeSessionArchiveMenu.tsx"),
    "utf8",
  );
  assert.match(archiveSource, /await archive\(\)[\s\S]*commit\(\)[\s\S]*await reload\(\)/);
  assert.match(archiveSource, /commitArchiveTombstone\(topicId\)[\s\S]*invalidateProjectTreeTopicLoads[\s\S]*optimisticallyRemoveTopic\(topicId\)/);
  assert.match(source, /projectTreeWithoutTopics\(asArray\(page\.items\), currentArchiveTombstones\(\)\)/);
  assert.match(archiveSource, /return previous\.catch\(\(\) => undefined\)\.then\(work\)/);
  assert.match(archiveSource, /pendingLoads = targets\.map[\s\S]*onReloadStarted[\s\S]*await Promise\.all\(pendingLoads\)/);
  assert.match(archiveSource, /onReloadStarted: \(\) => releaseArchiveTombstone\(topicId\)/);
  assert.match(archiveSource, /finishPending: \(\) => endTrashingTopic\(topicId\)/);
  assert.match(source, /const topicMenuOpen = menuNodeKey === key/);
  assert.match(source, /onContextMenu=\{openTopicMenu\}/);
  assert.match(source, /node\.sessionPath \?\? ""/);
  assert.match(sessionMenuSource, /disabled: !sessionPath \|\| blocked \|\| busy/);
  assert.match(source, /sessionPath=\{sessionPath\} blocked=\{archiveBlocked \|\| topicTrashing\}/);
  assert.match(source, /void trashSession\(sessionPath\)/);
  assert.match(archiveSource, /projectTreeFolderKeyForSession\(treeRef\.current, sessionPath\)[\s\S]*refreshRef\.current\(reloadOptions\)/);
}

await testLatePreArchivePageCannotReinsertTopic();
await testPendingTombstonesFilterEveryIncomingPage();
await testPostCommitRestoreCanWinWhilePendingIndicatorFinishes();
await testConcurrentArchivesReachBackendSerially();
await testPendingEndsOnlyAfterCanonicalReload();
await testFailedArchiveRestoresVisibilityBeforeRecoveryReload();
testArchiveTargetsDoNotCollideAcrossRows();
testSessionArchiveReloadsItsOwningFolder();
testProjectTreeWiresEveryRaceGuard();
console.log("project tree archive race: 9 passed");
