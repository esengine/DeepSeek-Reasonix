// Run: tsx src/__tests__/workspace-tree-tab-ready.test.tsx

import { act } from "react";
import { flushPromises, renderFilesWorkspace, waitFor } from "./workspace-panel-test-harness";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\nworkspace tree tab-ready reload");

// A cold tab (tabReady=false) returns an empty dir list from the backend even
// though the workspace is not empty. The tree must not render a blank panel:
// it shows a loading placeholder until the tab reports ready, then reloads and
// populates the file list.
// The first root listing (while cold) is empty; every later root listing (after
// readiness) returns the real entry.
const tabReadySequence: (object[] | undefined)[] = [[], undefined];
const { dom, root, rerender } = await renderFilesWorkspace({
  ListDirForTab: async (_tabId, dir) => {
    if (dir !== "") return [];
    const next = tabReadySequence.shift();
    if (next !== undefined) return next;
    return [{ name: "notes.txt", isDir: false }];
  },
  ReadFileForTab: async (_tabId, path) => ({ path, body: "file preview", size: 12, truncated: false, binary: false }),
}, { tabReady: false });

await waitFor("loading placeholder while tab not ready", () =>
  document.querySelector(".workspace-tree .workspace-empty") !== null,
);
ok(document.body.textContent?.includes("Loading file") === true, "shows a loading placeholder instead of a blank panel while the tab is not ready");
ok(document.body.textContent?.includes("notes.txt") === false, "does not show the file before the tab is ready");

// The tab finishes booting: readiness flips to true, which must reload the tree.
await rerender({ tabReady: true });
await waitFor("file appears after tab ready", () => document.body.textContent?.includes("notes.txt") === true);
ok(document.body.textContent?.includes("notes.txt") === true, "reloads the file tree once the tab reports ready");
ok(document.querySelector(".workspace-tree .workspace-empty") === null, "clears the loading placeholder once files are present");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
