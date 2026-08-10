import assert from "node:assert/strict";
import { coalescesQuery, invalidateSharedQuery, resetQueryCoalescing, shareQuery } from "../lib/queryCoalesce";

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

async function identicalCallsInFlightShareOneAnswer() {
  resetQueryCoalescing();
  let runs = 0;
  const d = deferred<string>();
  const run = () => { runs++; return d.promise; };
  const a = shareQuery("ContextPanel", ["tab-1"], run);
  const b = shareQuery("ContextPanel", ["tab-1"], run);
  d.resolve("panel");
  assert.equal(await a, "panel");
  assert.equal(await b, "panel");
  assert.equal(runs, 1, "a duplicate inside the window must not reach the backend");
}

async function differentArgumentsStayIndependent() {
  resetQueryCoalescing();
  let runs = 0;
  const run = async () => { runs++; return runs; };
  await shareQuery("MetaForTab", ["tab-1"], run);
  await shareQuery("MetaForTab", ["tab-2"], run);
  assert.equal(runs, 2, "another tab is another question");
}

// A stale answer is worse than a slow one: a failure must not be inherited.
async function rejectionIsNotSharedOnward() {
  resetQueryCoalescing();
  let runs = 0;
  const first = shareQuery("ListTabs", [], async () => { runs++; throw new Error("boom"); });
  await assert.rejects(first);
  const second = await shareQuery("ListTabs", [], async () => { runs++; return "ok"; });
  assert.equal(second, "ok");
  assert.equal(runs, 2, "the next caller retries instead of inheriting the failure");
}

async function invalidationStartsAFreshRead() {
  resetQueryCoalescing();
  let runs = 0;
  const run = async () => ++runs;
  assert.equal(await shareQuery("MetaForTab", ["tab-1"], run), 1);
  invalidateSharedQuery("MetaForTab", ["tab-1"]);
  assert.equal(await shareQuery("MetaForTab", ["tab-1"], run), 2);
}

async function settledTabListsAreNotShared() {
  resetQueryCoalescing();
  let runs = 0;
  const run = async () => ++runs;
  assert.equal(await shareQuery("ListTabs", [], run), 1);
  assert.equal(await shareQuery("ListTabs", [], run), 2, "tab mutations must be visible after the in-flight burst");
}

// Anything that mutates or starts work must never be collapsed.
function onlyReadOnlyQueriesAreCoalesced() {
  for (const readOnly of ["ListProjectTree", "ContextPanel", "MetaForTab", "BalanceForTab"]) {
    assert.equal(coalescesQuery(readOnly), true, `${readOnly} should coalesce`);
  }
  for (const mutating of ["Send", "SetActiveTab", "OpenGlobalTab", "Cancel", "ApproveTool"]) {
    assert.equal(coalescesQuery(mutating), false, `${mutating} must never be coalesced`);
  }
}

const tests: Array<[string, () => unknown]> = [
  ["identical in-flight calls share one answer", identicalCallsInFlightShareOneAnswer],
  ["different arguments stay independent", differentArgumentsStayIndependent],
  ["a rejection is not shared onward", rejectionIsNotSharedOnward],
  ["external state boundaries invalidate settled answers", invalidationStartsAFreshRead],
  ["settled tab lists do not cross mutations", settledTabListsAreNotShared],
  ["only read-only queries are coalesced", onlyReadOnlyQueriesAreCoalesced],
];

let failed = 0;
for (const [name, fn] of tests) {
  try {
    await fn();
    console.log(`ok  ${name}`);
  } catch (err) {
    failed++;
    console.error(`FAIL ${name}\n  ${(err as Error).message}`);
  }
}
if (failed > 0) process.exit(1);
