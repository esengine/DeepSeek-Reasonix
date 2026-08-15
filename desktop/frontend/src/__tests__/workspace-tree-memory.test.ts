// Run: tsx src/__tests__/workspace-tree-memory.test.ts

import { JSDOM } from "jsdom";
import {
  flushWorkspaceTreeMemory,
  readWorkspaceTreeMemory,
  rememberWorkspaceTreeScroll,
  rememberWorkspaceTreeState,
  resetWorkspaceTreeMemoryForTests,
} from "../lib/workspaceTreeMemory";
import {
  createWorkspaceTreePersistenceScheduler,
  type WorkspaceTreePersistenceClock,
} from "../lib/workspaceTreePersistence";

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

let synchronousWrites = 0;
const originalStorage = globalThis.localStorage;
const countingStorage: Storage = {
  get length() { return originalStorage.length; },
  clear: () => originalStorage.clear(),
  getItem: (key) => originalStorage.getItem(key),
  key: (index) => originalStorage.key(index),
  removeItem: (key) => originalStorage.removeItem(key),
  setItem: (key, value) => {
    synchronousWrites += 1;
    originalStorage.setItem(key, value);
  },
};
globalThis.localStorage = countingStorage;
for (let index = 0; index < 120; index += 1) rememberWorkspaceTreeScroll("project-a", 200 + index);
ok(synchronousWrites === 0, "keeps high-frequency scroll updates off the synchronous storage path");
ok(readWorkspaceTreeMemory("project-a")?.scrollTop === 319, "updates the in-memory scroll position immediately");
flushWorkspaceTreeMemory();
ok(synchronousWrites === 1, "coalesces 120 scroll updates into one durable write");
globalThis.localStorage = originalStorage;

let nextHandle = 1;
const frames = new Map<number, () => void>();
const timers = new Map<number, () => void>();
const fakeClock: WorkspaceTreePersistenceClock = {
  requestFrame(callback) {
    const handle = nextHandle++;
    frames.set(handle, callback);
    return handle;
  },
  cancelFrame(handle) {
    frames.delete(handle);
  },
  setTimer(callback) {
    const handle = nextHandle++;
    timers.set(handle, callback);
    return handle as unknown as ReturnType<typeof setTimeout>;
  },
  clearTimer(handle) {
    timers.delete(handle as unknown as number);
  },
};
const persistedKeys: string[] = [];
const scheduler = createWorkspaceTreePersistenceScheduler((key) => persistedKeys.push(key), 200, fakeClock);
for (let index = 0; index < 120; index += 1) scheduler.schedule("project-a");
ok(frames.size === 1 && timers.size === 0, "allows at most one persistence scheduling frame");
for (const callback of Array.from(frames.values())) callback();
frames.clear();
ok(timers.size === 1 && persistedKeys.length === 0, "waits for the quiet-period timer after the frame");
for (const callback of Array.from(timers.values())) callback();
timers.clear();
ok(persistedKeys.join(",") === "project-a", "persists the final project once after the quiet period");

scheduler.schedule("project-b");
scheduler.flush();
ok(
  frames.size === 0 && persistedKeys[persistedKeys.length - 1] === "project-b",
  "flushes pending state before a scope or page exit",
);

resetWorkspaceTreeMemoryForTests();
localStorage.setItem("reasonix.workspaceState.v2", JSON.stringify({ version: 99, projects: [{ key: "future", state: {} }] }));
ok(readWorkspaceTreeMemory("future") === null, "safely ignores storage from an unsupported future schema");

resetWorkspaceTreeMemoryForTests();
localStorage.setItem("reasonix.workspaceState.v2", "{not-json");
ok(readWorkspaceTreeMemory("broken") === null, "a corrupt cache cannot prevent workspace startup");

dom.window.close();
console.log(`\n${passed} passed, 0 failed, ${passed} total`);
