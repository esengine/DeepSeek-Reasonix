// Run: tsx src/__tests__/typography-overflow-contract.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { TEXT_SIZES } from "../lib/textSize";

const testDir = dirname(fileURLToPath(import.meta.url));
const styles = readFileSync(resolve(testDir, "../styles.css"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");

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

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function matchingBlocks(selector: string): string[] {
  const blocks: string[] = [];
  const rule = /([^{}]+)\{([^{}]*)\}/g;
  let match: RegExpExecArray | null;
  while ((match = rule.exec(styles)) !== null) {
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

function hasDeclaration(selector: string, property: string, expected: string): boolean {
  return matchingBlocks(selector).some((block) => {
    const declaration = new RegExp(`(?:^|;)\\s*${property}\\s*:\\s*([^;]+)`, "g");
    let match: RegExpExecArray | null;
    while ((match = declaration.exec(block)) !== null) {
      if (match[1].trim() === expected) return true;
    }
    return false;
  });
}

function clipsSingleLine(selector: string) {
  eq(finalDeclaration(selector, "overflow"), "hidden", `${selector} clips long text`);
  eq(finalDeclaration(selector, "text-overflow"), "ellipsis", `${selector} uses ellipsis`);
  eq(finalDeclaration(selector, "white-space"), "nowrap", `${selector} stays on one line`);
}

console.log("\ntypography overflow contract");

eq(
  JSON.stringify(TEXT_SIZES),
  JSON.stringify(["small", "default", "large", "xlarge", "xxlarge"]),
  "text-size presets include the large accessibility step",
);
eq(finalDeclaration(":root", "--sans"), "var(--font-ui)", "legacy sans alias stays synced with UI font");
eq(finalDeclaration(':root[data-text-size="xxlarge"]', "--font-scale"), "1.32", "xxlarge has a real scale bump");
ok(
  (finalDeclaration(":root", "--statusbar-dock-height") ?? "").includes("var(--font-scale)"),
  "status bar dock height scales with interface text size",
);
ok(
  hasDeclaration(".layout", "--statusbar-height", "var(--statusbar-dock-height)"),
  "layout reserves scaled status bar height",
);
eq(
  finalDeclaration(".app", "height"),
  "var(--app-viewport-height, 100%)",
  "app height follows the live viewport height variable",
);
eq(finalDeclaration(".transcript--empty", "overflow-y"), "auto", "empty transcript can scroll instead of clipping");
eq(finalDeclaration(".welcome", "overflow"), "visible", "welcome empty state is not clipped by its own box");
ok(
  hasDeclaration(".transcript--empty > .welcome", "margin-block", "auto"),
  "empty-state auto margins apply only to the welcome content",
);
ok(
  finalDeclaration(".transcript--empty > *", "margin-block") === undefined,
  "empty-state generic children do not receive auto margins",
);
eq(
  finalDeclaration(":root[data-theme-style] .statusbar", "height"),
  "var(--statusbar-dock-height)",
  "fixed status bar height follows the scaled dock token",
);
eq(
  finalDeclaration(":root[data-theme-style] .statusbar", "min-height"),
  "var(--statusbar-dock-height)",
  "status bar min-height follows the scaled dock token",
);
eq(finalDeclaration(".provider-template-grid", "grid-auto-rows"), "92px", "provider preset cards use compact equal-height grid rows");
eq(finalDeclaration(".provider-template-card", "height"), "100%", "provider preset cards stretch to the grid row height");
eq(finalDeclaration(".provider-template-card strong", "-webkit-line-clamp"), "1", "provider preset card titles clamp to one line");
eq(finalDeclaration(".provider-template-card span", "-webkit-line-clamp"), "2", "provider preset card descriptions clamp to two lines");

eq(finalDeclaration(".statusbar", "white-space"), "nowrap", "status bar keeps metrics on one row");
eq(finalDeclaration(".statusbar", "overflow"), "hidden", "status bar clips instead of overflowing");
clipsSingleLine(".statusbar__model");

for (const selector of [
  ".sidebar-im__summary-label",
  ".sidebar-im__summary-status",
  ".workbench-dock__tab-label",
  ".workspace-files__scope-title",
  ".workspace-files__scope-meta",
  ".context-panel__section-head span",
  ".context-panel__metric span",
  ".context-panel__metric strong",
  ".app--creation .context-panel__mini-stat span",
  ".app--creation .context-panel__mini-stat strong",
  ".topbar__model",
  ".composer-modebar__item span",
  ".composer-more-menu__item span",
]) {
  clipsSingleLine(selector);
}

eq(
  finalDeclaration(".app--creation .layout.layout--workspace-open", "transition"),
  "grid-template-columns 0s, min-width 0s",
  "creation dock skips zero-width grid interpolation on open",
);
eq(
  finalDeclaration(".app--creation .context-panel__usage", "animation"),
  "none",
  "creation overview usage card disables inherited entrance animation",
);
ok(
  finalDeclaration(".app--creation .context-panel__mini-stat", "justify-content") !== "space-between",
  "creation overview rows avoid edge-pinned value alignment",
);
ok(
  finalDeclaration(".app--creation .context-panel__mini-stat", "grid-template-columns") !== "minmax(0, 1fr) auto",
  "creation overview rows avoid the spacer grid that pushes values to the edge",
);
ok(
  finalDeclaration(".app--creation .context-panel__mini-stat strong", "max-width") !== "14ch",
  "creation overview values are not capped to a fixed 14ch width",
);

eq(finalDeclaration(".composer-modebar", "overflow"), "hidden", "chat mode switcher contains enlarged labels");
eq(finalDeclaration(".composer-meta__control--profile", "flex"), "0 0 auto", "work mode selector sizes to its localized label");
eq(finalDeclaration(".composer-meta__control--profile", "max-width"), "68px", "work mode selector keeps a compact narrow-width bound");
eq(finalDeclaration(".composer-profile-trigger__label", "overflow"), "hidden", "work mode selector clips only when space is constrained");
eq(finalDeclaration(".composer-profile-trigger__label", "text-overflow"), "ellipsis", "work mode selector shows an ellipsis when constrained");
eq(finalDeclaration(".composer-meta__control--intent", "max-width"), "72px", "task method selector keeps its current state visible at narrow widths");
eq(finalDeclaration(".composer-task-mode-trigger__value", "text-overflow"), "ellipsis", "task method selector truncates its value only when constrained");
eq(finalDeclaration(".composer-meta .modelsw__trigger", "font-weight"), "var(--composer-control-font-weight)", "model selector uses the shared control weight");
eq(finalDeclaration(".composer-meta__divider", "height"), "18px", "execution policy and model settings have a compact visual divider");
eq(finalDeclaration(".composer-modebar--approval", "--composer-modebar-active-bg"), "var(--mode-auto-bg)", "ask approval restores the solid semantic fill");
eq(finalDeclaration('.composer-modebar--approval[data-mode="auto"]', "--composer-modebar-active-fg"), "#fff", "auto approval keeps high-contrast text on its solid fill");
eq(finalDeclaration('.composer-modebar--approval[data-mode="yolo"]', "--composer-modebar-active-bg"), "var(--mode-yolo-bg)", "yolo approval restores the solid warning fill");
eq(finalDeclaration(".composer-intent-menu", "width"), "min(284px, calc(100vw - 16px))", "task method menu uses the shared menu width");
eq(finalDeclaration(".composer-profile-menu", "width"), "min(284px, calc(100vw - 16px))", "work mode menu uses the shared menu width");
eq(finalDeclaration(".composer-access-menu__desc", "white-space"), "normal", "menu descriptions can wrap onto a second line");
eq(finalDeclaration(".composer-access-menu__desc", "text-overflow"), "clip", "menu descriptions no longer use single-line ellipsis");
eq(finalDeclaration(".composer-profile-menu .composer-access-menu__desc", "font-size"), "12px", "work mode summaries use the shared control text size");
eq(finalDeclaration(".composer-profile-menu .composer-access-menu__desc", "color"), "var(--fg-dim)", "work mode summaries remain readable as secondary text");
eq(finalDeclaration(".composer-profile-menu .composer-access-menu__desc", "white-space"), "nowrap", "work mode summaries stay on one scannable line");
eq(finalDeclaration(".composer-task-mode-trigger:focus-visible", "box-shadow"), "var(--focus-ring)", "task method selector uses the shared keyboard focus ring");
eq(finalDeclaration(".composer-profile-trigger:focus-visible", "box-shadow"), "var(--focus-ring)", "work mode selector uses the shared keyboard focus ring");
eq(finalDeclaration(".composer-meta .modelsw__trigger:focus-visible", "box-shadow"), "var(--focus-ring)", "model and effort selectors use the shared keyboard focus ring");
eq(finalDeclaration(":root[data-theme-style] .composer-modebar__item--active:focus-visible", "box-shadow"), "var(--focus-ring)", "active permission options retain keyboard focus feedback");
eq(
  finalDeclaration(".app--creation .tool:not(.tool--open) > .tool__body", "height"),
  "0 !important",
  "collapsed creation tool bodies keep mounted content clipped",
);
eq(
  finalDeclaration(".app--creation .tool:not(.tool--open) > .tool__body", "visibility"),
  "hidden",
  "collapsed creation tool bodies do not paint hidden tool text",
);
ok(
  /@container\s*\(max-width:\s*760px\)[\s\S]*?\.composer-meta__control--model\s*\{[\s\S]*?flex\s*:\s*0 1 auto[\s\S]*?width\s*:\s*fit-content[\s\S]*?max-width\s*:\s*min\(240px,\s*42vw\)[\s\S]*?\.composer-meta__control--profile\s*\{[\s\S]*?max-width\s*:\s*126px[\s\S]*?\.composer-meta__control--intent\s*\{[\s\S]*?max-width\s*:\s*128px[\s\S]*?\.composer-meta__control--effort\s*\{[\s\S]*?display\s*:\s*none[\s\S]*?\.composer-meta__control--more\s*\{[\s\S]*?display\s*:\s*inline-flex/.test(styles),
  "composer compact controls activate at the capped theme width",
);
eq(finalDeclaration(".md table", "overflow-x"), "auto", "markdown tables scroll horizontally");
eq(finalDeclaration(".code", "overflow"), "auto", "code blocks scroll instead of widening the layout");
ok(
  /@media\s*\(max-width:\s*900px\)[\s\S]*?\.settings-center\s*\{[\s\S]*?grid-template-columns\s*:\s*1fr/.test(styles),
  "settings center stacks navigation before the modal is too narrow",
);
ok(
  /@media\s*\(max-width:\s*900px\)[\s\S]*?\.settings-field\s*\{[\s\S]*?grid-template-columns\s*:\s*1fr/.test(styles),
  "settings fields collapse to one column at the mid-width breakpoint",
);
ok(
  /@media\s*\(max-width:\s*760px\)[\s\S]*?\.settings-modal\s*\{[\s\S]*?width\s*:\s*100vw[\s\S]*?height\s*:\s*100vh/.test(styles),
  "settings modal only becomes fullscreen at the narrow breakpoint",
);
ok(
  /@media\s*\(max-width:\s*820px\)[\s\S]*?\.app\s+\.layout[\s\S]*?grid-template-columns\s*:\s*minmax\(0,\s*1fr\)\s*!important[\s\S]*?\.app\s+\.sidebar[\s\S]*?display\s*:\s*none\s*!important[\s\S]*?\.app\s+\.chat-pane[\s\S]*?grid-column\s*:\s*1\s*!important/.test(styles),
  "narrow workbench layout hides side panels and keeps chat single-column",
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
