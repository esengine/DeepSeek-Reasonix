import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const panel = readFileSync(resolve(testDir, "../components/WorkspacePanel.tsx"), "utf8");
const stabilityCSS = readFileSync(resolve(testDir, "../components/WorkspacePanelStability.css"), "utf8");
const workspaceChangesResource = readFileSync(resolve(testDir, "../lib/useWorkspaceChangesResource.ts"), "utf8");

assert.match(
  workspaceChangesResource,
  /emptyKeyedResource<WorkspaceChangesView>/,
  "working-tree changes use the same keyed stale-while-revalidate resource as preview and history",
);
assert.match(
  workspaceChangesResource,
  /setResource\(\(current\) => rejectKeyedResourceRequest/,
  "a failed working-tree refresh preserves the last painted data",
);
assert.doesNotMatch(
  workspaceChangesResource,
  /setResource\(\{\s*files:\s*\[\]/,
  "a failed working-tree refresh never clears the visible list",
);
assert.match(
  panel,
  /loadingHistory\s*&&\s*gitHistory\.length\s*===\s*0/,
  "history uses a loading placeholder only when there is no stale data to paint",
);
assert.doesNotMatch(
  panel,
  /\{loadingHistory\s*\?\s*\(/,
  "history refresh does not replace an already-painted list with a loading branch",
);

const cwdReset = panel.match(/useEffect\(\(\) => \{[\s\S]*?\}, \[cwd, loadDir, open\]\);/)?.[0] ?? "";
assert.ok(cwdReset, "workspace cwd reset effect is present");
assert.doesNotMatch(cwdReset, /setSelected(?:File|Change)Path\(null\)/, "cwd reset does not erase restored per-project selections");

const scopeReset = panel.match(/useEffect\(\(\) => \{[\s\S]*?\}, \[open, resetWorkspaceChanges, viewMode, workspaceScopeKey\]\);/)?.[0] ?? "";
assert.ok(scopeReset, "workspace scope reset effect is present");
assert.doesNotMatch(scopeReset, /setSelectedChangePath\(null\)/, "scope reset does not erase the restored change selection");

assert.match(
  stabilityCSS,
  /\.workspace-resource-status\s*\{[\s\S]*?position:\s*absolute;/,
  "refresh and error badges overlay stale content without changing its layout",
);

console.log("  PASS  workspace panel SWR and per-project restoration contract");
