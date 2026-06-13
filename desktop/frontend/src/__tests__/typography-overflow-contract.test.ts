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
  finalDeclaration(":root[data-theme-style] .statusbar", "height"),
  "var(--statusbar-dock-height)",
  "fixed status bar height follows the scaled dock token",
);
eq(
  finalDeclaration(":root[data-theme-style] .statusbar", "min-height"),
  "var(--statusbar-dock-height)",
  "status bar min-height follows the scaled dock token",
);

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
  ".context-panel__file-copy > span",
  ".topbar__model",
  ".composer-modebar__item span",
  ".composer-more-menu__item span",
]) {
  clipsSingleLine(selector);
}

eq(finalDeclaration(".composer-modebar", "overflow"), "hidden", "chat mode switcher contains enlarged labels");
eq(finalDeclaration(".md table", "overflow-x"), "auto", "markdown tables scroll horizontally");
eq(finalDeclaration(".code", "overflow"), "auto", "code blocks scroll instead of widening the layout");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
