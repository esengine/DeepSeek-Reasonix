import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

let generation = 0;
const statusWriteIsAllowed = (candidateGeneration: number, rebuildFailed: boolean) => !rebuildFailed && candidateGeneration === generation;
const renderedStates: string[] = [];
let resolveRemoteStatus: ((state: string) => void) | undefined;
const remoteStatus = new Promise<string>((resolve) => {
  resolveRemoteStatus = resolve;
});
const remoteGeneration = generation;
const lateRemoteWrite = remoteStatus.then((state) => {
  if (statusWriteIsAllowed(remoteGeneration, false)) renderedStates.push(state);
});

// The Wails rebuild rejects after the finished event has already started its
// status read. Restoring the retryable failure must fence out that late read.
generation += 1;
renderedStates.push("degraded");
resolveRemoteStatus?.("opening");
await lateRemoteWrite;
assert.deepEqual(
  renderedStates,
  ["degraded"],
  "a late pre-failure status response cannot replace the restored retry state",
);

const postFailureGeneration = generation;
if (statusWriteIsAllowed(postFailureGeneration, true)) renderedStates.push("opening");
assert.deepEqual(
  renderedStates,
  ["degraded"],
  "status reads started after a failed rebuild cannot replace its retry state during watcher startup",
);

const retryGeneration = generation;
if (statusWriteIsAllowed(retryGeneration, false)) renderedStates.push("ready");
assert.deepEqual(renderedStates, ["degraded", "ready"], "a new rebuild re-enables authoritative status writes");

const projectTreeSource = readFileSync(new URL("../components/ProjectTree.tsx", import.meta.url), "utf8");
assert.match(
  projectTreeSource,
  /catch \{\s*catalogRebuildFailedRef\.current = true; catalogStatusGenerationRef\.current \+= 1; setCatalogStatus\(catalogStatus\)/,
  "ProjectTree invalidates older status reads before restoring a failed rebuild",
);
assert.match(
  projectTreeSource,
  /GetSessionCatalogStatus\(\)[\s\S]*!catalogRebuildFailedRef\.current && catalogStatusGeneration === catalogStatusGenerationRef\.current/,
  "ProjectTree fences every event-driven status write behind rebuild ownership",
);
assert.match(
  projectTreeSource,
  /GetProjectTreeSnapshot\(\)[\s\S]*!catalogRebuildFailedRef\.current && catalogStatusGeneration === catalogStatusGenerationRef\.current/,
  "ProjectTree fences snapshot status writes behind the same rebuild ownership",
);

console.log("session catalog status ownership tests passed");
