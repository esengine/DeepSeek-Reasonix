// Run: tsx src/__tests__/workspace-preview-css.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";

const testDir = dirname(fileURLToPath(import.meta.url));
const styles = readFileSync(resolve(testDir, "../styles.css"), "utf8");
const appGo = readFileSync(resolve(testDir, "../../../app.go"), "utf8");
const localeNotices = [
  [readFileSync(resolve(testDir, "../locales/en.ts"), "utf8"), '"workspace.truncated": "Preview truncated to the first 2 MiB."'],
  [readFileSync(resolve(testDir, "../locales/zh.ts"), "utf8"), '"workspace.truncated": "预览已截断到前 2 MiB。"'],
  [readFileSync(resolve(testDir, "../locales/zh-TW.ts"), "utf8"), '"workspace.truncated": "預覽已截斷到前 2 MiB。"'],
];
const localeSearchLabels = [
  [readFileSync(resolve(testDir, "../locales/en.ts"), "utf8"), '"workspace.searchPlaceholder": "Find"'],
  [readFileSync(resolve(testDir, "../locales/zh.ts"), "utf8"), '"workspace.searchPlaceholder": "查找"'],
  [readFileSync(resolve(testDir, "../locales/zh-TW.ts"), "utf8"), '"workspace.searchPlaceholder": "尋找"'],
];

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
eq(finalDeclaration(".workspace-preview__body--code .code-block__wrap", "display"), "flex", "code wrapper keeps the preview viewport height");
eq(finalDeclaration(".workspace-preview__body--code .code-block__wrap", "flex"), "1 1 auto", "code wrapper participates in the preview flex height");
eq(finalDeclaration(".workspace-preview__body--code .code-block__wrap", "min-height"), "0", "code wrapper can shrink around the scrollable code viewport");
eq(finalDeclaration(".workspace-preview__body--code .code", "overflow"), "auto", "code viewport owns horizontal and vertical scrolling");
eq(finalDeclaration(".workspace-preview__body--code .code", "min-height"), "0", "code viewport can shrink inside the preview pane");
eq(finalDeclaration(".workspace-preview__body--code .code", "margin"), "0", "code viewport scrollbar sits at the visible pane bottom");
eq(finalDeclaration(".code-search__input", "min-width"), "0", "search input can shrink in the minimum preview width");
eq(finalDeclaration(".code-block__wrap--search-open .code-block__copy", "top"), "42px", "open search moves copy below its toolbar");
eq(
  computedDeclaration(
    `<html data-theme-style="default"><head></head><body><div class="workspace-preview__body workspace-preview__body--code"><div class="code code--lines"></div></div></body></html>`,
    ".code--lines",
    "padding",
  ),
  "0px",
  "themed workspace code keeps the line-number gutter flush",
);
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
eq(
  /const filePreviewLimit = 2 \* 1024 \* 1024/.test(appGo),
  true,
  "backend workspace preview limit remains 2 MiB",
);
eq(
  localeNotices.every(([source, expected]) => source.includes(expected)),
  true,
  "all locale truncation notices match the 2 MiB backend limit",
);
eq(
  localeNotices.every(([source]) => !source.includes("256 KB")),
  true,
  "locale truncation notices do not retain the previous 256 KB limit",
);
eq(
  localeSearchLabels.every(([source, expected]) => source.includes(expected)),
  true,
  "all locales provide workspace search labels",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
