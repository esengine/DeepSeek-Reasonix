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

console.log("\nrecovery banner privacy");

const recoveryBannerMatch = /<div className="banner banner--recovery"[\s\S]*?<\/div>/.exec(appSource);
const recoveryBanner = recoveryBannerMatch?.[0] ?? "";

ok(Boolean(recoveryBannerMatch), "App renders a recovery banner");
ok(!recoveryBanner.includes("title="), "recovery banner does not expose internals through a tooltip");
ok(!recoveryBanner.includes("recoveryDigest"), "recovery banner does not expose recovery digest");
ok(!recoveryBanner.includes("recoveryParentId"), "recovery banner does not expose parent session id");
ok(!appSource.includes("recoveryBannerTitle"), "App no longer builds a detailed recovery tooltip");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
