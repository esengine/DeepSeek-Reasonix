// Run: tsx src/__tests__/composer-resized-input-css.test.ts
//
// Contract: in the user-resized composer (`.composer-card--resized`), the
// input fills the fixed card height and scrolls internally. JS applies no
// inline height in resized mode, so `height: auto` would collapse the
// rows=1 textarea to a single visible line forever (regression shipped in
// desktop-v1.17.11: typing multiline text kept the input one line tall).

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
// Strip comments so declaration parsing never matches prose inside them.
const styles = readFileSync(resolve(testDir, "../styles.css"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");

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

console.log("\ncomposer resized input css");

eq(finalDeclaration(".composer-card--resized .composer__input", "height"), "100%", "resized textarea fills the card's input area");
eq(finalDeclaration(".composer-card--resized .composer__input", "overflow-y"), "auto", "resized textarea scrolls internally");
eq(finalDeclaration(".composer-card--resized .composer__rich-input", "height"), "100%", "resized rich input fills the card's input area");
eq(finalDeclaration(".composer-card--resized .composer__rich-input", "overflow-y"), "auto", "resized rich input scrolls internally");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
