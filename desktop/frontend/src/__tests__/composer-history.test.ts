// Run: tsx src/__tests__/composer-history.test.ts
//
// Tests for composerHistory.ts (backend-backed prompt history with nonce caching).

import { invalidateCache, snapshot, pushHistory, clearHistory, loadOlder } from "../lib/composerHistory";
import type { PromptHistoryEntry, PromptHistoryResult } from "../lib/types";

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

// Ensure window exists so bridge's realApp() check works.
function ensureWindow() {
  if (typeof window === "undefined") {
    (globalThis as Record<string, unknown>).window = {} as Window & typeof globalThis;
  }
}

// Install a mock ScanPromptHistory on window.go so the bridge proxy finds it.
function setMock(
  mock: (
    nonce: string,
  ) => Promise<
    | { entries: PromptHistoryEntry[] | null; nonce?: string }
    | PromptHistoryResult
    | PromptHistoryEntry[]
    | [PromptHistoryEntry[] | null, string]
    | { "0"?: PromptHistoryEntry[] | null; "1"?: string; [key: string]: unknown }
    | null
  >,
) {
  ensureWindow();
  const w = window as unknown as { go?: Record<string, unknown> };
  w.go = {
    main: { App: { ScanPromptHistory: mock } as never },
  };
  invalidateCache();
}

async function testSnapshotSupportsArrayResult() {
  setMock(async () => [{ text: "array 1", at: Date.now(), sessionPath: "/mock/a.jsonl", turn: 0 }]);
  const entries = await snapshot();
  eq(entries.length, 1, "array result is supported");
  eq(entries[0].text, "array 1", "array result text");
}

async function testSnapshotSupportsTupleResult() {
  setMock(async () => [[{ text: "tuple 1", at: Date.now(), sessionPath: "/mock/a.jsonl", turn: 0 }], "tuple-nonce"]);
  const entries = await snapshot();
  eq(entries.length, 1, "tuple result is supported");
  eq(entries[0].text, "tuple 1", "tuple result text");
}

async function testSnapshotSupportsMapTupleResult() {
  setMock(async () => ({
    0: [{ text: "map tuple 1", at: Date.now(), sessionPath: "/mock/a.jsonl", turn: 0 }],
    1: "map-tuple-nonce",
  }));
  const entries = await snapshot();
  eq(entries.length, 1, "map tuple result is supported");
  eq(entries[0].text, "map tuple 1", "map tuple result text");
}

async function testSnapshotSupportsNullResult() {
  setMock(async () => null);
  const entries = await snapshot();
  eq(entries.length, 0, "null result is supported");
}

async function testTupleCacheHitRespectsTupleNonce() {
  let callCount = 0;
  setMock(async (nonce) => {
    callCount++;
    if (nonce === "tuple-hit") {
      return [null, "tuple-hit"];
    }
    return [[{ text: "tuple cache", at: Date.now(), sessionPath: "/mock/a.jsonl", turn: 0 }], "tuple-hit"];
  });

  const r1 = await snapshot();
  eq(r1.length, 1, "tuple first call returns entry");
  const r2 = await snapshot();
  eq(r2.length, 1, "tuple cache hit returns cached entry");
  eq(r2[0].text, "tuple cache", "tuple cache hit keeps cached text");
  eq(callCount, 1, "snapshot reuses loaded tape entries");
}

// --- Test 1: snapshot returns entries from backend ---
async function testSnapshotReturnsEntries() {
  setMock(async (_nonce) => ({
    entries: [{ text: "hello", at: Date.now(), sessionPath: "/mock/a.jsonl", turn: 0 }],
    nonce: "n1",
  }));
  const entries = await snapshot();
  eq(entries.length, 1, "returns 1 entry");
  eq(entries[0].text, "hello", "correct text");
}

// --- Test 2: cache hit reuses previous nonce ---
async function testCacheHit() {
  let callCount = 0;
  setMock(async (nonce) => {
    callCount++;
    if (nonce === "cached-nonce") {
      return { entries: null, nonce: "cached-nonce" };
    }
    return { entries: [{ text: "hello", at: Date.now(), sessionPath: "/mock/a.jsonl", turn: 0 }], nonce: "cached-nonce" };
  });

  const r1 = await snapshot();
  eq(r1.length, 1, "first call returns entry");
  eq(callCount, 1, "first call hits backend");

  // Second call: same nonce → backend returns nil (cache hit).
  const r2 = await snapshot();
  eq(r2.length, 1, "cache hit returns cached entry");
  eq(r2[0].text, "hello", "cache hit keeps cached text");
  eq(callCount, 1, "second snapshot reads loaded tape entries");

  // After invalidate: resets nonce to "" → backend returns fresh.
  invalidateCache();
  const r3 = await snapshot();
  eq(r3.length, 1, "after invalidate re-fetches");
  eq(callCount, 2, "after invalidate, backend called");
}

