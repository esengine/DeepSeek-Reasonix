// Run: tsx src/__tests__/workspace-preview-css.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";

const testDir = dirname(fileURLToPath(import.meta.url));
const styles = readFileSync(resolve(testDir, "../styles.css"), "utf8");

let passed = 0;
let failed = 0;

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

function computedDeclaration(html: string, selector: string, property: string): string {
  const dom = new JSDOM(html);
  const style = dom.window.document.createElement("style");
  style.textContent = styles;
  dom.window.document.head.append(style);
  const element = dom.window.document.querySelector(selector);
  if (!element) throw new Error(`Missing selector in test DOM: ${selector}`);
  return dom.window.getComputedStyle(element).getPropertyValue(property).trim();
}

console.log("\nworkspace preview css");

eq(finalDeclaration(".workspace-preview__body--code", "overflow"), "hidden", "code preview body does not create a nested scroller");
eq(finalDeclaration(".workspace-preview__body--code", "display"), "flex", "code preview body hosts an editor-like viewport");
eq(finalDeclaration(".workspace-preview__body--code", "flex-direction"), "column", "truncated code notes stack above the code viewport");
eq(
  computedDeclaration(
    `<html data-theme-style="default"><head></head><body><aside class="workspace-panel workspace-panel--embedded"><div class="workspace-preview__body workspace-preview__body--code"></div></aside></body></html>`,
    ".workspace-preview__body--code",
    "padding",
  ),
  "0px",
  "code preview body keeps zero padding under embedded and themed cascade",
);
eq(finalDeclaration(".workspace-preview__body--code .workspace-note", "flex"), "0 0 auto", "code truncation note keeps its own row");
eq(finalDeclaration(".workspace-preview__body--code .code-block", "display"), "flex", "code block fills the preview viewport");
eq(finalDeclaration(".workspace-preview__body--code .code", "overflow"), "auto", "code viewport owns horizontal and vertical scrolling");
eq(finalDeclaration(".workspace-preview__body--code .code", "min-height"), "0", "code viewport can shrink inside the preview pane");
eq(finalDeclaration(".workspace-preview__body--code .code", "margin"), "0", "code viewport scrollbar sits at the visible pane bottom");
eq(
  finalDeclaration(".workspace-panel--with-tree-rail:not(.workspace-panel--tree-hidden)", "grid-template-columns"),
  "var(--workspace-tree-rail-width) var(--workspace-tree-width) minmax(var(--workspace-preview-min-width), 1fr)",
  "split mode keeps a narrow tree toggle rail beside the file tree",
);
eq(finalDeclaration(".workspace-panel--with-tree-rail:not(.workspace-panel--tree-hidden) .workspace-files", "grid-column"), "2", "file tree sits beside the rail");
eq(finalDeclaration(".workspace-panel--with-tree-rail:not(.workspace-panel--tree-hidden) .workspace-preview", "grid-column"), "3", "preview sits after rail and tree");
eq(
  finalDeclaration(".workspace-panel--with-tree-rail .workspace-tree-resizer", "left"),
  "calc(var(--workspace-tree-rail-width) + var(--workspace-tree-width) - 4px)",
  "tree resizer accounts for the persistent rail",
);
eq(
  finalDeclaration(".workspace-panel--tree-hidden", "grid-template-columns"),
  "var(--workspace-tree-rail-width) minmax(0, 1fr)",
  "preview-only mode keeps a narrow tree toggle rail",
);
eq(finalDeclaration(".workspace-panel--tree-hidden .workspace-preview", "grid-column"), "2", "preview sits beside the rail");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
