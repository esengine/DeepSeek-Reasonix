// Run: tsx src/__tests__/desktop-creation-style.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(testDir, "../App.tsx"), "utf8");
const settingsSource = readFileSync(resolve(testDir, "../components/SettingsPanel.tsx"), "utf8");
const contextPanelSource = readFileSync(resolve(testDir, "../components/ContextPanel.tsx"), "utf8");
const stylesSource = readFileSync(resolve(testDir, "../styles.css"), "utf8");

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\ndesktop creation style");

ok(
  /type DesktopLayoutStyle = "classic" \| "workbench" \| "creation";/.test(appSource),
  "App accepts creation as a desktop layout style",
);

ok(
  /\(\["classic", "workbench", "creation"\] as const\)/.test(settingsSource),
  "Settings exposes Classic, Workbench, and Creation",
);

ok(
  /sidebarCreation = desktopLayoutStyle === "creation"/.test(appSource) &&
    /sidebarCreation \? "app--creation"/.test(appSource) &&
    /sidebarCreation \? "layout--creation"/.test(appSource),
  "App scopes Creation with dedicated layout classes",
);

ok(
  /sidebarWorkbenchLike = sidebarWorkbench \|\| sidebarCreation/.test(appSource) &&
    /sidebarWorkbenchLike \? "sidebar--workbench"/.test(appSource) &&
    /projectTreeVariant = sidebarWorkbenchLike \? "workbench" : "classic"/.test(appSource),
  "Creation inherits Workbench sidebar density",
);

ok(
  /<AppChrome[\s\S]*workbenchChrome=\{sidebarWorkbench\}/.test(appSource),
  "Creation keeps AppChrome tab strip mounted instead of using workbench chrome",
);

ok(
  /variant=\{sidebarCreation \? "creation" : "default"\}/.test(appSource) &&
    /context-panel--creation/.test(contextPanelSource),
  "Creation uses a scoped ContextPanel variant",
);

ok(
  /\.app--creation/.test(stylesSource) &&
    /\.layout--creation/.test(stylesSource) &&
    /--sidebar-workbench-active: color-mix\(in srgb, var\(--creation-accent\)/.test(stylesSource) &&
    /\.context-panel--creation \.context-panel__usage-bar/.test(stylesSource),
  "Creation styles are scoped and include the sidebar density tokens",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
