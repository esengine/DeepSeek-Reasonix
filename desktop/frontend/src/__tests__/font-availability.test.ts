// Run: tsx src/__tests__/font-availability.test.ts

import { getAvailableFontFamilies, getAvailableMonoFontFamilies } from "../lib/fontAvailability";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

const noneInstalled = () => false;
const hasFont = (name: string) => (names: readonly string[]) => names.includes(name);

console.log("\nfont availability");

eq(
  getAvailableFontFamilies("system", "darwin", noneInstalled),
  ["system", "pingfang", "custom"],
  "macOS shows native UI font plus stable choices",
);
eq(
  getAvailableFontFamilies("system", "windows", noneInstalled),
  ["system", "yahei", "custom"],
  "Windows shows native UI font plus stable choices",
);
eq(
  getAvailableFontFamilies("system", "linux", noneInstalled),
  ["system", "noto", "custom"],
  "Linux shows Noto-style UI font plus stable choices",
);
eq(
  getAvailableFontFamilies("system", "darwin", hasFont("Microsoft YaHei")),
  ["system", "yahei", "pingfang", "custom"],
  "detected cross-platform UI fonts are included",
);
eq(
  getAvailableFontFamilies("yahei", "darwin", noneInstalled),
  ["system", "yahei", "pingfang", "custom"],
  "current UI font remains visible even when it is not native",
);

eq(
  getAvailableMonoFontFamilies("system", "darwin", noneInstalled),
  ["system", "sfmono", "custom"],
  "macOS shows native mono font plus stable choices",
);
eq(
  getAvailableMonoFontFamilies("system", "windows", noneInstalled),
  ["system", "cascadia", "custom"],
  "Windows shows native mono font plus stable choices",
);
eq(
  getAvailableMonoFontFamilies("system", "linux", hasFont("JetBrains Mono")),
  ["system", "jetbrains", "custom"],
  "detected mono fonts are included on Linux",
);
eq(
  getAvailableMonoFontFamilies("sfmono", "windows", noneInstalled),
  ["system", "cascadia", "sfmono", "custom"],
  "current mono font remains visible even when it is not native",
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
