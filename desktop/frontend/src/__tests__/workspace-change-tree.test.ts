// Run: tsx src/__tests__/workspace-change-tree.test.ts

import { buildWorkspaceChangeTree } from "../lib/workspaceChangeTree";

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

console.log("\nworkspace change tree");

const changes = [
  { path: "src/main.ts", kind: "modified" },
  { path: "README.md", kind: "added" },
  { path: "src/components/App.tsx", kind: "modified" },
  { path: "src/components/panels/Changes.tsx", kind: "added" },
];

const tree = buildWorkspaceChangeTree(changes);

eq(
  tree.map((node) => ({ kind: node.kind, name: node.name, path: node.path })),
  [
    { kind: "folder", name: "src", path: "src/" },
    { kind: "file", name: "README.md", path: "README.md" },
  ],
  "groups changed files under their top-level folder while keeping root files visible",
);

const src = tree[0];
eq(
  src.kind === "folder" ? src.children.map((node) => ({ kind: node.kind, name: node.name, path: node.path })) : [],
  [
    { kind: "folder", name: "components", path: "src/components/" },
    { kind: "file", name: "main.ts", path: "src/main.ts" },
  ],
  "builds nested folder nodes and keeps files attached to their parent",
);

const components = src.kind === "folder" ? src.children[0] : undefined;
eq(
  components?.kind === "folder" ? components.children.map((node) => node.path) : [],
  ["src/components/panels/", "src/components/App.tsx"],
  "preserves deeper nesting beneath a folder node",
);

eq(
  tree.find((node) => node.kind === "file" && node.path === "README.md")?.change,
  changes[1],
  "file nodes retain the original change record for rendering actions and metadata",
);

const duplicatePaths = buildWorkspaceChangeTree([
  { key: "first", path: "src/shared.ts" },
  { key: "second", path: "src/shared.ts" },
]);
const duplicateFiles = duplicatePaths[0]?.kind === "folder" ? duplicatePaths[0].children : [];
eq(
  duplicateFiles.map((node) => node.key),
  ["file:first", "file:second"],
  "scoped change keys keep duplicate paths as distinct file rows",
);

if (failed > 0) {
  process.exitCode = 1;
}
process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
