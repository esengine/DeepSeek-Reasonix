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
const settingsNavigationSource = readFileSync(resolve(here, "../components/SettingsNavigation.tsx"), "utf8");
const stylesSource = readFileSync(resolve(here, "../styles.css"), "utf8") +
  readFileSync(resolve(here, "../components/ProviderAccessSettings.css"), "utf8") +
  readFileSync(resolve(here, "../components/SettingsPanel.css"), "utf8");
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
  appSource.includes('hydrateReasoningDisplayMode("auto", false);'),
  "startup failure preserves legacy reasoning-display migration precedence",
);
ok(
  bridgeSource.includes('displayMode: "standard", reasoningDisplayMode: "auto", reasoningDisplayModeExplicit: false'),
  "browser startup defaults match the classic standard/live-follow experience",
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
  !/case "deepseek-anthropic":\s*return t\("settings\.addProvider\.preset\.deepseekAnthropicDesc"\)/.test(settingsSource),
  "redundant DeepSeek Anthropic preset is not separately localized in the provider list",
);
ok(
  /case "token-rhythm":\s*return t\("settings\.addProvider\.preset\.tokenRhythmDesc"\)/.test(settingsSource) &&
    /case "deepseek-responses":\s*return t\("settings\.addProvider\.preset\.deepseekResponsesLabel"\)/.test(settingsSource) &&
    !/case "deepseek-anthropic":\s*return t\("settings\.addProvider\.preset\.deepseekAnthropicLabel"\)/.test(settingsSource) &&
    /case "token-rhythm":\s*return t\("settings\.addProvider\.preset\.tokenRhythmLabel"\)/.test(settingsSource),
  "visible official protocol presets and Token Rhythm localize their display names",
);
ok(
  [enLocaleSource, zhLocaleSource, zhTWLocaleSource].every((source) =>
    source.includes('"settings.addProvider.preset.deepseekResponsesDesc"') &&
    source.includes('"settings.addProvider.preset.deepseekResponsesLabel"') &&
    !source.includes('"settings.addProvider.preset.deepseekAnthropicDesc"') &&
    !source.includes('"settings.addProvider.preset.deepseekAnthropicLabel"') &&
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
  settingsSource.includes('settings.general.sectionConversation') &&
    settingsSource.includes('settings.displayMode') &&
    settingsSource.includes('["standard", "compact"]') &&
    settingsSource.includes('settings.reasoningDisplay') &&
    settingsSource.includes('["hidden", "summary", "auto"]') &&
    settingsSource.includes('settings.processFold') &&
    settingsSource.includes('["auto", "expanded"]') &&
    settingsSource.includes('setProcessFoldPreference(pref)') &&
    settingsSource.includes('app.SetReasoningDisplayMode(mode)'),
  "General settings presents transcript density, reasoning display, and completed-work folding in one conversation section",
);
ok(
  [enLocaleSource, zhLocaleSource, zhTWLocaleSource].every((source) =>
    source.includes('"settings.sessionContentDisplay"') &&
    source.includes('"settings.sessionContentDisplayHint"') &&
    source.includes('"settings.displayMode"') &&
    source.includes('"settings.reasoningDisplay"') &&
    source.includes('"settings.reasoningDisplay.hidden"') &&
    source.includes('"settings.reasoningDisplay.summary"') &&
    source.includes('"settings.reasoningDisplay.auto"') &&
    source.includes('"settings.processFold"'),
  ),
  "conversation-content display group labels are localized in every supported locale",
);
ok(
  stylesSource.includes(".settings-page--general .settings-section") &&
    stylesSource.includes("grid-template-columns: minmax(260px, 1fr) max-content") &&
    stylesSource.includes(".settings-field__copy--icon") &&
    stylesSource.includes("@media (max-width: 900px)"),
  "General controls use the selected flat responsive section layout",
);
ok(
  settingsNavigationSource.includes('settings.searchPlaceholder') &&
    settingsNavigationSource.includes('SETTINGS_TAB_GROUPS') &&
    settingsNavigationSource.includes('settings-center__navgroup') &&
    settingsNavigationSource.includes('settingsTabIcon(id)') &&
    [enLocaleSource, zhLocaleSource, zhTWLocaleSource].every((source) =>
      source.includes('"settings.navGroup.preferences"') &&
      source.includes('"settings.searchNoResults"'),
    ),
  "settings navigation provides searchable localized groups and visible category icons",
);
ok(
  /function settingsTabMeta[\s\S]*?case "skills":\s+return t\("settings\.tabSub\.skills"\)/.test(settingsSource) &&
    enLocaleSource.includes('"settings.tab.skills": "Agent Skills"') &&
    enLocaleSource.includes('"settings.tabSub.skills": "Reusable instructions, tools & workflows"') &&
    zhLocaleSource.includes('"settings.tab.skills": "Agent Skills"') &&
    zhLocaleSource.includes('"settings.tabSub.skills": "可复用的指令、工具与工作流"') &&
    zhTWLocaleSource.includes('"settings.tab.skills": "Agent Skills"') &&
    zhTWLocaleSource.includes('"settings.tabSub.skills": "可重複使用的指令、工具與工作流程"'),
  "Agent Skills navigation uses the dedicated reusable-workflow description in every supported locale",
);
ok(
  [settingsSource, enLocaleSource, zhLocaleSource, zhTWLocaleSource, stylesSource].every((source) =>
    !source.includes("settings.workProcess") &&
    !source.includes("settings-work-process"),
  ),
  "the superseded work-process-only visual group does not return",
);
ok(
  [settingsSource, enLocaleSource, zhLocaleSource, zhTWLocaleSource].every((source) =>
    !source.includes("settings.reasoningSummary"),
  ),
  "the retired standalone reasoning-summary setting does not return",
);
ok(
  !/mockPreset\("deepseek-anthropic",/.test(bridgeSource),
  "browser mock hides the redundant DeepSeek Anthropic preset",
);
ok(
  bridgeSource.includes('value === "deepseek-upgrade"') &&
    bridgeSource.includes('recommendedUpgradeAvailable: deepSeekUpgradeMock') &&
    bridgeSource.includes('headers: deepSeekUpgradeMock ? { "X-Route": "official-custom" } : undefined'),
  "browser mock can preview the customized legacy DeepSeek upgrade flow",
);
ok(
  /async ConnectKey\(apiKey: string\)[\s\S]*?await this\.AddOfficialProviderAccess\("deepseek", apiKey\)/.test(bridgeSource),
  "browser onboarding installs the same current DeepSeek official template as production",
);
ok(
  settingsSource.includes("officialProviders={s.officialProviders}") &&
    settingsSource.includes("added: Boolean(state?.added)") &&
    settingsSource.includes("keySet: Boolean(state?.keySet)"),
  "official provider templates honor the backend installed state",
);
ok(
  /onUpgradeRecommended=\{\(name\) => \{[\s\S]*?cancelGroupFetch\(group\.id\);[\s\S]*?return apply\(\(\) => app\.UpgradeDeepSeekProviderAccess\(name\)\)/.test(settingsSource) &&
    settingsSource.includes("onConfirm={() => onUpgradeRecommended(canonicalOfficialProviderName(upgradeProvider.name))}") &&
    settingsSource.includes('className="provider-protocol-upgrade"') &&
    settingsSource.includes('t("settings.providerProtocol")}: OpenAI Chat Completions') &&
    /<InlineConfirmButton[\s\S]*?label=\{<>\{t\("settings\.upgradeRecommendedProtocol"\)\}[\s\S]*?primary[\s\S]*?onConfirm=\{\(\) => onUpgradeRecommended/.test(settingsSource),
  "legacy official DeepSeek cards expose an explicit recommended-protocol action",
);
ok(
  settingsSource.includes("const providerNames = group.providers.map((provider) => provider.name)") &&
    settingsSource.includes("app.SetProviderWebSearch(providerNames, enabled)") &&
    !settingsSource.includes("app.SaveProvider({ ...provider, webSearch: enabled })"),
  "grouped DeepSeek profiles update server-side web search through one atomic backend call",
);
ok(
  /<div className="provider-access-card__actions">[\s\S]*?<ProviderAccessMoreMenu[\s\S]*?<\/div>\s*<\/div>\s*\{group\.description && <div className="provider-access-card__desc">[\s\S]*?\{upgradeProvider && \(/.test(settingsSource) &&
    settingsSource.includes('className="provider-access-more__menu"') &&
    settingsSource.includes('buttonRole="menuitem"') &&
    stylesSource.includes(".provider-protocol-upgrade") &&
    stylesSource.includes(".provider-access-more__menu"),
  "provider protocol migration stays in a stable row and removal lives in the overflow menu",
);
ok(
  settingsSource.includes('className="provider-technical-details"') &&
    settingsSource.includes('t("settings.providerCapabilities")') &&
    settingsSource.includes('showModelSummary && (') &&
    settingsSource.includes('hiddenModelCount={hiddenModelCount ?? 0}') &&
    !settingsSource.includes('className="provider-capability-badges"') &&
    !settingsSource.includes('t("settings.anthropicCompatible")') &&
    stylesSource.includes('.provider-card-block--inline') &&
    stylesSource.includes('.provider-technical-details'),
  "provider cards keep descriptions out of the action row and collapse repeated diagnostics into compact details",
);
ok(
  !settingsSource.includes("return p.baseUrl;") &&
    settingsSource.includes("group.providers.every(providerSupportsServerWebSearchForView)") &&
    settingsSource.includes("group.providers.every((provider) => Boolean(provider.webSearch))") &&
    settingsSource.includes("supported={supportsServerWebSearch}"),
  "all provider cards keep endpoint details collapsed and use backend web-search capability authority",
);
ok(
  /existing\.recommendedUpgradeAvailable = existing\.recommendedUpgradeAvailable \|\| Boolean\(p\.recommendedUpgradeAvailable\)/.test(settingsSource) &&
    /case "deepseek-flash":\s*case "deepseek-pro":\s*return "deepseek";/.test(settingsSource),
  "legacy DeepSeek aliases remain grouped into one official provider card",
);
ok(
  settingsSource.includes('className="btn btn--small provider-profile-row__refresh"') &&
    settingsSource.includes('className="btn btn--small provider-profile-row__configure"') &&
    stylesSource.includes('"profile-refresh profile-configure"') &&
    stylesSource.includes(".provider-profile-row__refresh") &&
    stylesSource.includes(".provider-profile-row__configure"),
  "multi-profile provider actions move into stable narrow-screen grid areas",
);
ok(
  /@media \(max-width: 900px\)[\s\S]*?\.settings-section__head\s*\{[\s\S]*?flex-direction:\s*column;[\s\S]*?align-items:\s*stretch;/.test(stylesSource),
  "narrow settings section headings stretch so descriptions wrap inside the viewport",
);
ok(
  [enLocaleSource, zhLocaleSource, zhTWLocaleSource].every((source) =>
    source.includes('"settings.upgradeRecommendedProtocol"') &&
    source.includes('"settings.providerAccess"') &&
    source.includes('"settings.providerBaseUrlLabel"') &&
    source.includes('"settings.providerApiKeyEnv"') &&
    !source.includes('"settings.anthropicCompatible"'),
  ),
  "the compact DeepSeek access and upgrade flows are localized in every supported locale",
);
ok(
  enLocaleSource.includes('"settings.serverWebSearchHint": "Searches only when needed; content is sent to the current model provider') &&
    zhLocaleSource.includes('"settings.serverWebSearchHint": "仅在需要时搜索；内容会发送给当前供应商') &&
    zhTWLocaleSource.includes('"settings.serverWebSearchHint": "僅在需要時搜尋；內容會傳送給目前供應商'),
  "server web-search disclosure stays provider-neutral for compatible services",
);
ok(
  /mockPreset\("token-rhythm",\s*"Token Rhythm"/.test(bridgeSource),
  "browser mock exposes the Token Rhythm preset",
);
ok(
  /function mockProviderPresetDisplayRank\(id: string\): number \{\s*if \(id === "deepseek-responses"\) return -2;/.test(bridgeSource),
  "browser mock keeps the visible DeepSeek Responses preset first",
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
