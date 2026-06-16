// Run: tsx src/__tests__/desktop-creation-style.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(testDir, "../App.tsx"), "utf8");
const composerSource = readFileSync(resolve(testDir, "../components/Composer.tsx"), "utf8");
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
  /sidebarWorkbenchLike = sidebarWorkbench;/.test(appSource) &&
    /sidebarWorkbenchLike \? "sidebar--workbench"/.test(appSource) &&
    /projectTreeVariant = sidebarWorkbench \? "workbench" : "classic"/.test(appSource),
  "Creation does not inherit Workbench sidebar density",
);

ok(
  /sidebarCreationFeaturesBlock/.test(appSource) &&
    /sidebar__features sidebar__features--creation/.test(appSource) &&
    /\{sidebarCreation \? sidebarCreationFeaturesBlock : null\}/.test(appSource) &&
    /className="sidebar__feature"/.test(appSource) &&
    /className="sidebar__utility"/.test(appSource) &&
    /className="sidebar__utility-btn"/.test(appSource),
  "Creation renders a dedicated sidebar feature area before the project tree",
);

ok(
  /<AppChrome[\s\S]*workbenchChrome=\{sidebarWorkbench \|\| sidebarCreation\}/.test(appSource) &&
    /\.app--creation \.app-chrome__panel-toggle--left/.test(stylesSource),
  "Creation skips the classic AppChrome tab strip",
);

ok(
  /<Composer[\s\S]*variant=\{sidebarCreation \? "creation" : "default"\}/.test(appSource) &&
    /creationVariant = variant === "creation"/.test(composerSource) &&
    /composer__input-row/.test(composerSource) &&
    /composer-meta__group composer-meta__group--left/.test(composerSource) &&
    /composer-card--running/.test(composerSource),
  "Creation uses the original composer structure behind a variant",
);

ok(
  /variant=\{sidebarCreation \? "creation" : "default"\}/.test(appSource) &&
    /context-panel--creation/.test(contextPanelSource),
  "Creation uses a scoped ContextPanel variant",
);

ok(
  /\.app--creation/.test(stylesSource) &&
    /\.layout--creation/.test(stylesSource) &&
    /--creation-accent: var\(--accent\)/.test(stylesSource) &&
    /--sidebar-rail-card: var\(--side-panel-card\)/.test(stylesSource) &&
    /\.sidebar--creation \.sidebar__brand-search/.test(stylesSource) &&
    /\.sidebar--creation \.sidebar__new/.test(stylesSource) &&
    /\.sidebar--creation \.sidebar__utility-btn/.test(stylesSource) &&
    /\.layout--creation \.sidebar-resizer-shell/.test(stylesSource) &&
    /\.context-panel--creation \.context-panel__usage-bar/.test(stylesSource),
  "Creation styles are scoped and include the original sidebar rail tokens",
);

ok(
  /\.app--creation \.composer-card::before/.test(stylesSource) &&
    /--composer-glow: var\(--creation-glow\)/.test(stylesSource) &&
    /:root\[data-theme-style\] \.app--creation \.composer-card/.test(stylesSource) &&
    /\.app--creation \.composer__btn--send svg/.test(stylesSource) &&
    /\.app--creation \.composer__btn--send:disabled[\s\S]*color: #ffffff;/.test(stylesSource) &&
    /\.app--creation \.composer-meta \.modelsw__kind,\n\.app--creation \.composer-modebar__item span[\s\S]*display: none;/.test(stylesSource),
  "Creation keeps the original composer glow and white send icon contract",
);

ok(
  /\.app--creation \.msg--user:not\(\.msg--im-source\) \.msg__body/.test(stylesSource) &&
    /:root\[data-theme-style\] \.app--creation \.msg--user:not\(\.msg--im-source\) \.msg__body/.test(stylesSource) &&
    /\.app--creation \.process-card,/.test(stylesSource) &&
    /:root\[data-theme-style\] \.app--creation \.process-card,/.test(stylesSource) &&
    /\.app--creation \.prompt-shelf__bar/.test(stylesSource) &&
    /\.app--creation \.approval-subject/.test(stylesSource),
  "Creation scopes message bubbles and simplified process/approval cards",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
