// Run: tsx src/__tests__/workspace-tree-memory.test.ts

import { JSDOM } from "jsdom";
import {
  readWorkspaceTreeMemory,
  rememberWorkspaceTreeState,
  resetWorkspaceTreeMemoryForTests,
} from "../lib/workspaceTreeMemory";

let passed = 0;
function ok(value: boolean, label: string): void {
  if (!value) throw new Error(label);
  passed += 1;
  process.stdout.write(`  PASS  ${label}\n`);
}

const dom = new JSDOM("<!doctype html>", { url: "http://localhost/" });
globalThis.localStorage = dom.window.localStorage;

console.log("\nversioned per-project workspace memory");
resetWorkspaceTreeMemoryForTests();

rememberWorkspaceTreeState("project-a", {
  openDirs: new Set(["", "src/"]),
  selectedFilePath: "src/App.tsx",
  selectedChangePath: "src/store.ts",
  treeWidth: 276,
  treeWidthMode: "even",
  scrollTop: 144,
  dockTreeWidth: 320,
  dockPreviewWidth: 640,
});
rememberWorkspaceTreeState("project-b", { selectedFilePath: "README.md", treeWidth: 220 });

const projectA = readWorkspaceTreeMemory("project-a");
const projectB = readWorkspaceTreeMemory("project-b");
ok(projectA?.selectedFilePath === "src/App.tsx", "restores the file selection independently");
ok(projectA?.selectedChangePath === "src/store.ts", "restores the change selection independently");
ok(projectA?.openDirs.has("src/") === true && projectA.scrollTop === 144, "restores expanded directories and tree scroll");
ok(projectA?.dockTreeWidth === 320 && projectA.dockPreviewWidth === 640, "restores both outer dock widths");
ok(projectB?.selectedFilePath === "README.md" && projectB.treeWidth === 220, "keeps project state isolated by key");

const persisted = JSON.parse(localStorage.getItem("reasonix.workspaceState.v2") ?? "null") as { version?: number } | null;
ok(persisted?.version === 2, "writes an explicit schema version");

resetWorkspaceTreeMemoryForTests();
localStorage.setItem("reasonix.workspaceState.v2", JSON.stringify({ version: 99, projects: [{ key: "future", state: {} }] }));
ok(readWorkspaceTreeMemory("future") === null, "safely ignores storage from an unsupported future schema");

resetWorkspaceTreeMemoryForTests();
localStorage.setItem("reasonix.workspaceState.v2", "{not-json");
ok(readWorkspaceTreeMemory("broken") === null, "a corrupt cache cannot prevent workspace startup");

dom.window.close();
console.log(`\n${passed} passed, 0 failed, ${passed} total`);
