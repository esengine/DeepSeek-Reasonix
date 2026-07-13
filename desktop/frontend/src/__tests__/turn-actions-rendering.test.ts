// Run: tsx src/__tests__/turn-actions-rendering.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const styles = readFileSync(resolve(testDir, "../styles.css"), "utf8");

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

console.log("\nturn actions rendering");

const creationTranscriptRule = styles.match(
  /\.app--creation \.transcript\.transcript--creation-scrollbar,\s*:root\[data-theme-style\] \.app--creation \.transcript\.transcript--creation-scrollbar\s*\{([^}]+)\}/,
)?.[1] ?? "";

ok(
  /background-color:\s*var\(--bg\);/.test(creationTranscriptRule),
  "creation scrollbar layer paints an opaque backdrop so removed turn actions repaint (#6359)",
);

ok(
  /scrollbar-width:\s*none;/.test(creationTranscriptRule),
  "opaque backdrop preserves the custom Creation scrollbar contract",
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
