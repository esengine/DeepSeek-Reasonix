// Run: tsx src/__tests__/topic-worktree.test.ts
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const dir = dirname(fileURLToPath(import.meta.url));
const source = (path: string) => readFileSync(resolve(dir, path), "utf8");
const bridge = source("../lib/bridge.ts");
const tree = source("../components/ProjectTree.tsx");
const app = source("../App.tsx");
const controller = source("../lib/useController.ts");

let failed = 0;
function ok(value: unknown, label: string) {
  if (value) process.stdout.write(`  PASS  ${label}\n`);
  else {
    failed += 1;
    process.stdout.write(`  FAIL  ${label}\n`);
  }
}

console.log("\ntopic worktree (#4304)");
ok(/TopicWorktreeAvailability\(workspaceRoot: string\)/.test(bridge), "bridge exposes topic worktree availability probe");
ok(/CreateTopicWorktree\(workspaceRoot: string\)/.test(bridge), "bridge exposes topic worktree creation");
ok(/onCreateTopicWorktree\?\.\(workspaceRoot\)/.test(tree), "project menu delegates topic worktree creation");
ok(/key: "topic-worktree"/.test(tree), "project menu offers opt-in topic worktree item");
ok(/kind: "topic-worktree"/.test(app) && /enqueueNavigation\(\{ kind: "topic-worktree"/.test(app), "creation shares the last-click-wins navigation queue");
ok(/createTopicWorktree/.test(controller) && /app\.CreateTopicWorktree/.test(controller), "controller opens the created topic worktree tab");
ok(/activeTab\?\.sourceRoot \|\| activeTab\?\.workspaceRoot/.test(app), "sidebar highlights the source project for topic worktrees");
ok(/topicWorktreeCreatedDirty/.test(app), "dirty source checkout receives an explicit warning");
ok(/sourceRoot\?: string/.test(source("../lib/types.ts")), "tab meta carries sourceRoot for logical project identity");

if (failed) process.exit(1);
console.log("topic worktree tests passed");
