// Run: tsx src/__tests__/startup-settings-contract.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  ONBOARDING_DISMISSED_STORAGE_KEY,
  dismissOnboarding,
  onboardingWasDismissed,
  shouldOpenOnboarding,
} from "../lib/onboarding";

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
const settingsSource = readFileSync(resolve(here, "../components/SettingsPanel.tsx"), "utf8");

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
ok(
  /onChooseProvider=\{\(\) => \{[\s\S]*?setSettingsFocus\(\{ target: "model-access" \}\);[\s\S]*?setSettingsTarget\("models"\);/.test(appSource),
  "onboarding opens the model access flow instead of model usage",
);
ok(
  /initialFocus\?\.target === "model-access" \? "access" : "usage"/.test(settingsSource),
  "model settings honor the onboarding access target while preserving usage as the default",
);

const values = new Map<string, string>();
const storage = {
  getItem: (key: string) => values.get(key) ?? null,
  setItem: (key: string, value: string) => values.set(key, value),
  removeItem: (key: string) => values.delete(key),
  clear: () => values.clear(),
  key: (index: number) => [...values.keys()][index] ?? null,
  get length() { return values.size; },
} as Storage;

ok(!onboardingWasDismissed(storage), "fresh installs have no onboarding dismissal marker");
ok(shouldOpenOnboarding(true, storage), "missing providers open the guide before dismissal");
ok(!shouldOpenOnboarding(false, storage), "configured providers never open the guide");
dismissOnboarding(storage);
ok(values.get(ONBOARDING_DISMISSED_STORAGE_KEY) === "1", "skip persists a versioned dismissal marker");
ok(!shouldOpenOnboarding(true, storage), "persisted skip prevents repeated full-screen interruption");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
