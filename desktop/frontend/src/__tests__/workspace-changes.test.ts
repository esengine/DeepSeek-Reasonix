// Run: tsx src/__tests__/workspace-changes.test.ts

import {
  workspaceChangeRows,
  workspaceGitStatusLabel,
  workspaceVisibleChangeCount,
} from "../lib/workspaceChanges";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

const labels: Record<string, string> = {
  "workspace.gitStatusAdded": "Added",
  "workspace.gitStatusCopied": "Copied",
  "workspace.gitStatusDeleted": "Deleted",
  "workspace.gitStatusModified": "Modified",
  "workspace.gitStatusRenamed": "Renamed",
  "workspace.gitStatusUnmerged": "Conflict",
  "workspace.gitStatusUntracked": "Untracked",
  "workspace.turnBadge": "Turn {turn}",
};

function t(key: string, vars?: Record<string, string | number>): string {
  let value = labels[key] ?? key;
  for (const [name, replacement] of Object.entries(vars ?? {})) {
    value = value.replace(`{${name}}`, String(replacement));
  }
  return value;
}

console.log("\nworkspace changes contract");

eq(workspaceGitStatusLabel("??", t), "Untracked", "porcelain untracked status is user-readable");
eq(workspaceGitStatusLabel(" M", t), "Modified", "porcelain modified status is user-readable");
eq(workspaceGitStatusLabel("R", t), "Renamed", "porcelain rename status is user-readable");

const view = {
  gitAvailable: true,
  files: [
    {
      path: "parse_ber.py",
      sources: ["session", "git"],
      gitStatus: "??",
      turns: [0, 12],
      latestPrompt: "latest edit",
      latestTime: 2000,
    },
    {
      path: "README.md",
      sources: ["git"],
      gitStatus: " M",
    },
  ],
  records: [
    {
      key: "0:parse_ber.py",
      path: "parse_ber.py",
      sources: ["session", "git"],
      gitStatus: "??",
      turn: 0,
      prompt: "first edit",
      time: 1000,
    },
    {
      key: "12:parse_ber.py",
      path: "parse_ber.py",
      sources: ["session", "git"],
      gitStatus: "??",
      turn: 12,
      prompt: "thirteenth edit",
      time: 2000,
    },
  ],
} as any;

const rows = workspaceChangeRows(view, t);
eq(rows.length, 3, "session records and git-only files both appear in changes");
eq(workspaceVisibleChangeCount(view), 3, "visible count reflects records instead of unique session files");
eq(rows[0]?.detail, "thirteenth edit", "newest session record is listed first");
eq(rows[0]?.badges.join(","), "Turn 13,Untracked", "session record shows turn and readable git status");
ok(!rows.some((row) => row.badges.includes("??")), "raw git porcelain status is not shown as a badge");
eq(rows[2]?.path, "README.md", "git-only file remains visible");
eq(rows[2]?.badges.join(","), "Modified", "git-only file uses readable git status");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
