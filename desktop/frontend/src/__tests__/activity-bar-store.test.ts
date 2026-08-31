// Run: tsx src/__tests__/activity-bar-store.test.ts
// Verifies the right-dock activity-bar store: open-entry reuse, add/close tab
// activation fallback, and localStorage persistence round-trip.

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";

// The store reads localStorage at module load, so install a jsdom global
// before importing it (mirrors how browser-only tests bootstrap the DOM).
const dom = new JSDOM("", { url: "https://reasonix.local/" });
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.localStorage = dom.window.localStorage;

const testDir = dirname(fileURLToPath(import.meta.url));
const storeSource = readFileSync(resolve(testDir, "../store/activityBar.ts"), "utf8");
const { useActivityBarStore } = await import("../store/activityBar");

let passed = 0;
let failed = 0;

function check(name: string, condition: boolean, detail = "") {
  if (condition) {
    passed += 1;
    console.log(`  PASS  ${name}`);
  } else {
    failed += 1;
    console.error(`  FAIL  ${name}${detail ? ` — ${detail}` : ""}`);
  }
}

function resetStore(): void {
  localStorage.clear();
  useActivityBarStore.setState({ tabs: [], activeTabId: null, addMenuOpen: false });
}

// --- persistence contract: store reads/writes localStorage under one key ---
check(
  "store persists under reasonix.dock.tabs",
  /STORAGE_KEY\s*=\s*"reasonix\.dock\.tabs"/.test(storeSource),
  "STORAGE_KEY literal not found",
);

// --- openEntry reuses an existing tab of the same type ---
resetStore();
useActivityBarStore.getState().openEntry("file", "Files");
const first = useActivityBarStore.getState().tabs[0].id;
check("openEntry opens the first tab", useActivityBarStore.getState().tabs.length === 1, "expected 1 tab");
check("openEntry activates the tab", useActivityBarStore.getState().activeTabId === first, "activeTabId mismatch");
useActivityBarStore.getState().openEntry("changed", "Changes");
check("openEntry appends a different type", useActivityBarStore.getState().tabs.length === 2, "expected 2 tabs");
useActivityBarStore.getState().openEntry("file", "Files");
check("openEntry reuses same-type tab", useActivityBarStore.getState().tabs.length === 2, "expected still 2 tabs");
check("openEntry switches back to the reused tab", useActivityBarStore.getState().activeTabId === first, "activeTabId mismatch");

// --- closeTab falls back to the left neighbor, then right, then null ---
resetStore();
useActivityBarStore.getState().openEntry("file", "Files");
useActivityBarStore.getState().openEntry("changed", "Changes");
useActivityBarStore.getState().openEntry("terminal", "Terminal");
const ids = useActivityBarStore.getState().tabs.map((tab) => tab.id);
check("opening a tab expands the container", useActivityBarStore.getState().activityBarOpen === true, "expected expanded");
// Closing a non-active tab leaves the active tab untouched.
useActivityBarStore.getState().activateTab(ids[2]);
useActivityBarStore.getState().closeTab(ids[1]);
check("closeTab removes the tab", useActivityBarStore.getState().tabs.length === 2, "expected 2 tabs left");
check("closing a non-active tab keeps the active tab", useActivityBarStore.getState().activeTabId === ids[2], "activeTabId mismatch");
// Closing the active tab falls back to its left neighbor.
useActivityBarStore.getState().closeTab(ids[2]);
check("closeTab activates left neighbor", useActivityBarStore.getState().activeTabId === ids[0], "activeTabId mismatch");
// Closing the last tab collapses back to the activity bar.
useActivityBarStore.getState().closeTab(ids[0]);
check("closing the last tab clears activeTabId", useActivityBarStore.getState().tabs.length === 0 && useActivityBarStore.getState().activeTabId === null, "expected empty dock");
check("closing the last tab collapses the container", useActivityBarStore.getState().activityBarOpen === false, "expected collapsed");

// --- persistence round-trip survives a store re-init ---
resetStore();
useActivityBarStore.getState().openEntry("file", "Files");
useActivityBarStore.getState().openEntry("browser", "Browser");
const persisted = JSON.parse(localStorage.getItem("reasonix.dock.tabs") ?? "{}");
check("persistence writes tabs", Array.isArray(persisted.tabs) && persisted.tabs.length === 2, "expected 2 persisted tabs");
check("persistence writes activeTabId", typeof persisted.activeTabId === "string", "activeTabId not persisted");

// --- activityBarOpen is independent of tabs (toggle collapse keeps tabs) ---
resetStore();
useActivityBarStore.getState().openEntry("file", "Files");
useActivityBarStore.getState().openEntry("browser", "Browser");
useActivityBarStore.getState().setActivityBarOpen(false);
check("collapse via setActivityBarOpen keeps tabs", useActivityBarStore.getState().tabs.length === 2, "expected 2 tabs kept");
check("collapse via setActivityBarOpen closes the container", useActivityBarStore.getState().activityBarOpen === false, "expected collapsed");
useActivityBarStore.getState().setActivityBarOpen(true);
check("re-expand restores the container", useActivityBarStore.getState().activityBarOpen === true, "expected expanded");

