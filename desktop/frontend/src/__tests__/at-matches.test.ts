// Run: tsx src/__tests__/at-matches.test.ts
//
// Regression coverage for the Composer @-menu filter (issue #3769).
// The frontend must mirror the backend fuzzy @-search contract:
// a fragment like "planind" should surface "src/planind/index.tsx"
// because the search results return full slash-normalized relative
// paths, not basenames.

import { filterAtMatches } from "../lib/atMatches";
import type { DirEntry } from "../lib/types";

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

function entry(name: string, isDir = false): DirEntry {
  return { name, isDir };
}

console.log("\nat-matches filter");

// 1. Nested file surfaces when fragment matches an intermediate segment.
{
  const entries: DirEntry[] = [];
  const searchEntries = [entry("src/planind/index.tsx")];
  const got = filterAtMatches(entries, searchEntries, "planind");
  eq(
    got.map((e) => e.name),
    ["src/planind/index.tsx"],
    "src/planind/index.tsx + planind → surfaces the nested file",
  );
}

// 2. Basename hit is preserved (regression guard for the legacy v1 path).
{
  const entries: DirEntry[] = [];
  const searchEntries = [entry("src/planind/index.tsx")];
  const got = filterAtMatches(entries, searchEntries, "index");
  eq(
    got.map((e) => e.name),
    ["src/planind/index.tsx"],
    "src/planind/index.tsx + index → still surfaces via basename",
  );
}

// 3. Local ListDir hit and fuzzy Search hit with the same name are de-duped
// to a single entry, with the local one taking precedence (it appears first).
{
  const entries = [entry("planind.go")];
  const searchEntries = [entry("planind.go")];
  const got = filterAtMatches(entries, searchEntries, "planind");
  eq(
    got.map((e) => e.name),
    ["planind.go"],
    "entries ∩ searchEntries with same name dedup to one",
  );
}

// 4. Local ListDir entries with a matching fragment appear first; fuzzy hits
// fill the rest in the order the backend returned them.
{
  const entries = [entry("planind.go"), entry("planind.md")];
  const searchEntries = [entry("src/planind/index.tsx")];
  const got = filterAtMatches(entries, searchEntries, "planind");
  eq(
    got.map((e) => e.name),
    ["planind.go", "planind.md", "src/planind/index.tsx"],
    "local entries precede fuzzy search hits, no dupes",
  );
}

// 5. Empty fragment matches every entry because includes("") is true; this
// pins the current behavior so a future change that re-introduces
// basename-only matching or skips empty fragments is caught immediately.
{
  const entries = [entry("a.ts"), entry("b.ts")];
  const searchEntries = [entry("src/c.ts")];
  const got = filterAtMatches(entries, searchEntries, "");
  eq(
    got.map((e) => e.name),
    ["a.ts", "b.ts", "src/c.ts"],
    "empty fragment includes every entry (legacy behavior preserved)",
  );
}

console.log(`\n${passed} passed, ${failed} failed\n`);
process.exit(failed === 0 ? 0 : 1);
