// Run: tsx src/__tests__/workspace-selection-isolation.test.tsx

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

console.log("\nworkspace selection isolation");

const changeDetailPaths: string[] = [];
const { dom, root, rerender } = await renderFilesWorkspace({
  ListDirForTab: async (_tabId, dir) => dir === "" ? [{ name: "notes.txt", isDir: false }] : [],
  WorkspaceChanges: async () => ({
    files: [{ path: "src/changed.ts", sources: ["git"], gitStatus: "M" }],
    gitAvailable: true,
  }),
  WorkspaceChangeDetail: async (_tabId, path) => {
    changeDetailPaths.push(path);
    return { source: "git", diff: "changed diff" };
  },
  ReadFileForTab: async (_tabId, path) => ({ path, body: "file preview", size: 12, truncated: false, binary: false }),
});

await waitFor("files view entry", () => document.body.textContent?.includes("notes.txt") === true);
await act(async () => {
  document.querySelector<HTMLButtonElement>('[data-workspace-path="notes.txt"]')
    ?.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
  await flushPromises();
});
await waitFor("selected file preview", () => document.body.textContent?.includes("file preview") === true);

await rerender({ initialViewMode: "changed" });
await waitFor("changes overview", () => document.body.textContent?.includes("changed.ts") === true);
ok(changeDetailPaths.length === 0, "entering Changes does not request diff detail for the Files selection");
ok(document.querySelector(".workspace-panel--changed-overview") !== null, "entering Changes opens its overview instead of leaking the Files selection");

await act(async () => {
  document.querySelector<HTMLButtonElement>(".workspace-change")
    ?.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
  await flushPromises();
});
await waitFor("selected change detail", () => changeDetailPaths.includes("src/changed.ts"));

await rerender({ initialViewMode: "files" });
await waitFor("restored file preview", () => document.body.textContent?.includes("file preview") === true);
ok(changeDetailPaths.every((path) => path === "src/changed.ts"), "Changes requests only its own selected path");
ok(document.body.textContent?.includes("notes.txt") === true, "returning to Files restores the prior file selection after viewing a change");

await rerender({
  initialViewMode: "changed",
  changeListRequest: {
    id: 1,
    changes: [{ key: "scoped-change", path: "src/changed.ts", meta: "M", time: "", detail: "changed" }],
  },
});
await waitFor("scoped Changes list", () => document.body.textContent?.includes("Session changes") === true);
await rerender({ initialViewMode: "files", changeListRequest: null });
await waitFor("file preview after scoped Changes", () => document.body.textContent?.includes("file preview") === true);
ok(document.body.textContent?.includes("notes.txt") === true, "scoped Changes requests preserve the Files preview state");

await rerender({ initialViewMode: "changed" });
await waitFor("change before scope switch", () => document.body.textContent?.includes("changed.ts") === true);
await act(async () => {
  document.querySelector<HTMLButtonElement>(".workspace-change")
    ?.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
  await flushPromises();
});
await waitFor("change selected before scope switch", () => changeDetailPaths.includes("src/changed.ts"));
const callsBeforeScopeSwitch = changeDetailPaths.length;
await rerender({ workspaceScopeKey: "other-session" });
await waitFor("new scope Changes overview", () => document.querySelector(".workspace-panel--changed-overview") !== null);
ok(changeDetailPaths.length === callsBeforeScopeSwitch, "workspace scope changes do not request the previous scope's selected change");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
