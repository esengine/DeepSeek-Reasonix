// Run: tsx src/__tests__/app-chrome-tabs.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(testDir, "../App.tsx"), "utf8");
const appChromeSource = readFileSync(resolve(testDir, "../components/AppChrome.tsx"), "utf8");
const stylesSource = readFileSync(resolve(testDir, "../styles.css"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");

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

function matchingBlocks(selector: string): string[] {
  const blocks: string[] = [];
  const rule = /([^{}]+)\{([^{}]*)\}/g;
  let match: RegExpExecArray | null;
  while ((match = rule.exec(stylesSource)) !== null) {
    const selectors = match[1].split(",").map((part) => part.trim());
    if (selectors.includes(selector)) blocks.push(match[2]);
  }
  return blocks;
}

function finalDeclaration(selector: string, property: string): string | undefined {
  let value: string | undefined;
  for (const block of matchingBlocks(selector)) {
    const declaration = new RegExp(`(?:^|;)\\s*${property}\\s*:\\s*([^;]+)`, "g");
    let match: RegExpExecArray | null;
    while ((match = declaration.exec(block)) !== null) {
      value = match[1].trim();
    }
  }
  return value;
}

console.log("\napp chrome tabs");

ok(
  /import \{ TabBar \} from "\.\/TabBar";/.test(appChromeSource),
  "AppChrome keeps the classic top session tab strip implementation",
);

for (const propName of ["onTabChange", "onTabClose", "onTabsClose", "onTabsReorder", "onNewTab"]) {
  ok(
    new RegExp(`\\b${propName}\\b`).test(appChromeSource),
    `AppChrome exposes ${propName} for classic tabs`,
  );
}

ok(
  /app-chrome__tab-strip/.test(appChromeSource),
  "AppChrome markup includes classic tab strip containers",
);

ok(
  finalDeclaration(".app-chrome__tab-strip", "overflow") === "hidden",
  "AppChrome tab strip clips tabs to the available chrome width",
);

ok(
  finalDeclaration(".app-chrome__tab-strip", "min-width") === "0",
  "AppChrome tab strip can shrink beside the right dock",
);

ok(
  finalDeclaration(":root[data-theme-style] .app-chrome--tabs .tabbar__tabs", "max-width")?.includes("--chrome-panel-control-size"),
  "themed AppChrome tab lists reserve a flowing new-tab button slot",
);

ok(
  finalDeclaration(":root[data-theme-style] .app-chrome--tabs .tabbar__tabs", "flex") === "0 1 auto",
  "themed AppChrome tab lists size to tab content before shrinking",
);

ok(
  finalDeclaration(":root[data-theme-style] .app-chrome--tabs .tabbar__tabs", "width") === "max-content",
  "themed AppChrome tab lists keep the new-tab button next to the last tab",
);

ok(
  finalDeclaration(":root[data-theme-style] .app-chrome--tabs .tabbar > .tooltip-trigger:has(.tabbar__new)", "flex")?.includes("--chrome-panel-control-size"),
  "themed AppChrome new-tab button keeps a stable slot beside the tabs",
);

ok(
  /workbenchChrome \? \(\s*<span className="app-chrome__spacer" aria-hidden="true" \/>/s.test(appChromeSource),
  "AppChrome workbench branch skips the tab strip",
);

ok(
  /app-chrome__tools--fixed/.test(appChromeSource),
  "AppChrome renders the command search as a fixed chrome tool",
);

ok(
  /workbenchChromeHidden\s*=\s*sidebarWorkbench/.test(appSource),
  "workbench chrome is hidden for every desktop platform",
);

ok(
  /\{!workbenchChromeHidden && \(/.test(appSource),
  "workbench skips rendering the top AppChrome row",
);

ok(
  /topicbar__chrome-btn/.test(appSource),
  "workbench keeps chrome controls in the topic bar",
);

for (const selector of [
  ".app--darwin .app-chrome--tabs",
  ":root[data-theme-style] .app--darwin .app-chrome--tabs",
]) {
  const rightSpace = finalDeclaration(selector, "padding-right") ?? finalDeclaration(selector, "padding") ?? "";
  ok(
    rightSpace.includes("--chrome-toggle-size") && !rightSpace.includes("--chrome-right-toggle-offset"),
    `${selector} reserves fixed chrome tool width without shrinking for the right dock`,
  );
}

for (const selector of [
  ".app--windows .app-chrome--native-tabs",
  ".app--linux .app-chrome--native-tabs",
  ":root[data-theme-style] .app--windows .app-chrome--native-tabs",
  ":root[data-theme-style] .app--linux .app-chrome--native-tabs",
]) {
  const rightSpace = finalDeclaration(selector, "padding-right") ?? finalDeclaration(selector, "padding") ?? "";
  ok(
    rightSpace.includes("--chrome-right-toggle-offset"),
    `${selector} reserves right-dock width before rendering tabs`,
  );
}

for (const selector of [
  ".layout--workbench-chrome-hidden",
  ":root[data-theme-style] .layout--workbench-chrome-hidden",
]) {
  ok(
    finalDeclaration(selector, "--app-chrome-height") === "0px" &&
      finalDeclaration(selector, "grid-template-rows") === "minmax(0, 1fr)" &&
      finalDeclaration(selector, "background") === "var(--bg)",
    `${selector} removes the workbench chrome row`,
  );
}

ok(
  finalDeclaration(":root[data-theme-style] .app--darwin .layout--workbench-chrome-hidden", "--app-chrome-height") === "0px" &&
    finalDeclaration(".app--darwin .layout--workbench-chrome-hidden .sidebar--workbench", "padding-top") === "46px" &&
    finalDeclaration(".app--darwin .layout--workbench-chrome-hidden.layout--sidebar-collapsed .topicbar", "padding-left") === "96px",
  "macOS workbench leaves safe space for inset window controls",
);

for (const selector of [
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__chrome-btn",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__icon-btn",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__action-btn",
]) {
  ok(
    finalDeclaration(selector, "box-shadow") === "none",
    `${selector} stays flat after removing the workbench chrome row`,
  );
}

ok(
  finalDeclaration(":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar", "background") === "var(--chat-bg)",
  "workbench topic bar uses a flat chat background",
);

for (const selector of [
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__identity",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__title-row",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__title-row h1",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .tooltip-trigger:has(.topicbar__icon-btn)",
]) {
  ok(
    finalDeclaration(selector, "background") === "transparent" &&
      finalDeclaration(selector, "box-shadow") === "none" &&
      finalDeclaration(selector, "filter") === "none",
    `${selector} cannot paint residual title-row shadows in workbench mode`,
  );
}

for (const selector of [
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__icon-btn",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__chrome-btn",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__icon-btn:hover",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__icon-btn:focus-visible",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__chrome-btn:hover:not(.topicbar__chrome-btn--blocked)",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__chrome-btn:focus-visible:not(.topicbar__chrome-btn--blocked)",
]) {
  ok(
    finalDeclaration(selector, "background") === "transparent",
    `${selector} does not paint a hover block in workbench mode`,
  );
}

ok(
  finalDeclaration(".skip-to-composer", "box-shadow") === "none" &&
    finalDeclaration(".skip-to-composer:focus-visible", "box-shadow")?.includes("0 12px 28px"),
  "offscreen skip link does not leak its focus shadow into the workbench title area",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
