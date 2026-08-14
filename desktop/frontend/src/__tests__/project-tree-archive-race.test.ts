import assert from "node:assert/strict";
import {
  enqueueProjectTreeArchive,
  projectTreeTrashingTopics,
  runProjectTreeArchiveJob,
} from "../lib/projectTreeArchive";
import {
  invalidateProjectTreeTopicLoads,
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
  let pending = true;
  let recoveryObservedPending: boolean | null = null;
  const job = runProjectTreeArchiveJob({
    archive: async () => { throw new Error("busy"); },
    reload: async () => {},
    finishPending: () => { pending = false; },
    recover: async () => { recoveryObservedPending = pending; },
  });

  assert.equal(await job, false);
  assert.equal(recoveryObservedPending, false);
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
  assert.match(archiveSource, /invalidateProjectTreeTopicLoads[\s\S]*optimisticallyRemoveTopic\(topicId\)/);
  assert.match(source, /projectTreeWithoutTopics\(asArray\(page\.items\), currentArchiveTombstones\(\)\)/);
  assert.match(archiveSource, /archiveQueueRef\.current\.catch[\s\S]*\.then\(async \(\) =>/);
  assert.match(archiveSource, /pendingLoads = targets\.map[\s\S]*onReloadStarted[\s\S]*await Promise\.all\(pendingLoads\)/);
  assert.match(archiveSource, /onReloadStarted: \(\) => releaseArchiveTombstone\(topicId\)/);
  assert.match(archiveSource, /await refreshRef\.current\(reloadOptions\)[\s\S]*finally \{[\s\S]*endTrashingTopic\(topicId\)/);
}

await testLatePreArchivePageCannotReinsertTopic();
await testPendingTombstonesFilterEveryIncomingPage();
await testPostCommitRestoreCanWinWhilePendingIndicatorFinishes();
await testConcurrentArchivesReachBackendSerially();
await testPendingEndsOnlyAfterCanonicalReload();
await testFailedArchiveRestoresVisibilityBeforeRecoveryReload();
testProjectTreeWiresEveryRaceGuard();
console.log("project tree archive race: 7 passed");
