// Run: tsx src/__tests__/delivery-worktree.test.ts
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const dir = dirname(fileURLToPath(import.meta.url));
const source = (path: string) => readFileSync(resolve(dir, path), "utf8");
const bridge = source("../lib/bridge.ts");
const tree = source("../components/ProjectTree.tsx");
const tabs = source("../components/TabBar.tsx");
const app = source("../App.tsx");
const badge = source("../components/WorktreeBadge.tsx");

let failed = 0;
function ok(value: unknown, label: string) {
  if (value) process.stdout.write(`  PASS  ${label}\n`);
  else {
    failed += 1;
    process.stdout.write(`  FAIL  ${label}\n`);
  }
}

console.log("\ndelivery worktree");
ok(/DeliveryWorktreeAvailability\(workspaceRoot: string\)/.test(bridge), "bridge exposes non-mutating availability probe");
ok(/CreateDeliveryWorktree\(workspaceRoot: string\)/.test(bridge), "bridge exposes isolated workspace creation");
ok(/app\.DeliveryWorktreeAvailability\(projectRoot\)/.test(tree), "project menu probes Git before enabling isolation");
ok(/disabled: isolatingProject !== null \|\| isolationAvailability\?\.available === false/.test(tree), "menu disables unavailable or duplicate creation");
ok(/onCreateDeliveryWorktree\?\.\(workspaceRoot\)/.test(tree), "project menu delegates isolated workspace creation");
ok(/kind: "delivery-worktree"/.test(app) && /enqueueNavigation\(\{ kind: "delivery-worktree"/.test(app), "creation shares the last-click-wins navigation queue");
ok(/sourceDirty[\s\S]*worktreeCreatedDirty/.test(app), "dirty source checkout receives an explicit warning");
ok(/isolatedWorktree && <WorktreeBadge/.test(tabs), "tab strip identifies isolated worktrees");
ok(/activeTab\?\.isolatedWorktree && <WorktreeBadge/.test(app), "topic bar identifies isolated worktrees");
ok(/node\.isolatedWorktree && <WorktreeBadge/.test(tree), "project tree identifies isolated worktrees");
ok(/GitBranch/.test(badge) && /#6119/.test(badge), "shared badge preserves the credited #6119 design contribution");

if (failed) process.exit(1);
console.log("delivery worktree tests passed");
