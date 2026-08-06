// Run: tsx src/__tests__/workspace-change-status.test.ts

import { workspaceGitStatusLabel, workspaceGitStatusLabelKey } from "../lib/workspaceChanges";
import { en } from "../locales/en";
import { zh } from "../locales/zh";
import { zhTW } from "../locales/zh-TW";

let passed = 0;
let failed = 0;

function eq<T>(actual: T, expected: T, label: string) {
  if (Object.is(actual, expected)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n    expected: ${String(expected)}\n    actual:   ${String(actual)}\n`);
    failed += 1;
  }
}

const t = ((key: string) => (en as Record<string, string>)[key] ?? key) as never;

console.log("\nworkspace git status labels");

eq(workspaceGitStatusLabel("??", t), "Untracked", "?? reads as untracked instead of leaking porcelain");
eq(workspaceGitStatusLabel(" M", t), "Modified", "unstaged modification");
eq(workspaceGitStatusLabel("A ", t), "Added", "staged addition");
eq(workspaceGitStatusLabel("D ", t), "Deleted", "deletion");
eq(workspaceGitStatusLabel("R ", t), "Renamed", "rename");
eq(workspaceGitStatusLabel("UU", t), "Conflict", "unmerged conflict");
eq(workspaceGitStatusLabel("MM", t), "Modified", "staged and unstaged modification");

eq(workspaceGitStatusLabel(undefined, t), "", "missing status renders nothing");
eq(workspaceGitStatusLabel("  ", t), "", "blank status renders nothing");
eq(workspaceGitStatusLabel("!!", t), "!!", "an unmapped code passes through rather than vanishing");

eq(workspaceGitStatusLabelKey("DD"), "workspace.gitStatusUnmerged", "DD is a merge conflict, not a plain deletion");
eq(workspaceGitStatusLabelKey("AA"), "workspace.gitStatusUnmerged", "AA is a merge conflict, not a plain addition");
eq(workspaceGitStatusLabelKey("UD"), "workspace.gitStatusUnmerged", "UD is a merge conflict");

for (const [name, dict] of [["zh", zh], ["zh-TW", zhTW]] as const) {
  const missing = Object.keys(en)
    .filter((key) => key.startsWith("workspace.gitStatus"))
    .filter((key) => !(key in (dict as Record<string, string>)));
  eq(missing.length, 0, `${name} translates every git status label (${missing.join(", ")})`);
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
