// Run: npx tsx src/__tests__/project-tree-shell-race.test.ts
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const topic = readFileSync(join(root, "lib/projectTreeTopic.ts"), "utf8");
const panel = readFileSync(join(root, "components/ProjectTree.tsx"), "utf8");

assert.match(topic, /projectTreeShouldApplyShellSnapshot/, "shell race helper exported");
assert.match(topic, /treeEmpty/, "empty-tree shells bypass catalog revision watermark");
assert.match(panel, /projectTreeShouldApplyShellSnapshot/, "ProjectTree uses shell race helper");
assert.match(panel, /treeRef\.current\.length === 0/, "v2 event re-fetches shell when tree is empty");
assert.match(panel, /void refresh\(\)/, "empty-tree event path calls refresh");
assert.match(
  panel,
  /onProjectTreeChangedV2[\s\S]*projectTreeRevisionIsFresh\(latestRevisionRef\.current, event\.revision\)/,
  "equal-revision overlay events use the shared freshness contract",
);
assert.doesNotMatch(
  panel,
  /event\.revision\s*<=\s*latestRevisionRef\.current/,
  "equal-revision tombstone overlays are not discarded",
);

console.log("  PASS  project tree shell race contract");
