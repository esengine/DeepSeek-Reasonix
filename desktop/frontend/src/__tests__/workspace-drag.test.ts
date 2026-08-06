// Run: tsx src/__tests__/workspace-drag.test.ts
//
// Coverage for the workspace-reference drag serialization helpers
// (src/lib/workspaceDrag.ts): formatWorkspaceReference / parseWorkspaceReference
// round-trip an "@path" text payload, with directories carrying a trailing "/".

import { formatWorkspaceReference, parseWorkspaceReference } from "../lib/workspaceDrag";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nworkspace-drag reference format/parse");

// ── formatWorkspaceReference ────────────────────────────────────────
eq(formatWorkspaceReference("/repo/src/main.ts"), "@/repo/src/main.ts", "file path is prefixed with @");
eq(formatWorkspaceReference("/repo/src"), "@/repo/src", "non-directory without trailing slash stays as-is");
eq(formatWorkspaceReference("/repo/src", true), "@/repo/src/", "directory gets a trailing slash");
eq(formatWorkspaceReference("/repo/src/", true), "@/repo/src/", "directory already ending in slash is not doubled");
eq(formatWorkspaceReference("relative/path"), "@relative/path", "relative path is serialized unchanged");
eq(formatWorkspaceReference(""), "@", "empty path serializes to a bare @");

// ── parseWorkspaceReference ─────────────────────────────────────────
eq(parseWorkspaceReference("@/repo/src/main.ts"), { path: "/repo/src/main.ts", isDir: false }, "file reference parses with isDir false");
eq(parseWorkspaceReference("@/repo/src/"), { path: "/repo/src/", isDir: true }, "trailing slash parses as isDir true");
eq(parseWorkspaceReference("  @/repo/src/main.ts  "), { path: "/repo/src/main.ts", isDir: false }, "surrounding whitespace is trimmed");
eq(parseWorkspaceReference("@relative/path"), { path: "relative/path", isDir: false }, "relative reference parses");
eq(parseWorkspaceReference("@"), null, "bare @ (empty path) returns null");
eq(parseWorkspaceReference(""), null, "empty string returns null");
eq(parseWorkspaceReference("   "), null, "whitespace-only string returns null");
eq(parseWorkspaceReference("no-at-prefix"), null, "text without @ prefix returns null");
eq(parseWorkspaceReference("a @b"), null, "@ in the middle of text returns null");
eq(parseWorkspaceReference("@has space"), null, "@ followed by whitespace returns null");

// ── round-trips ─────────────────────────────────────────────────────
eq(
  parseWorkspaceReference(formatWorkspaceReference("/repo/src/main.ts")),
  { path: "/repo/src/main.ts", isDir: false },
  "file round-trips through format then parse",
);
eq(
  parseWorkspaceReference(formatWorkspaceReference("/repo/src", true)),
  { path: "/repo/src/", isDir: true },
  "directory round-trips through format then parse",
);

console.log(`\n${passed} passed, ${failed} failed\n`);
process.exit(failed === 0 ? 0 : 1);
