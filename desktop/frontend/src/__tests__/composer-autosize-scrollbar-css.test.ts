// Run: tsx src/__tests__/composer-autosize-scrollbar-css.test.ts
//
// Contract: in autosize mode the composer textarea hides its scrollbar while
// the content fits, but once the content exceeds the max height (the JS
// inline overflow-y flips to auto and the card gains
// `.composer-card--auto-overflow`) the thin scrollbar reappears so long
// drafts expose their scrollability (#8494/#8742/#9019). The hero (creation)
// input cap is min(30vh, 160px), not the old 96px hard clip.

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
// Strip comments so declaration parsing never matches prose inside them.
const styles = readFileSync(resolve(testDir, "../styles.css"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
const composerSource = readFileSync(resolve(testDir, "../components/Composer.tsx"), "utf8");

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

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    ok(true, label);
  } else {
    ok(false, `${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}`);
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

console.log("\ncomposer autosize scrollbar css");

eq(finalDeclaration(".composer__input", "scrollbar-width"), "none", "autosize textarea hides the scrollbar while content fits");
eq(finalDeclaration(".composer__input::-webkit-scrollbar", "display"), "none", "autosize textarea hides the webkit scrollbar while content fits");
eq(finalDeclaration(".composer-card--auto-overflow .composer__input", "scrollbar-width"), "thin", "overflowing autosize textarea restores the thin scrollbar");
eq(finalDeclaration(".composer-card--auto-overflow .composer__input::-webkit-scrollbar", "display"), "block", "overflowing autosize textarea restores the webkit scrollbar");
eq(finalDeclaration(".composer-card--auto-overflow .composer__input::-webkit-scrollbar", "width"), "6px", "overflowing autosize scrollbar matches the resized-card width");
eq(
  finalDeclaration(".app--creation .composer-wrap--hero .composer__input", "max-height"),
  "min(30vh, 160px) !important",
  "hero input cap is min(30vh, 160px) instead of the 96px hard clip",
);
eq(
  finalDeclaration(".composer__input.composer__input--measure", "position"),
  "absolute",
  "autosize mirror stays outside the composer layout flow",
);
eq(
  finalDeclaration(".composer__input.composer__input--measure", "visibility"),
  "hidden",
  "autosize mirror is never visible to the user",
);

ok(
  /composerAutoOverflow = composerHeight === null && textareaAutoOverflow/.test(composerSource)
    && /composerAutoOverflow \? " composer-card--auto-overflow" : ""/.test(composerSource),
  "Composer wires the auto-overflow modifier to the autosize overflow state",
);
ok(
  /Math\.min\(Math\.floor\(window\.innerHeight \* 0\.3\), 160\)/.test(composerSource),
  "hero input JS cap is min(30vh, 160px)",
);
ok(
  /const measureTaRef = useRef<HTMLTextAreaElement>\(null\)/.test(composerSource)
    && /ref=\{measureTaRef\}[\s\S]*?value=\{text\}[\s\S]*?aria-hidden="true"/.test(composerSource),
  "Composer renders a text-synchronized, accessibility-hidden measurement mirror",
);
ok(
  !/\.style\.height\s*=\s*"auto"/.test(composerSource),
  "autosize measurement never collapses the live textarea",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
