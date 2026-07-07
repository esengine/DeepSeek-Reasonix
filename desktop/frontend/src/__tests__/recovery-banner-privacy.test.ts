// Run: tsx src/__tests__/recovery-banner-privacy.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

let passed = 0;
let failed = 0;

function ok(cond: boolean, label: string) {
  if (cond) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

const here = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(here, "../App.tsx"), "utf8");

console.log("\nquiet recovery prompt privacy");

ok(!appSource.includes("banner--recovery"), "App does not render a persistent recovery banner");
ok(!appSource.includes("recoveryDigest"), "App does not expose recovery digest through recovery prompts");
ok(!appSource.includes("recoveryParentId"), "App does not expose parent session id through recovery prompts");
ok(!appSource.includes("recoveryBannerTitle"), "App does not build a detailed recovery tooltip");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
