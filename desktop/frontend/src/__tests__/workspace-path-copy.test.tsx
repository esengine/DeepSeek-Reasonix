// Run: tsx src/__tests__/workspace-path-copy.test.tsx

import { workspacePathCopyMenuItems } from "../lib/workspacePathCopyMenuItems";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  process.stdout.write(`  ${value ? "PASS" : "FAIL"}  ${label}\n`);
  if (value) passed += 1;
  else failed += 1;
}

const clipboardWrites: string[] = [];
Object.defineProperty(globalThis, "navigator", {
  configurable: true,
  value: {
    clipboard: {
      writeText: async (value: string) => {
        clipboardWrites.push(value);
      },
    },
  },
});

let closeCalls = 0;
let resolveCalls = 0;
let scopeCurrent = true;
const items = workspacePathCopyMenuItems({
  path: "src/",
  resolveAbsolutePath: async () => {
    resolveCalls += 1;
    return "/repo/src";
  },
  isScopeCurrent: () => scopeCurrent,
  close: () => {
    closeCalls += 1;
  },
  relativeLabel: "Copy relative path",
  absoluteLabel: "Copy absolute path",
});

console.log("\nworkspace path copy menu");
ok(items.length === 2, "builds both path-copy actions");
ok(items[0]?.label === "Copy relative path", "exposes the localized relative-path label");
ok(items[1]?.label === "Copy absolute path", "exposes the localized absolute-path label");

items[0]?.onSelect();
await new Promise((resolve) => setTimeout(resolve, 0));
ok(clipboardWrites[0] === "src", "folder relative paths omit the tree-only trailing slash");
ok(closeCalls === 1, "relative-path selection closes the menu");

items[1]?.onSelect();
await new Promise((resolve) => setTimeout(resolve, 0));
ok(resolveCalls === 1, "absolute-path selection uses backend resolution");
ok(clipboardWrites[1] === "/repo/src", "copies the backend-resolved absolute path");

scopeCurrent = false;
items[1]?.onSelect();
await new Promise((resolve) => setTimeout(resolve, 0));
ok(resolveCalls === 2 && clipboardWrites.length === 2, "drops stale absolute-path results after the workspace scope changes");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
