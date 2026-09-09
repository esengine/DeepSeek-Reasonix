// Run: tsx src/__tests__/topicbar-controls.test.ts

import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(testDir, "../AppRuntime.tsx"), "utf8");
const dockToggleSource = readFileSync(resolve(testDir, "../app-shell/DockToggleButton.tsx"), "utf8");
const sessionActionsSource = readFileSync(resolve(testDir, "../components/TopicbarSessionActions.tsx"), "utf8");

assert.doesNotMatch(appSource, /t\("shortcuts\.cheatsheetTitle"\)|t\("topicBar\.command"\)/);

const taskSummaryControlIndex = sessionActionsSource.indexOf('t("summary.session")');
const workspaceToggleIndex = dockToggleSource.indexOf('<Tooltip label={renderable ? t("rightDock.collapse") : t("rightDock.expand")}>');
assert.ok(taskSummaryControlIndex >= 0, "topic bar renders the localized Session summary control");
assert.ok(workspaceToggleIndex >= 0, "topic bar keeps the right-edge workspace toggle");
// Remote/local surface policy is exercised by conversation-projection.test.ts
// against the production projection and mounted WorkspaceDockRegion.
assert.ok(!sessionActionsSource.includes('aria-label="Session summary"'), "Session summary does not use a hard-coded English label");

process.stdout.write("topicbar static presentation contracts passed\n");
