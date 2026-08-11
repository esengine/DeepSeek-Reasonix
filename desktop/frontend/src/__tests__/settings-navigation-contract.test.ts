// Run: tsx src/__tests__/settings-navigation-contract.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const panel = readFileSync(resolve(testDir, "../components/SettingsPanel.tsx"), "utf8");
const navigation = readFileSync(resolve(testDir, "../components/SettingsNavigation.tsx"), "utf8");

let passed = 0;
let failed = 0;

function ok(condition: boolean, label: string) {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\nsettings navigation contract");

ok(panel.includes('["workbench", "classic", "creation"] as const'), "desktop styles prioritize workbench before classic and creation");
ok(/useEffect\(\(\) => \{[\s\S]*?content\.scrollTop = 0;[\s\S]*?content\.scrollLeft = 0;[\s\S]*?\}, \[tab\]\);/.test(panel), "switching settings pages resets both content scroll axes");
ok(navigation.includes('aria-current={activeTab === id ? "page" : undefined}'), "the active settings page is exposed semantically");
ok(navigation.includes('item.meta && (activeTab === id || query.trim())'), "navigation metadata stays limited to the active or searched items");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
