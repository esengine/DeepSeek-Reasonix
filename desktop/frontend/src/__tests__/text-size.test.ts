// Run: tsx src/__tests__/text-size.test.ts

import { DEFAULT_TEXT_SIZE, nextTextSize } from "../lib/textSize";

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

console.log("\ntext size shortcuts");
eq(nextTextSize("small", 1), DEFAULT_TEXT_SIZE, "increase from small");
eq(nextTextSize("default", 1), "large", "increase from default");
eq(nextTextSize("large", 1), "xlarge", "increase from large");
eq(nextTextSize("xlarge", 1), "xlarge", "increase clamps at largest");
eq(nextTextSize("xlarge", -1), "large", "decrease from xlarge");
eq(nextTextSize("large", -1), DEFAULT_TEXT_SIZE, "decrease from large");
eq(nextTextSize("default", -1), "small", "decrease from default");
eq(nextTextSize("small", -1), "small", "decrease clamps at smallest");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
