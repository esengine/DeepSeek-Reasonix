// Run: tsx src/__tests__/command-palette-css.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

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

console.log("\ncommand palette css");

eq(
  finalDeclaration(".palette__item", "display"),
  "flex",
  "palette result rows stay in flex layout",
);

eq(
  finalDeclaration(".palette__item", "flex-direction") ?? "row",
  "row",
  "palette result rows keep icon and text side-by-side",
);

eq(
  finalDeclaration(".palette__body", "min-width"),
  "0",
  "palette text body can shrink before ellipsis",
);

for (const selector of [".palette__title", ".palette__hint"]) {
  eq(finalDeclaration(selector, "overflow"), "hidden", `${selector} clips overflow inside the row`);
  eq(finalDeclaration(selector, "text-overflow"), "ellipsis", `${selector} uses ellipsis for long text`);
  eq(finalDeclaration(selector, "white-space"), "nowrap", `${selector} stays on one measured line`);
}

ok(
  !matchingBlocks(".palette__item").some((block) => /(?:^|;)\s*flex-direction\s*:\s*column\s*(?:;|$)/.test(block)),
  "no palette item rule can stack icon above long session text",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
