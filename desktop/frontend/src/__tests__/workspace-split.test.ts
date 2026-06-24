// Run: tsx src/__tests__/workspace-split.test.ts

import {
  initialWorkspaceSplitTreeWidth,
  resolveWorkspaceSplitTreeWidth,
  workspaceSplitCanFit,
  workspaceSplitTreeWidthFromPointer,
} from "../lib/workspaceSplit";
import { resolveWorkspacePanelWidth } from "../lib/workspaceLayout";
import { closeWorkspacePreviewTab } from "../lib/workspacePreviewTabs";

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

console.log("\nworkspace file split");

const TREE_MIN_WIDTH = 140;
const TREE_RAIL_WIDTH = 44;
const PREVIEW_MIN_WIDTH = 140;

eq(
  initialWorkspaceSplitTreeWidth({
    panelWidth: 660,
    railWidth: TREE_RAIL_WIDTH,
    savedTreeWidth: null,
    treeMinWidth: TREE_MIN_WIDTH,
    previewMinWidth: PREVIEW_MIN_WIDTH,
  }),
  308,
  "first split divides the file area evenly after reserving the tree rail",
);

eq(
  initialWorkspaceSplitTreeWidth({
    panelWidth: 660,
    railWidth: TREE_RAIL_WIDTH,
    savedTreeWidth: 620,
    treeMinWidth: TREE_MIN_WIDTH,
    previewMinWidth: PREVIEW_MIN_WIDTH,
  }),
  476,
  "tree width is clamped so the preview keeps its minimum width",
);

eq(
  workspaceSplitTreeWidthFromPointer({
    clientX: 400,
    panelLeft: 100,
    panelWidth: 660,
    railWidth: TREE_RAIL_WIDTH,
    treeMinWidth: TREE_MIN_WIDTH,
    previewMinWidth: PREVIEW_MIN_WIDTH,
  }),
  256,
  "tree resize pointer coordinates start after the tree rail",
);

eq(
  workspaceSplitTreeWidthFromPointer({
    clientX: 700,
    panelLeft: 100,
    panelWidth: 660,
    railWidth: TREE_RAIL_WIDTH,
    treeMinWidth: TREE_MIN_WIDTH,
    previewMinWidth: PREVIEW_MIN_WIDTH,
  }),
  476,
  "tree resize pointer clamps against the preview minimum after reserving the rail",
);

eq(
  resolveWorkspaceSplitTreeWidth({
    mode: "even",
    currentTreeWidth: 140,
    panelWidth: 660,
    railWidth: TREE_RAIL_WIDTH,
    treeMinWidth: TREE_MIN_WIDTH,
    previewMinWidth: PREVIEW_MIN_WIDTH,
  }),
  308,
  "default split recomputes evenly after the parent preview width applies",
);

eq(
  resolveWorkspaceSplitTreeWidth({
    mode: "manual",
    currentTreeWidth: 256,
    panelWidth: 660,
    railWidth: TREE_RAIL_WIDTH,
    treeMinWidth: TREE_MIN_WIDTH,
    previewMinWidth: PREVIEW_MIN_WIDTH,
  }),
  256,
  "manual split width is preserved when the parent width changes",
);

eq(
  resolveWorkspacePanelWidth({
    open: true,
    maximized: false,
    preferredWidth: 660,
    minWidth: 300,
    availableWidth: 228,
  }),
  228,
  "outer file area can still shrink below split target width",
);

eq(
  workspaceSplitCanFit({
    panelWidth: 323,
    railWidth: TREE_RAIL_WIDTH,
    treeMinWidth: TREE_MIN_WIDTH,
    previewMinWidth: PREVIEW_MIN_WIDTH,
  }),
  false,
  "file tree collapses before rail tree and preview would overflow a narrow panel",
);

eq(
  workspaceSplitCanFit({
    panelWidth: 324,
    railWidth: TREE_RAIL_WIDTH,
    treeMinWidth: TREE_MIN_WIDTH,
    previewMinWidth: PREVIEW_MIN_WIDTH,
  }),
  true,
  "file tree remains available at the exact split minimum width",
);

eq(
  workspaceSplitCanFit({
    panelWidth: undefined,
    railWidth: TREE_RAIL_WIDTH,
    treeMinWidth: TREE_MIN_WIDTH,
    previewMinWidth: PREVIEW_MIN_WIDTH,
  }),
  true,
  "file tree stays visible until the measured panel width is known",
);

const closedRecentPreview = closeWorkspacePreviewTab(["a.ts", "b.ts", "c.ts"], "c.ts");
eq(closedRecentPreview.selectedPath, "b.ts", "closing the current preview falls back to the previous recent file");
eq(closedRecentPreview.openTabs.join(","), "a.ts,b.ts", "closing one preview preserves the other recent files");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
