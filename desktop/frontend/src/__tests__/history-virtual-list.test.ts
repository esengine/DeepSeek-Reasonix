// Run: tsx src/__tests__/history-virtual-list.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const styles = readFileSync(resolve(testDir, "../styles.css"), "utf8");
const virtualListSource = readFileSync(resolve(testDir, "../components/VirtualList.tsx"), "utf8");

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

console.log("\nhistory virtual list");

ok(
  /getItemKey\s*:/.test(virtualListSource),
  "VirtualList passes stable row keys to TanStack virtualizer",
);

ok(
  /getKey\(item,\s*index\)/.test(virtualListSource),
  "VirtualList derives virtualizer item keys from the caller-provided key",
);

eq(
  finalDeclaration(".history-virtual-row .hist-item", "margin-bottom"),
  "0",
  "virtualized history rows remove default item spacing",
);

eq(
  finalDeclaration(":root[data-theme-style] .history-virtual-row .hist-item", "margin-bottom"),
  "0",
  "theme-styled virtualized history rows keep item spacing disabled",
);

ok(
  styles.indexOf(":root[data-theme-style] .history-virtual-row .hist-item") >
    styles.indexOf(":root[data-theme-style] .hist-item"),
  "theme virtual-row spacing override follows the generic themed history item rule",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