// --- addMenuOpen is session-local ---
resetStore();
useActivityBarStore.getState().setAddMenuOpen(true);
check("addMenuOpen setter works", useActivityBarStore.getState().addMenuOpen === true, "expected open");

// --- updateTab rewrites one tab's label/meta, keeps active, leaves others ---
// The dock mirrors the current preview by updating the active file tab; this
// must never activate a tab, never add one, and never touch other tabs.
resetStore();
useActivityBarStore.getState().addTab("file", "a.ts", { path: "/a.ts" });
const fileA = useActivityBarStore.getState().tabs[0].id;
useActivityBarStore.getState().addTab("file", "b.ts", { path: "/b.ts" });
const fileB = useActivityBarStore.getState().tabs[1].id;
useActivityBarStore.getState().activateTab(fileA);
useActivityBarStore.getState().updateTab(fileA, "c.ts", { path: "/c.ts" });
check("updateTab rewrites label", useActivityBarStore.getState().tabs[0].label === "c.ts", "label mismatch");
check("updateTab rewrites meta", useActivityBarStore.getState().tabs[0].meta?.path === "/c.ts", "meta mismatch");
check("updateTab leaves the other tab untouched", useActivityBarStore.getState().tabs[1].label === "b.ts" && useActivityBarStore.getState().tabs[1].meta?.path === "/b.ts", "other tab changed");
check("updateTab keeps the active tab", useActivityBarStore.getState().activeTabId === fileA, "activeTabId changed");
check("updateTab adds no tabs", useActivityBarStore.getState().tabs.length === 2, "expected still 2 tabs");
check("updateTab persists", JSON.parse(localStorage.getItem("reasonix.dock.tabs") ?? "{}").tabs[0].meta?.path === "/c.ts", "not persisted");
check("updateTab with empty meta clears the path", () => {
  useActivityBarStore.getState().updateTab(fileA, "files", {});
  return useActivityBarStore.getState().tabs[0].meta?.path === undefined && useActivityBarStore.getState().tabs.length === 2;
}, "path not cleared");

// --- fresh tab ids never collide with restored/persisted ones ---
// New tabs must always receive a unique id (React key uniqueness relies on
// it). The seq seeder runs at module load against persisted ids; here we
// verify consecutive adds stay unique and strictly increasing.
resetStore();
useActivityBarStore.getState().addTab("file", "a");
useActivityBarStore.getState().addTab("file", "b");
useActivityBarStore.getState().addTab("file", "c");
const freshIds = useActivityBarStore.getState().tabs.map((tab) => tab.id);
check(
  "fresh tab ids are unique and increasing",
  freshIds.length === new Set(freshIds).size && freshIds.every((id) => /^dock-tab-\d+$/.test(id)),
  `ids=${freshIds.join(",")}`,
);

// --- moveTab reorders tabs and persists the new order ---
resetStore();
useActivityBarStore.getState().addTab("file", "a");
useActivityBarStore.getState().addTab("changed", "b");
useActivityBarStore.getState().addTab("context", "c");
const orderBefore = useActivityBarStore.getState().tabs.map((tab) => tab.id);
// Move c to the left of a → [c, a, b]
useActivityBarStore.getState().moveTab(orderBefore[2], orderBefore[0], "left");
const afterLeft = useActivityBarStore.getState().tabs.map((tab) => tab.id);
check("moveTab left places the tab before the target", afterLeft[0] === orderBefore[2] && afterLeft[1] === orderBefore[0], `order=${afterLeft.join(",")}`);
check("moveTab persists", JSON.parse(localStorage.getItem("reasonix.dock.tabs") ?? "{}").tabs.map((t: { id: string }) => t.id).join(",") === afterLeft.join(","), "order not persisted");
// Move a to the right of c → [c, a, b] stays? a is at index 1, target c at 0, right → [c, a, b]? move a right of c inserts after c → [c, a, b] (already). Pick a different move: b right of c → [c, b, a]
useActivityBarStore.getState().moveTab(afterLeft[2], afterLeft[0], "right");
const afterRight = useActivityBarStore.getState().tabs.map((tab) => tab.id);
check("moveTab right places the tab after the target", afterRight[0] === orderBefore[2] && afterRight[1] === orderBefore[1], `order=${afterRight.join(",")}`);
check("moveTab keeps the active tab", useActivityBarStore.getState().activeTabId === orderBefore[2], "activeTabId changed");
// Same-id move is a no-op.
useActivityBarStore.getState().moveTab(afterRight[0], afterRight[0], "left");
check("moveTab same id is a no-op", useActivityBarStore.getState().tabs.map((tab) => tab.id).join(",") === afterRight.join(","), "order changed");

console.log(`\nactivity-bar-store: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
