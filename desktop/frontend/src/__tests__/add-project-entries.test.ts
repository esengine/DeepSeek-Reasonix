// Run: tsx src/__tests__/add-project-entries.test.ts
// Source-contract test: the add-project entry points must offer the remote
// connection action next to the local one, and the removed sidebar quick
// button must stay gone.

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

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

console.log("\nAdd-project entry matrix");
const here = dirname(fileURLToPath(import.meta.url));
const treeSource = readFileSync(resolve(here, "../components/ProjectTree.tsx"), "utf8");
const addControlsSource = readFileSync(resolve(here, "../components/ProjectTreeAddControls.tsx"), "utf8");
const hookSource = readFileSync(resolve(here, "../components/useProjectCreation.tsx"), "utf8");
const appSource = readFileSync(resolve(here, "../App.tsx"), "utf8");
const locales = ["en", "zh", "zh-TW"].map((name) =>
  readFileSync(resolve(here, `../locales/${name}.ts`), "utf8"),
);

// Workbench "+" menu: three items, remote opens the wizard flow.
ok(
  /onRemote: \(\) => \{ closeMenu\(\); openRemoteConnectFlow\(\); \}/.test(treeSource) &&
    /key: "remote-connection"[\s\S]*?onSelect: onRemote/.test(addControlsSource),
  "workbench + menu offers the remote connection item wired to the wizard flow",
);
ok(
  /key: "blank-project"[\s\S]*?key: "open-local-folder"[\s\S]*?key: "remote-connection"/.test(addControlsSource),
  "workbench + menu keeps blank project and existing folder before remote connection",
);

// Classic/creation "+": no longer a direct picker; renders a menu that
// contains the remote connection item.
ok(
  /project-tree__add-project[\s\S]*?aria-haspopup="menu"/.test(addControlsSource),
  "classic/creation + button opens a menu instead of the direct picker",
);
ok(
  /ProjectTreeHeaderAddControl[\s\S]*?items=\{classicHeaderAddItems\}/.test(treeSource),
  "classic/creation + menu is the two-item local/remote list",
);

// Empty state: primary local + secondary remote.
ok(
  /project-tree__empty-primary[\s\S]*?ProjectTreeRemoteAction[\s\S]*?openRemoteConnectFlow/.test(treeSource) &&
    /project-tree__empty-secondary/.test(addControlsSource),
  "empty state pairs the local action with a remote connection action",
);

// The wizard mounts through the creation hook, outside the startup bundle.
ok(
  /lazy\(\(\) => import\("\.\/RemoteConnectWizardEntry"\)/.test(hookSource),
  "remote wizard loads lazily through useProjectCreation",
);

// Removed sidebar quick button: gone from App.tsx and no locale key remains.
ok(!/sidebar__new--remote/.test(appSource), "removed sidebar quick button leaves no JSX behind");
ok(!/creation\.sidebar\.remote/.test(appSource), "no creation.sidebar.remote usage in App.tsx");
for (const [index, source] of locales.entries()) {
  ok(!/creation\.sidebar\.remote/.test(source), `creation.sidebar.remote removed from locale #${index + 1}`);
  ok(/"projectTree\.remoteConnection"/.test(source), `projectTree.remoteConnection present in locale #${index + 1}`);
}

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
