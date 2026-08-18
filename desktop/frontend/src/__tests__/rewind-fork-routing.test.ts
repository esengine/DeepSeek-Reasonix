// Run: tsx src/__tests__/rewind-fork-routing.test.ts

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { dispatchPartialRewindNotice, partialRewindNotice, rewindFailureDetail, rewindOutcome } from "../lib/rewindCommit";

const testDir = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(testDir, "../App.tsx"), "utf8");
const controllerSource = readFileSync(resolve(testDir, "../lib/useController.ts"), "utf8");

assert.match(appSource, /const targetTabId = outcome\.tabId \|\| sourceTabId/);
assert.match(appSource, /undoTabId: sourceTabId/);
assert.match(appSource, /const outcome = await rewindForTabDetailed\(sourceTabId, turn, "conversation"\)/);
assert.match(appSource, /sendToTab\(targetTabId, next, submit, original\)/);
assert.match(controllerSource, /settleRewindTarget\(result, tab => adoptReturnedTab\(tab, sourceTabId, forkNavigationSeq, "tab\.rewind"\)/);
assert.match(controllerSource, /partialNotice = partialRewindNotice\(result\)/);
assert.match(controllerSource, /dispatchPartialRewindNotice\(partialNotice, sourceTabId, outcome\.tabId,/);
assert.ok(
  controllerSource.indexOf('await loadSessionDataForTab(sourceTabId, true, "rewind")')
    < controllerSource.indexOf("dispatchPartialRewindNotice(partialNotice, sourceTabId, outcome.tabId,"),
  "partial rewind warning must be appended after history reload",
);
assert.equal(partialRewindNotice({ ok: true }), "");
assert.equal(
  partialRewindNotice({ ok: true, partial: true, conflicts: ["a.txt changed", "b.txt missing"] }),
  "The conversation was forked, but code could not be fully restored. a.txt changed; b.txt missing",
);
assert.equal(rewindFailureDetail({ ok: false, conflicts: ["a.txt changed"] }), "a.txt changed");
assert.deepEqual(rewindOutcome({ ok: true, tabId: "fork-tab" }), { ok: true, tabId: "fork-tab" });
const notices: Array<[string, string]> = [];
dispatchPartialRewindNotice("partial", "source", "fork", (tabId, text) => notices.push([tabId, text]));
assert.deepEqual(notices, [["source", "partial"], ["fork", "partial"]]);

console.log("rewind fork routing contract passed");
