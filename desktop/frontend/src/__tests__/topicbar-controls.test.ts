// Run: tsx src/__tests__/topicbar-controls.test.ts

import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(testDir, "../App.tsx"), "utf8");
const sessionActionsSource = readFileSync(resolve(testDir, "../components/TopicbarSessionActions.tsx"), "utf8");

assert.doesNotMatch(appSource, /t\("shortcuts\.cheatsheetTitle"\)|t\("topicBar\.command"\)/);

const taskSummaryControlIndex = sessionActionsSource.indexOf('t("summary.session")');
const workspaceToggleIndex = appSource.indexOf('<Tooltip label={surfaceWorkspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}>');
assert.ok(taskSummaryControlIndex >= 0, "topic bar renders the localized Session summary control");
assert.ok(workspaceToggleIndex >= 0, "topic bar keeps the right-edge workspace toggle");
assert.match(
  appSource,
  /const localWorkspaceDockBlocked = remoteSurfaceActive && \(rightDockMode === "files" \|\| rightDockMode === "changed"\);/,
  "remote sessions block local Files and Changes surfaces",
);
assert.match(
  appSource,
  /const surfaceWorkspacePanelRenderable = chatSurfaceVisible && workspacePanelRenderable && !localWorkspaceDockBlocked;/,
  "the topic bar projects the workspace toggle through the active surface boundary",
);
assert.ok(!sessionActionsSource.includes('aria-label="Session summary"'), "Session summary does not use a hard-coded English label");

process.stdout.write("topicbar controls: 4 contracts passed\n");
