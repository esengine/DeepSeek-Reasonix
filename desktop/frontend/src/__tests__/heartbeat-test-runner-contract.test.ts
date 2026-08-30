// Run: tsx src/__tests__/heartbeat-test-runner-contract.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

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

const testDir = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(resolve(testDir, "heartbeat-next-run.test.ts"), "utf8");
const exitChecks = Array.from(source.matchAll(/if \(failed > 0\) process\.exit\(1\);/g));
const finalAssertion = Math.max(source.lastIndexOf("eq("), source.lastIndexOf("ok("));
const finalExit = source.lastIndexOf("if (failed > 0) process.exit(1);");

console.log("\nheartbeat test runner contract");
ok(exitChecks.length === 1, "focused suite has exactly one failing-exit gate");
ok(finalExit > finalAssertion, "failing-exit gate runs after every assertion");
ok(source.indexOf("passed, ${failed} failed", finalAssertion) > finalAssertion, "final summary runs after every assertion");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
