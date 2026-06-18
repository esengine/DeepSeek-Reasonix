// Run: tsx src/__tests__/startup-settings-contract.test.ts

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
const bridgeSource = readFileSync(resolve(here, "../lib/bridge.ts"), "utf8");

console.log("\nstartup settings contract");

ok(
  bridgeSource.includes("DesktopStartupSettings()"),
  "bridge exposes a lightweight desktop startup settings call",
);
ok(
  appSource.includes("app.DesktopStartupSettings()"),
  "App loads startup chrome preferences through the lightweight settings call",
);
ok(
  !/const\s+reloadSidebarImConnections[\s\S]*?app\.Settings\(\)[\s\S]*?\}, \[t\]\);/.test(appSource),
  "sidebar IM refresh avoids rebuilding the full Settings payload",
);
ok(
  !/const\s+syncDesktopPreferences[\s\S]*?app\.Settings\(\)[\s\S]*?\};/.test(appSource),
  "startup preference sync avoids rebuilding the full Settings payload",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
