// Run: tsx src/__tests__/theme-auto-background.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const themeSource = readFileSync(resolve(testDir, "../lib/theme.ts"), "utf8");
const stylesSource = readFileSync(resolve(testDir, "../styles.css"), "utf8");

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

function blockAfter(source: string, needle: string, after = 0): string {
  const selectorIndex = source.indexOf(needle, after);
  if (selectorIndex < 0) return "";
  const open = source.indexOf("{", selectorIndex);
  if (open < 0) return "";

  let depth = 0;
  for (let index = open; index < source.length; index += 1) {
    const char = source[index];
    if (char === "{") depth += 1;
    else if (char === "}") {
      depth -= 1;
      if (depth === 0) return source.slice(open + 1, index);
    }
  }
  return "";
}

console.log("\ntheme auto native background contract");

ok(
  themeSource.includes('const AUTO_THEME_MEDIA_QUERY = "(prefers-color-scheme: light)";'),
  "auto theme uses one shared light color-scheme media query",
);
ok(
  themeSource.includes("window.matchMedia?.(AUTO_THEME_MEDIA_QUERY).matches"),
  "resolved auto theme reads the shared color-scheme query",
);
ok(
  themeSource.includes("syncAutoThemeBackgroundListener(theme);"),
  "applyTheme updates the native auto-theme listener",
);
ok(
  themeSource.includes('autoThemeMediaQuery.addEventListener("change", syncAutoThemeBackground)'),
  "auto mode listens for color-scheme changes",
);
ok(
  themeSource.includes('autoThemeMediaQuery.removeEventListener("change", syncAutoThemeBackground)'),
  "explicit modes remove the color-scheme listener",
);
ok(
  themeSource.includes('if (theme !== "auto")') && themeSource.includes("clearAutoThemeBackgroundListener();"),
  "non-auto themes clear the auto listener",
);
ok(
  themeSource.includes('if (currentTheme === "auto"') && themeSource.includes('syncNativeWindowBackground("auto")'),
  "color-scheme changes only sync native background while auto is active",
);
ok(
  themeSource.includes("if (autoThemeMediaQuery || typeof window"),
  "reapplying auto does not register duplicate listeners",
);

const workbenchRefreshIndex = stylesSource.indexOf("Native Workbench refresh");
const workbenchSource = workbenchRefreshIndex >= 0 ? stylesSource.slice(workbenchRefreshIndex) : "";
const workbenchLightBlock = blockAfter(workbenchSource, ':root[data-theme="light"]');
const workbenchAutoLightMediaIndex = workbenchSource.indexOf("@media (prefers-color-scheme: light)");
const workbenchAutoLightBlock = blockAfter(workbenchSource, ":root:not([data-theme])", workbenchAutoLightMediaIndex);

ok(workbenchRefreshIndex >= 0, "styles include the native workbench theme refresh section");
ok(workbenchAutoLightMediaIndex >= 0, "workbench refresh has an auto-mode light media override");
ok(
  workbenchLightBlock.includes("--bg: #e9edf3;") && workbenchAutoLightBlock.includes("--bg: #e9edf3;"),
  "auto light mode keeps the workbench light background after late root overrides",
);
ok(
  workbenchLightBlock.includes("--fg: #121722;") && workbenchAutoLightBlock.includes("--fg: #121722;"),
  "auto light mode keeps the workbench light foreground after late root overrides",
);
ok(
  workbenchLightBlock.includes("--workspace-files-bg: #f1f4f8;") &&
    workbenchAutoLightBlock.includes("--workspace-files-bg: #f1f4f8;"),
  "auto light mode keeps workbench panel surfaces aligned with forced light mode",
);

const creationAssistantLightRule = ':root[data-theme="light"] .app--creation .msg--assistant .msg__body';
const creationAssistantAutoLightRule = ':root:not([data-theme]) .app--creation .msg--assistant .msg__body';
const creationAssistantLightIndex = stylesSource.indexOf(creationAssistantLightRule);
const creationAssistantAutoLightIndex = stylesSource.indexOf(creationAssistantAutoLightRule);
const creationAssistantLightBlock = blockAfter(stylesSource, creationAssistantLightRule);
const creationAssistantAutoLightBlock = blockAfter(stylesSource, creationAssistantAutoLightRule);

ok(creationAssistantLightIndex >= 0, "creation assistant text has an explicit light-mode color adjustment");
ok(
  creationAssistantAutoLightIndex > creationAssistantLightIndex,
  "creation assistant text mirrors the light-mode color adjustment for auto light mode",
);
ok(
  creationAssistantLightBlock.includes("color-mix(in srgb, var(--fg) 90%, var(--bg))") &&
    creationAssistantAutoLightBlock.includes("color-mix(in srgb, var(--fg) 90%, var(--bg))"),
  "creation assistant auto light text color matches forced light mode",
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