async function testLoadOlderUsesCursor() {
  const seenCursors: string[] = [];
  setMock(async (request) => {
    const parsed = JSON.parse(request || "{}") as { cursor?: string; limit?: number };
    seenCursors.push(parsed.cursor ?? "");
    if (!parsed.cursor) {
      return {
        entries: [{ text: "page 1", at: 2, sessionPath: "/mock/a.jsonl", turn: 1 }],
        nonce: "tape",
        olderCursor: "cursor-2",
        hasOlder: true,
      };
    }
    return {
      entries: [{ text: "page 2", at: 1, sessionPath: "/mock/b.jsonl", turn: 0 }],
      nonce: "tape",
      olderCursor: "",
      hasOlder: false,
    };
  });

  const first = await loadOlder();
  const second = await loadOlder();
  const all = await snapshot();
  eq(first[0].text, "page 1", "first page text");
  eq(second[0].text, "page 2", "second page text");
  eq(all.length, 2, "snapshot contains loaded tape pages");
  eq(seenCursors[0], "", "first request has empty cursor");
  eq(seenCursors[1], "cursor-2", "second request uses older cursor");
}

// --- Test 2.5: first ArrowUp press should recall newest entry first ---
async function testFirstArrowUpIsMostRecent() {
  setMock(async (nonce) => {
    if (nonce === "u1") {
      return { entries: null, nonce: "u1" };
    }
    return {
      entries: [
        { text: "newest", at: 3000, sessionPath: "/mock/a.jsonl", turn: 0 },
        { text: "middle", at: 2000, sessionPath: "/mock/a.jsonl", turn: 1 },
        { text: "oldest", at: 1000, sessionPath: "/mock/a.jsonl", turn: 2 },
      ],
      nonce: "u1",
    };
  });

  const first = await snapshot();
  eq(first.length, 3, "first ArrowUp press sees 3 history entries");
  eq(first[0].text, "newest", "first recalled prompt is the newest entry");

  const second = await snapshot();
  eq(second.length, 3, "cache hit returns cached history");
  eq(second[0].text, "newest", "cache hit keeps newest first");

  invalidateCache();
  const third = await snapshot();
  eq(third.length, 3, "after invalidation first ArrowUp can still recall again");
  eq(third[0].text, "newest", "after invalidation still recalls newest first");
}

// --- Test 3: snapshot returns empty on error ---
async function testSnapshotError() {
  setMock(async (_nonce) => { throw new Error("backend failed"); });
  const entries = await snapshot();
  eq(entries.length, 0, "empty on error");
}

// --- Test 4: invalidateCache resets nonce to "" ---
async function testInvalidateResetsNonce() {
  const seenNonces: string[] = [];
  setMock(async (request) => {
    const parsed = JSON.parse(request || "{}") as { nonce?: string };
    seenNonces.push(parsed.nonce ?? "");
    return { entries: [{ text: "msg", at: Date.now(), sessionPath: "/mock/a.jsonl", turn: 0 }], nonce: "server-nonce", olderCursor: "", hasOlder: false };
  });

  invalidateCache();
  await loadOlder();
  eq(seenNonces.length, 1, "calls backend once after invalidate");
  eq(seenNonces[0], "", "nonce is '' after invalidate");
}

// --- Test 5: pushHistory and clearHistory are no-ops ---
async function testNoopFunctions() {
  // These should not throw.
  pushHistory("some text");
  clearHistory();
  eq(true, true, "push/clear do not throw");
}

// --- main ----------------------------------------------------------------

console.log("\ncomposerHistory");

(async () => {
  const tests: [string, () => Promise<void>][] = [
    ["returns entries from backend", testSnapshotReturnsEntries],
    ["cache hit reuses nonce", testCacheHit],
    ["loadOlder follows cursor", testLoadOlderUsesCursor],
    ["first ArrowUp press recalls newest entry first", testFirstArrowUpIsMostRecent],
    ["supports array return shape", testSnapshotSupportsArrayResult],
    ["supports tuple return shape", testSnapshotSupportsTupleResult],
    ["supports map tuple result", testSnapshotSupportsMapTupleResult],
    ["supports null result", testSnapshotSupportsNullResult],
    ["supports tuple cache-hit result", testTupleCacheHitRespectsTupleNonce],
    ["returns empty on error", testSnapshotError],
    ["invalidate resets nonce", testInvalidateResetsNonce],
    ["push/clear are no-ops", testNoopFunctions],
  ];

  for (const [name, fn] of tests) {
    try {
      await fn();
    } catch (e) {
      process.stdout.write(`  FAIL  ${name} threw: ${e}\n`);
      failed++;
    }
  }

  console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
  if (failed > 0) process.exit(1);
})();
