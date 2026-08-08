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
const enLocaleSource = readFileSync(resolve(here, "../locales/en.ts"), "utf8");
const zhLocaleSource = readFileSync(resolve(here, "../locales/zh.ts"), "utf8");
const zhTWLocaleSource = readFileSync(resolve(here, "../locales/zh-TW.ts"), "utf8");

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
  /initialFocus\?\.target === "model-access"[\s\S]*?initialFocus\?\.target === "model-stats"[\s\S]*?"usage"/.test(settingsSource),
  "model settings honor access and statistics focus targets while preserving usage as the default",
);
ok(
  !settingsSource.includes("modelFocusHandledRef"),
  "each fresh model focus object can re-target the same subtab again",
);
ok(
  /setSettingsFocus\(\(current\) => \(\{[\s\S]*?target: "model-stats",[\s\S]*?requestId: \(current\?\.requestId \?\? 0\) \+ 1,[\s\S]*?\}\)\)/.test(appSource) &&
    /initialFocus\?\.requestId/.test(settingsSource),
  "usage statistics commands derive a monotonic request id from the shared focus state",
);
ok(
  /case "deepseek-responses":\s*return t\("settings\.addProvider\.preset\.deepseekResponsesDesc"\)/.test(settingsSource),
  "DeepSeek Responses preset uses a localized description",
);
ok(
  /case "deepseek-anthropic":\s*return t\("settings\.addProvider\.preset\.deepseekAnthropicDesc"\)/.test(settingsSource),
  "DeepSeek Anthropic preset uses a localized description",
);
ok(
  /case "token-rhythm":\s*return t\("settings\.addProvider\.preset\.tokenRhythmDesc"\)/.test(settingsSource) &&
    /preset\.id === "token-rhythm"\) return t\("settings\.addProvider\.preset\.tokenRhythmLabel"\)/.test(settingsSource),
  "Token Rhythm preset localizes the English and Chinese brand names",
);
ok(
  [enLocaleSource, zhLocaleSource, zhTWLocaleSource].every((source) =>
    source.includes('"settings.addProvider.preset.deepseekResponsesDesc"') &&
    source.includes('"settings.addProvider.preset.deepseekAnthropicDesc"') &&
    source.includes('"settings.addProvider.preset.tokenRhythmLabel"') &&
    source.includes('"settings.addProvider.preset.tokenRhythmDesc"'),
  ),
  "provider preset localization is present in every supported locale",
);
ok(
  enLocaleSource.includes('"settings.addProvider.preset.tokenRhythmLabel": "Token Rhythm"') &&
    zhLocaleSource.includes('"settings.addProvider.preset.tokenRhythmLabel": "基元律动"') &&
    zhTWLocaleSource.includes('"settings.addProvider.preset.tokenRhythmLabel": "基元律动"'),
  "Token Rhythm preset uses the official English and Chinese brand names",
);
ok(
  [enLocaleSource, zhLocaleSource, zhTWLocaleSource].every((source) =>
    source.includes('"settings.reasoningProtocol.glm"'),
  ),
  "GLM reasoning protocol is localized in every supported locale",
);
ok(
  settingsSource.includes('settings.reasoningSummary') &&
    settingsSource.includes('settings.reasoningSummary.on') &&
    settingsSource.includes('setReasoningSummaryEnabled(enabled)'),
  "General settings exposes live reasoning-summary options",
);
ok(
  [enLocaleSource, zhLocaleSource, zhTWLocaleSource].every((source) =>
    source.includes('"settings.reasoningSummary"') &&
    source.includes('"settings.reasoningSummaryHint"') &&
    source.includes('"settings.reasoningSummary.on"') &&
    source.includes('"settings.reasoningSummary.off"'),
  ),
  "reasoning-summary switch labels are localized in every supported locale",
);
ok(
  /mockPreset\("deepseek-anthropic",\s*"DeepSeek Anthropic"/.test(bridgeSource),
  "browser mock exposes the DeepSeek Anthropic preset",
);
ok(
  /mockPreset\("token-rhythm",\s*"Token Rhythm"/.test(bridgeSource),
  "browser mock exposes the Token Rhythm preset",
);
ok(
  /function mockProviderPresetDisplayRank\(id: string\): number \{\s*if \(id === "deepseek-responses"\) return -1;\s*if \(id === "deepseek-anthropic"\) return 0;/.test(bridgeSource),
  "browser mock ranks Responses first and Anthropic as the adjacent compatibility preset",
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
