// Run: tsx src/__tests__/settings-storage-page.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const settings = readFileSync(resolve(testDir, "../components/SettingsPanel.tsx"), "utf8");
const settingsNavigation = readFileSync(resolve(testDir, "../components/SettingsNavigation.tsx"), "utf8");
const storagePage = readFileSync(resolve(testDir, "../components/StorageSettingsPage.tsx"), "utf8");
const bridge = readFileSync(resolve(testDir, "../lib/bridge.ts"), "utf8");
const backend = readFileSync(resolve(testDir, "../../../storage_app.go"), "utf8");
const styles = readFileSync(resolve(testDir, "../styles.css"), "utf8");
const types = readFileSync(resolve(testDir, "../lib/types.ts"), "utf8");
const locales = ["zh.ts", "zh-TW.ts", "en.ts"].map((name) => readFileSync(resolve(testDir, `../locales/${name}`), "utf8"));

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

console.log("\nsettings storage page contract");

const tabs = settingsNavigation.match(/SETTINGS_NAV_TABS: SettingsTab\[\] = \[([^\]]+)\]/)?.[1] ?? "";
ok(/"storage",\s*"updates",?\s*$/.test(tabs), "Storage is immediately before Updates in the settings navigation");
ok(types.includes('| "storage" | "updates";'), "SettingsTab exposes the storage route before updates");
ok(settings.includes('tab === "storage"') && settings.includes("<StorageSettingsPage />"), "Storage renders the same page on every platform");
ok(settings.includes('import("./StorageSettingsPage")'), "Storage data and UI are loaded only when its page bundle renders");

const generalStart = settings.indexOf("function GeneralSection(");
const generalEnd = settings.indexOf("function ModelsSection(", generalStart);
const generalSection = settings.slice(generalStart, generalEnd);
ok(!generalSection.includes("StorageSettingsPage"), "General no longer embeds storage settings");
ok(storagePage.includes('className="mem-input"') && !storagePage.includes('className="text-input"'), "Storage paths reuse the shared settings input recipe");
ok(storagePage.includes("readOnly"), "Every storage path input is explicitly read-only");

const mutations = ["PickStorageFolder", "MigrateStorage", "SetDefaultWorkspace"];
ok(mutations.every((name) => !storagePage.includes(name) && !bridge.includes(name) && !backend.includes(name)), "Storage mutation methods are absent from UI and Wails surfaces");
ok(!styles.includes("settings-path-control"), "Storage page contains no migration-only styling");
ok(!storagePage.includes("storageReadOnlyHint") && locales.every((locale) => !locale.includes("settings.storageReadOnlyHint")), "Storage page omits the redundant read-only notice");
ok(locales.every((locale) => !/Windows.*(?:迁移|移動|migrat)/i.test(locale.match(/"settings\.pageDesc\.storage":\s*"[^"]*"/)?.[0] ?? "")), "Storage page descriptions do not advertise Windows migration");
ok(!/\b(?:Walk|sizeBytes|availableBytes|models)\b/.test(backend), "Storage query performs no recursive size scan and exposes no synthetic models path");
ok(!types.includes("storage: StorageSettingsView"), "General SettingsView no longer owns storage data");
ok(!settings.match(/const needsSettings[^;]+/)?.[0].includes('tab === "storage"'), "Storage remains available when the general settings request fails");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
