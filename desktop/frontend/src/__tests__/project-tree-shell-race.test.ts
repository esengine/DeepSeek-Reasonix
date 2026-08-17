// Run: npx tsx src/__tests__/project-tree-shell-race.test.ts
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const topic = readFileSync(join(root, "lib/projectTreeTopic.ts"), "utf8");
const runtime = readFileSync(join(root, "lib/projectTreeRuntime.ts"), "utf8");
const runtimeHook = readFileSync(join(root, "lib/useProjectTreeRuntimeProjection.ts"), "utf8");
const bridge = readFileSync(join(root, "lib/bridge.ts"), "utf8");
const panel = readFileSync(join(root, "components/ProjectTree.tsx"), "utf8");

assert.match(topic, /projectTreeShouldApplyShellSnapshot/, "shell race helper exported");
assert.match(topic, /treeEmpty/, "empty-tree shells bypass catalog revision watermark");
assert.match(panel, /projectTreeShouldApplyShellSnapshot/, "ProjectTree uses shell race helper");
assert.match(panel, /treeRef\.current\.length === 0/, "v2 event re-fetches shell when tree is empty");
assert.match(panel, /void refresh\(\)/, "empty-tree event path calls refresh");
assert.match(
  panel,
  /projectTreeTopicPageIsFresh\(topicRevisionRef\.current, key, page\.revision\)/,
  "topic pages compare revisions within their own project",
);
assert.doesNotMatch(
  panel,
  /projectTreeRevisionIsFresh\(latestRevisionRef\.current, page\.revision\)/,
  "an unrelated project's revision cannot discard a valid slower topic page",
);
assert.match(
  panel,
  /onProjectTreeChangedV2[\s\S]*projectTreeRevisionIsFresh\(latestRevisionRef\.current, event\.revision\)/,
  "equal-revision catalog events use the shared freshness contract",
);
assert.match(runtime, /onProjectTreeRuntimeChanged/, "runtime projection has a dedicated Wails subscription");
assert.match(runtimeHook, /bindProjectTreeRuntime/, "ProjectTree binds the runtime projection after mount");
assert.match(runtimeHook, /GetProjectTreeRuntimeSnapshot/, "runtime subscription reconciles with a post-subscribe snapshot");
assert.match(bridge, /reason !== "runtime"/, "current frontend ignores tagged legacy runtime invalidations");
assert.match(bridge, /reason !== "catalog-v2"/, "current frontend ignores catalog v2 compatibility invalidations");
assert.doesNotMatch(
  runtime,
  /ListProjectTopics/,
  "runtime events never reload catalog topic pages",
);
assert.doesNotMatch(
  panel,
  /event\.revision\s*<=\s*latestRevisionRef\.current/,
  "equal-revision tombstone overlays are not discarded",
);
assert.doesNotMatch(panel, /setIndexingDone\(/, "topic refresh no longer waits for a global all-directories gate");
assert.match(panel, /projectTreeEventAffectsFolder\(project, affected\)/, "directory revisions refresh only affected expanded projects");
assert.match(
  panel,
  /projectTreeShellSignature/,
  "debounced reload observes project arrivals through a shell signature",
);
assert.doesNotMatch(
  panel,
  /\[\s*expanded,\s*loadProjectTopics,\s*query,\s*timeFilter,\s*tree\s*\]/,
  "debounced reload depends on the shell signature, not tree (topic loads would re-arm it forever)",
);

console.log("  PASS  project tree shell race contract");
