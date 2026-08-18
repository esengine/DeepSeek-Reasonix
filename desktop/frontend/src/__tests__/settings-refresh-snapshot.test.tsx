// Run: tsx src/__tests__/settings-refresh-snapshot.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import {
  SettingsPanel,
  formatProviderExtraBody,
  parseProviderExtraBody,
  providerExtraBodyParseError,
  providerEditorEffectiveKind,
  normalizeProviderView,
} from "../components/SettingsPanel";
import { LocaleProvider } from "../lib/i18n";
import type { AppBindings } from "../lib/bridge";
import type { ProviderView, SettingsView } from "../lib/types";
import {
  applyTypographyPreferences,
  createDefaultTypographyPreferences,
  getTypographyPreferences,
} from "../lib/typographyPreferences";
import {
  baseSettings,
  flushPromises,
  installCanvasMock,
  waitFor,
} from "../test-support/settingsTestFixtures";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    ok(true, label);
  } else {
    ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
  }
}

console.log("\nsettings refresh snapshot");

const nullableProvider = normalizeProviderView({
  name: null,
  baseUrl: null,
} as unknown as ProviderView);
eq(nullableProvider.name, "", "provider snapshots normalize a null name at the settings boundary");
eq(nullableProvider.baseUrl, "", "provider snapshots normalize a null base URL at the settings boundary");

const glmProvider = normalizeProviderView({
  name: "custom-glm",
  baseUrl: "https://gateway.example.com/v1",
  reasoningProtocol: "glm",
} as ProviderView);
eq(glmProvider.reasoningProtocol, "glm", "provider snapshots preserve the explicit GLM reasoning protocol");

const serverWebSearchProvider = normalizeProviderView({
  name: "custom-anthropic",
  kind: "anthropic",
  baseUrl: "https://gateway.example/anthropic",
  serverWebSearchCapability: true,
} as ProviderView);
eq(serverWebSearchProvider.serverWebSearchCapability, true, "provider snapshots preserve backend server web-search capability");

const legacyServerWebSearchProvider = normalizeProviderView({
  name: "legacy-anthropic",
  kind: "anthropic",
  baseUrl: "https://api.deepseek.com/anthropic",
} as ProviderView);
eq(legacyServerWebSearchProvider.serverWebSearchCapability, undefined, "older provider snapshots keep an absent capability distinguishable");

eq(providerEditorEffectiveKind(true, "anthropic", ["anthropic", "openai"]), "anthropic", "new custom providers keep the selected Anthropic-compatible kind");
eq(providerEditorEffectiveKind(false, "anthropic", ["anthropic", "openai"]), "anthropic", "existing providers preserve their stored kind");
eq(formatProviderExtraBody({ top_p: 0.7, enable_thinking: true }), "{\n  \"enable_thinking\": true,\n  \"top_p\": 0.7\n}", "extra body editor formats stable JSON");
eq(JSON.stringify(parseProviderExtraBody('{ "enable_thinking": true, "top_p": 0.7 }')), "{\"enable_thinking\":true,\"top_p\":0.7}", "extra body editor parses JSON object");
let extraBodyRejected = false;
try {
  parseProviderExtraBody("[true]");
} catch {
  extraBodyRejected = true;
}
ok(extraBodyRejected, "extra body editor rejects non-object JSON");
const extraBodyTestT = ((key: string, vars?: Record<string, string | number>) => {
  if (key === "settings.providerExtraBodyError") return "localized extra body fallback";
  if (key === "settings.providerExtraBodyNull") return `${vars?.path} localized null`;
  return key;
}) as any;
eq(
  providerExtraBodyParseError(new SyntaxError("Unexpected token } in JSON"), extraBodyTestT),
  "localized extra body fallback",
  "extra body editor localizes JSON syntax errors",
);
try {
  parseProviderExtraBody('{ "nested": { "value": null } }', extraBodyTestT);
  ok(false, "extra body editor rejects localized null validation errors");
} catch (e) {
  eq(
    providerExtraBodyParseError(e, extraBodyTestT),
    "extra_body.nested.value localized null",
    "extra body editor keeps localized structured validation errors",
  );
}

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
// React's legacy input-event fallback expects these IE hooks when JSDOM does
// not expose native input event support. The custom threshold editor focuses
// its input on open, so keep that production behavior testable without noise.
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });
installCanvasMock(dom.window as unknown as Window);
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;
globalThis.sessionStorage = dom.window.sessionStorage;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
window.scrollTo = () => {};
localStorage.clear();

const regionalTypography = createDefaultTypographyPreferences();
regionalTypography.code = {
  followGlobal: false,
  fontFamily: "jetbrains",
  customFontName: "",
  fontSize: 15,
};
applyTypographyPreferences(regionalTypography);
const regionalCodeFont = document.documentElement.style.getPropertyValue("--typography-code-font");

const settingsSnapshots = [baseSettings("standard"), baseSettings("compact")];
let settingsCalls = 0;
let setDisplayModeCalls = 0;
let onChangedSettings: SettingsView | undefined;

window.go = {
  main: {
    App: {
      Settings: async () => settingsSnapshots[Math.min(settingsCalls++, settingsSnapshots.length - 1)],
      SetDisplayMode: async () => {
        setDisplayModeCalls += 1;
      },
    } as Partial<AppBindings> as AppBindings,
  },
};

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(
    <LocaleProvider>
      <SettingsPanel
        initialTab="general"
        desktopPlatform="linux"
        onClose={() => {}}
        onChanged={(settings?: SettingsView) => {
          onChangedSettings = settings;
        }}
      />
    </LocaleProvider>,
  );
  await flushPromises();
});

const compactButton = Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.trim() === "Compact") as HTMLButtonElement | undefined;
if (!compactButton) throw new Error("compact display mode button did not render");
const generalFieldLabels = Array.from(rootEl.querySelectorAll(".settings-section__body > .settings-field .settings-field__label"))
  .map((label) => label.textContent?.trim());
eq(generalFieldLabels[0], "Desktop style", "general settings place desktop style first");
eq(document.querySelectorAll(".step-limit-control").length, 0, "general settings hide executor and planner step-limit controls");
ok(!document.body.textContent?.includes("step limit"), "general settings keep automatic progress free of step-limit copy");
ok(!document.body.textContent?.includes("Automatic plan mode"), "general settings omit the retired automatic Plan Mode control");
ok(!document.body.textContent?.includes("planning defaults"), "general settings omit retired automatic Plan Mode copy");

await act(async () => {
  compactButton.click();
  await flushPromises();
});

eq(setDisplayModeCalls, 1, "display mode mutation is invoked once");
eq(settingsCalls, 2, "settings panel reads Settings only for initial load and post-save reload");
ok(onChangedSettings?.displayMode === "compact", "onChanged receives the post-save SettingsView snapshot");

await act(async () => {
  root.unmount();
});

// Models > Agent runtime: the compaction preference is directly visible, shows
// the effective token threshold, and reloads the persisted Settings snapshot.
const compactRootEl = document.createElement("div");
document.body.appendChild(compactRootEl);
const compactRoot = createRoot(compactRootEl);
let compactSettings = baseSettings("standard");
delete compactSettings.agent.compactRatio; // Old backends omit the additive field.
compactSettings.agent.effectiveCompactRatio = 0.75;
compactSettings.agent.compactRatioOverridden = true;
compactSettings.defaultModel = "context-provider/context-model";
compactSettings.providers = [{
  name: "context-provider",
  builtIn: false,
  added: true,
  kind: "openai",
  baseUrl: "https://context.example.com/v1",
  chatUrl: "",
  models: ["context-model"],
  visionModels: [],
  visionModelsConfigured: false,
  modelsUrl: "",
  default: "context-model",
  apiKeyEnv: "",
  keySet: false,
  requiresKey: false,
  configured: true,
  balanceUrl: "",
  contextWindow: 100_000,
  reasoningProtocol: "",
  thinking: "",
  supportedEfforts: [],
  defaultEffort: "",
  modelOverrides: [],
}];
let compactRatioCalls: number[] = [];
window.go = {
  main: {
    App: {
      Settings: async () => compactSettings,
      FetchAllProviderModels: async () => ({}),
      SetCompactRatio: async (ratio: number) => {
        compactRatioCalls.push(ratio);
        compactSettings = { ...compactSettings, agent: { ...compactSettings.agent, compactRatio: ratio } };
      },
    } as Partial<AppBindings> as AppBindings,
  },
};

await act(async () => {
  compactRoot.render(
    <LocaleProvider>
      <SettingsPanel initialTab="models" desktopPlatform="linux" onClose={() => {}} onChanged={() => {}} />
    </LocaleProvider>,
  );
  await flushPromises();
});
ok(compactRootEl.textContent?.includes("Advanced context management") === false, "compaction preference has no redundant advanced disclosure");
ok(compactRootEl.textContent?.includes("Automatic compaction threshold") === true, "compaction preference is visible without expanding a disclosure");
ok(compactRootEl.textContent?.includes("80,000 tokens") === true, "compact ratio shows the default model token threshold");
ok(compactRootEl.textContent?.includes("Current threshold: 80% · Recommended") === true, "compact ratio summarizes the saved preset separately");
ok(compactRootEl.textContent?.includes("effective threshold is 75%") === true, "project override shows the active effective threshold");
ok(compactRootEl.querySelector('input[aria-label="Custom compaction threshold percentage"]') === null, "custom compact ratio editor stays hidden on the default path");
const recommendedCompactButton = compactRootEl.querySelector('button[aria-label="80% · Recommended"]') as HTMLButtonElement | null;
if (!recommendedCompactButton) throw new Error("recommended compaction preset did not render");
ok(recommendedCompactButton.getAttribute("aria-pressed") === "true", "saved compact ratio starts selected");
const customCompactButton = Array.from(compactRootEl.querySelectorAll("button")).find((button) => button.textContent?.includes("Custom threshold…")) as HTMLButtonElement | undefined;
if (!customCompactButton) throw new Error("custom compaction threshold option did not render");
ok(customCompactButton.closest(".compact-ratio-presets") === null, "custom compaction is a separate disclosure rather than a preset value");
ok(customCompactButton.hasAttribute("aria-pressed") === false, "custom disclosure does not announce a saved selection state");
await act(async () => {
  customCompactButton.click();
  await flushPromises();
});
let customCompactInput = compactRootEl.querySelector('input[aria-label="Custom compaction threshold percentage"]') as HTMLInputElement | null;
if (!customCompactInput) throw new Error("custom compaction threshold input did not open");
eq(customCompactInput.value, "80", "custom compaction threshold defaults older backends to 80 percent");
ok(compactRootEl.textContent?.includes("65%") === true, "custom compact ratio explains the lower guard rail");
ok(compactRootEl.textContent?.includes("85%") === true, "custom compact ratio explains the upper guard rail");
ok(document.activeElement === customCompactInput, "opening the custom compact ratio moves focus to its input");
ok(customCompactButton.getAttribute("aria-expanded") === "true", "custom compact ratio exposes its expanded state");
ok(recommendedCompactButton.getAttribute("aria-pressed") === "true", "opening custom editing preserves the saved preset selection");
const customCompactApply = Array.from(customCompactInput.closest(".compact-ratio-custom")?.querySelectorAll("button") ?? []).find((button) => button.textContent === "Apply") as HTMLButtonElement | undefined;
if (!customCompactApply) throw new Error("custom compaction threshold apply action did not render");
const inputValueSetter = Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")?.set;
const setCustomCompactInput = (input: HTMLInputElement, value: string) => {
  const previous = input.value;
  inputValueSetter?.call(input, value);
  (input as HTMLInputElement & { _valueTracker?: { setValue: (next: string) => void } })._valueTracker?.setValue(previous);
  input.dispatchEvent(new Event("input", { bubbles: true }));
  input.dispatchEvent(new Event("change", { bubbles: true }));
};
await act(async () => {
  setCustomCompactInput(customCompactInput, "64");
  await flushPromises();
});
ok(customCompactApply.disabled, "out-of-range custom compact ratio cannot be applied");
eq(compactRatioCalls.length, 0, "editing a custom compact ratio does not save eagerly");
await act(async () => {
  setCustomCompactInput(customCompactInput, "75");
  await flushPromises();
});
ok(!customCompactApply.disabled, "valid custom compact ratio enables explicit apply");
await act(async () => {
  customCompactApply.click();
  await flushPromises();
});
eq(compactRatioCalls.length, 1, "custom compact ratio mutation is invoked once after apply");
eq(compactRatioCalls[0], 0.75, "custom compact ratio converts percentage to fraction");
ok(compactRootEl.querySelector('input[aria-label="Custom compaction threshold percentage"]') === null, "successful custom compact ratio apply collapses the editor");
ok(compactRootEl.textContent?.includes("Current threshold: 75% · Custom") === true, "saved custom compact ratio is summarized independently from the disclosure");
ok(customCompactButton.textContent?.includes("Custom threshold…") === true, "custom disclosure keeps an action label after saving");
await act(async () => {
  customCompactButton.click();
  await flushPromises();
});
customCompactInput = compactRootEl.querySelector('input[aria-label="Custom compaction threshold percentage"]') as HTMLInputElement | null;
if (!customCompactInput) throw new Error("saved custom compaction threshold did not reopen");
await act(async () => {
  setCustomCompactInput(customCompactInput, "74");
  customCompactInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  await flushPromises();
});
eq(compactRatioCalls.length, 1, "Escape cancels a custom compact ratio without saving");
ok(compactRootEl.querySelector('input[aria-label="Custom compaction threshold percentage"]') === null, "Escape collapses the custom compact ratio editor");
await act(async () => {
  customCompactButton.click();
  await flushPromises();
});
customCompactInput = compactRootEl.querySelector('input[aria-label="Custom compaction threshold percentage"]') as HTMLInputElement | null;
if (!customCompactInput) throw new Error("custom compaction threshold did not reopen for cancel");
const customCompactCancel = Array.from(customCompactInput.closest(".compact-ratio-custom")?.querySelectorAll("button") ?? []).find((button) => button.textContent === "Cancel") as HTMLButtonElement | undefined;
if (!customCompactCancel) throw new Error("custom compaction threshold cancel action did not render");
await act(async () => {
  customCompactCancel.click();
  await flushPromises();
});
eq(compactRatioCalls.length, 1, "Cancel closes a custom compact ratio without saving");
ok(compactRootEl.querySelector('input[aria-label="Custom compaction threshold percentage"]') === null, "Cancel collapses the custom compact ratio editor");
const activeCompactButton = compactRootEl.querySelector('button[aria-label="70% · Active"]') as HTMLButtonElement | null;
if (!activeCompactButton) throw new Error("active compaction preset did not render");
await act(async () => {
  activeCompactButton.click();
  await flushPromises();
});
eq(compactRatioCalls.length, 2, "compact ratio preset adds one mutation");
eq(compactRatioCalls[1], 0.7, "compact ratio preset sends the expected fraction");
ok(activeCompactButton.getAttribute("aria-pressed") === "true", "saved compact ratio is selected after Settings reload");

await act(async () => {
  compactRoot.unmount();
});

const retryRootEl = document.createElement("div");
document.body.appendChild(retryRootEl);
const retryRoot = createRoot(retryRootEl);
let failingSettingsCalls = 0;
window.go = {
  main: {
    App: {
      Settings: async () => {
        failingSettingsCalls += 1;
        if (failingSettingsCalls === 1) throw new Error("/Users/example/.reasonix/settings.toml: permission denied");
        return baseSettings("standard");
      },
    } as Partial<AppBindings> as AppBindings,
  },
};

await act(async () => {
  retryRoot.render(
    <LocaleProvider>
      <SettingsPanel
        initialTab="general"
        desktopPlatform="linux"
        onClose={() => {}}
        onChanged={() => {}}
      />
    </LocaleProvider>,
  );
  await flushPromises();
});
await waitFor("settings load failure", () => Boolean(document.querySelector(".banner--error")));

ok(document.body.textContent?.includes("Settings could not be loaded.") === true, "failed initial settings load shows a visible error");
ok(document.body.textContent?.includes("Loading…") === false, "failed initial settings load stops showing the loading state");

const retryButton = Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.trim() === "Retry") as HTMLButtonElement | undefined;
if (!retryButton) throw new Error("settings retry button did not render");

await act(async () => {
  retryButton.click();
  await flushPromises();
});
await waitFor("settings retry success", () => Boolean(Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.trim() === "Compact")));

eq(failingSettingsCalls, 2, "settings retry calls Settings again");
ok(document.body.textContent?.includes("Settings could not be loaded.") === false, "settings retry clears the load error");

await act(async () => {
  retryRoot.unmount();
});

const windowsSandboxRootEl = document.createElement("div");
document.body.appendChild(windowsSandboxRootEl);
const windowsSandboxRoot = createRoot(windowsSandboxRootEl);
let windowsSetSandboxCalls = 0;
window.go = {
  main: {
    App: {
      // Deliberately return a stale enforce value: the Windows UI must still
      // render the effective immutable off state.
      Settings: async () => baseSettings("standard"),
      SetSandbox: async () => {
        windowsSetSandboxCalls += 1;
      },
    } as Partial<AppBindings> as AppBindings,
  },
};

await act(async () => {
  windowsSandboxRoot.render(
    <LocaleProvider>
      <SettingsPanel
        initialTab="sandbox"
        desktopPlatform="windows"
        onClose={() => {}}
        onChanged={() => {}}
      />
    </LocaleProvider>,
  );
  await flushPromises();
});
await waitFor("Windows Bash sandbox control", () => document.body.textContent?.includes("This setting is fixed to off.") === true);

const windowsBashSelect = Array.from(windowsSandboxRootEl.querySelectorAll("select")).find((select) =>
  Array.from(select.options).some((option) => option.value === "off"),
);
if (!windowsBashSelect) throw new Error("Windows Bash sandbox select did not render");
ok(windowsBashSelect.disabled, "Windows Bash sandbox selector is disabled");
eq(windowsBashSelect.value, "off", "Windows Bash sandbox selector is fixed to off");
ok(!Array.from(windowsBashSelect.options).some((option) => option.value === "enforce"), "Windows Bash sandbox selector omits enforce");
eq(windowsSetSandboxCalls, 0, "Windows immutable Bash sandbox state does not save enforce");

await act(async () => {
  windowsSandboxRoot.unmount();
});

const zoomRootEl = document.createElement("div");
document.body.appendChild(zoomRootEl);
const zoomRoot = createRoot(zoomRootEl);
let persistedZoom = 0.5;
const savedZoomFactors: number[] = [];
window.go = {
  main: {
    App: {
      Settings: async () => baseSettings("standard"),
      GetDesktopZoomFactor: async () => persistedZoom,
      SetDesktopZoomFactor: async (factor: number) => {
        persistedZoom = factor;
        savedZoomFactors.push(factor);
      },
    } as Partial<AppBindings> as AppBindings,
  },
};

localStorage.setItem("reasonix-zoom-restart", "1");
await act(async () => {
  zoomRoot.render(
    <LocaleProvider>
      <SettingsPanel
        initialTab="appearance"
        desktopPlatform="windows"
        onClose={() => {}}
        onChanged={() => {}}
      />
    </LocaleProvider>,
  );
  await flushPromises();
});
await waitFor("persisted display zoom sync", () => document.querySelector(".zoom-slider__value")?.textContent?.trim() === "50%");

const monoFontSelect = zoomRootEl.querySelector("select[aria-labelledby='appearance-mono-font-family-label']") as HTMLSelectElement | null;
if (!monoFontSelect) throw new Error("monospace font selector did not render");
await act(async () => {
  monoFontSelect.value = "custom";
  monoFontSelect.dispatchEvent(new Event("change", { bubbles: true }));
  await flushPromises();
});

const preservedTypography = getTypographyPreferences();
eq(preservedTypography.code.followGlobal, false, "global monospace changes preserve an explicit code-region override");
eq(preservedTypography.code.fontFamily, "jetbrains", "global monospace changes preserve the regional code font choice");
eq(
  document.documentElement.style.getPropertyValue("--typography-code-font"),
  regionalCodeFont,
  "global monospace changes keep the regional code font CSS variable",
);

const resetZoomButton = document.querySelector("button[aria-label='Reset display zoom to 100%']") as HTMLButtonElement | null;
if (!resetZoomButton) throw new Error("display zoom reset button did not render");
await act(async () => {
  resetZoomButton.click();
  await flushPromises();
});
await waitFor("display zoom reset", () => document.querySelector(".zoom-slider__value")?.textContent?.trim() === "100%");

eq(savedZoomFactors.at(-1), 1, "display zoom reset writes the default zoom factor");
eq(localStorage.getItem("reasonix-zoom-restart"), "1", "display zoom reset updates the local restart zoom cache");

await act(async () => {
  zoomRoot.unmount();
});

// Bots tab: direct four-channel bot manager.
const botsRootEl = document.createElement("div");
document.body.appendChild(botsRootEl);
const botsRoot = createRoot(botsRootEl);
const botsSettings = baseSettings("standard");
botsSettings.bot.connections = [
  {
    id: "conn-feishu-1",
    provider: "feishu",
    domain: "feishu",
    label: "kun",
    enabled: true,
    status: "connected",
    model: "",
    toolApprovalMode: "",
    workspaceRoot: "",
    credential: { appId: "cli_mock", appSecretEnv: "FEISHU_BOT_APP_SECRET", accountId: "", tokenEnv: "", secretSet: true },
    sessionMappings: [],
    lastError: "",
    createdAt: "",
	    updatedAt: "",
	    access: { enabled: true, allowAll: false, pairingEnabled: true, users: ["ou_mock_user_001"], groups: [], approvers: [], admins: [] },
	  },
	];
window.go = {
  main: {
    App: {
      Settings: async () => botsSettings,
    } as Partial<AppBindings> as AppBindings,
  },
};

await act(async () => {
  botsRoot.render(
    <LocaleProvider>
      <SettingsPanel initialTab="bots" desktopPlatform="linux" onClose={() => {}} onChanged={() => {}} />
    </LocaleProvider>,
  );
  await flushPromises();
});
await waitFor("bot channel manager", () => Boolean(document.querySelector(".bot-channel-manager")));

ok(!document.querySelector(".bot-overview-grid"), "bots tab does not render the removed entry overview");
ok(!document.getElementById("bot-mobile-remote"), "bots tab no longer renders the mobile remote entry card");
ok(!document.querySelector(".bot-channel-entry"), "bots tab no longer renders the Bot Channel entry panel");
ok(!document.getElementById("bot-step-access"), "bots tab omits the old global access step card");
ok(!document.getElementById("bot-step-behavior"), "bots tab omits global default behavior card");
eq(document.querySelectorAll(".bot-step-chip").length, 0, "hero no longer shows the old two-step chips");

eq(document.querySelectorAll(".bot-channel-tabs [role=\"tab\"]").length, 4, "bot manager uses four fixed channel tabs on the left");
ok(document.querySelector(".bot-channel-setup-card")?.textContent?.includes("Configure QQ") === true, "unconfigured QQ tab shows key setup on the right");
ok(document.body.textContent?.includes("Back to entry") === false, "bot manager does not show a return-to-entry action");

const feishuTab = Array.from(document.querySelectorAll(".bot-channel-tabs [role=\"tab\"]")).find((button) => button.textContent?.includes("Feishu")) as HTMLButtonElement | undefined;
if (!feishuTab) throw new Error("Feishu channel tab did not render");
await act(async () => {
  feishuTab.click();
  await flushPromises();
});
await waitFor("selected Feishu detail", () => Boolean(document.querySelector(".bot-channel-manager__detail .bot-detail-card")));

ok(Boolean(document.querySelector(".bot-channel-manager__detail .bot-detail-card")), "configured channel renders selected bot detail on the right");
ok(Boolean(document.querySelector(".bot-channel-manager__detail .bot-detail-section--access")), "selected bot detail owns its access control");
ok(document.body.textContent?.includes("Access control") === true, "selected bot detail labels per-bot access control");
const selectedBotDetailText = document.querySelector(".bot-channel-manager__detail .bot-detail-card")?.textContent ?? "";
const connectionSummaryIndex = selectedBotDetailText.indexOf("Connection summary");
const enableBotIndex = selectedBotDetailText.indexOf("Enable bot");
const toolApprovalIndex = selectedBotDetailText.indexOf("Tool approval");
const modelIndex = selectedBotDetailText.indexOf("Model");
const accessControlIndex = selectedBotDetailText.indexOf("Access control");
ok(
  connectionSummaryIndex >= 0 && enableBotIndex > connectionSummaryIndex && toolApprovalIndex > enableBotIndex && modelIndex > toolApprovalIndex && accessControlIndex > modelIndex,
  "selected bot detail places enable, approval, and model controls between summary and access control",
);
ok(document.body.textContent?.includes("ou_mock_user_001") === true, "selected bot detail shows its trusted user");
ok(document.body.textContent?.includes("Legacy global allowlist") === true, "advanced area keeps the legacy global allowlist");
ok(document.querySelector(".bot-simple-advanced")?.textContent?.includes("local control API") === false, "advanced area no longer owns mobile/control API setup");

await act(async () => {
  botsRoot.unmount();
});

// Models tab: switching away invalidates an in-flight background discovery so
// its older completion cannot attempt a stale catalog write.
sessionStorage.clear();
const providerRaceRootEl = document.createElement("div");
document.body.appendChild(providerRaceRootEl);
const providerRaceRoot = createRoot(providerRaceRootEl);
const providerRaceSettings = baseSettings("standard");
providerRaceSettings.defaultModel = "race-provider/old-model";
providerRaceSettings.providers = [{
  name: "race-provider",
  builtIn: false,
  added: true,
  kind: "openai",
  baseUrl: "https://old.example.com/v1",
  chatUrl: "",
  models: ["old-model"],
  visionModels: [],
  visionModelsConfigured: false,
  modelsUrl: "",
  default: "missing-default",
  apiKeyEnv: "RACE_PROVIDER_API_KEY",
  headers: { "X-Gateway-Token": "private-gateway-secret" },
  extraBody: {},
  authHeader: false,
  keySet: true,
  requiresKey: true,
  configured: true,
  keySource: "global",
  keySourcePath: "",
  balanceUrl: "",
  contextWindow: 128_000,
  reasoningProtocol: "",
  thinking: "",
  supportedEfforts: [],
  defaultEffort: "",
  modelOverrides: [],
  modelCatalogFingerprint: "old-fingerprint",
}];
let resolveProviderBatch: ((models: Record<string, string[]>) => void) | undefined;
const providerBatch = new Promise<Record<string, string[]>>((resolve) => {
  resolveProviderBatch = resolve;
});
let providerBatchCalls = 0;
let providerCatalogSaveCalls = 0;
window.go = {
  main: {
    App: {
      Settings: async () => providerRaceSettings,
      FetchAllProviderModels: async () => {
        providerBatchCalls += 1;
        return providerBatch;
      },
      SaveProviderModelCatalogs: async () => {
        providerCatalogSaveCalls += 1;
        return ["race-provider"];
      },
    } as Partial<AppBindings> as AppBindings,
  },
};

await act(async () => {
  providerRaceRoot.render(
    <LocaleProvider>
      <SettingsPanel initialTab="models" desktopPlatform="linux" onClose={() => {}} onChanged={() => {}} />
    </LocaleProvider>,
  );
  await flushPromises();
});
await waitFor("provider background discovery", () => providerBatchCalls === 1);
const providerRefreshStorageKeys = Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.key(index) ?? "");
ok(providerRefreshStorageKeys.some((key) => key.includes("old-fingerprint")), "provider auto-refresh cooldown uses the opaque catalog fingerprint");
ok(providerRefreshStorageKeys.every((key) => !key.includes("private-gateway-secret")), "provider auto-refresh cooldown does not persist header secrets");
const accessModelsButton = Array.from(providerRaceRootEl.querySelectorAll(".settings-subtab")).find(
  (button) => button.textContent?.trim() === "Access",
) as HTMLButtonElement | undefined;
if (!accessModelsButton) throw new Error("provider Access subtab did not render");
await act(async () => {
  accessModelsButton.click();
  await flushPromises();
});
await act(async () => {
  resolveProviderBatch?.({ "race-provider": ["old-model", "stale-fetched-model"] });
  await flushPromises();
});
await waitFor("stale provider discovery completion", () => providerBatchCalls === 1);
eq(providerCatalogSaveCalls, 0, "leaving the models usage tab suppresses the stale background catalog write");

await act(async () => {
  providerRaceRoot.unmount();
});

// Cancelling a freshly fetched model catalog must also clear the success copy
// that asks the user to confirm and save that now-hidden draft.
const providerRefreshCancelRootEl = document.createElement("div");
document.body.appendChild(providerRefreshCancelRootEl);
const providerRefreshCancelRoot = createRoot(providerRefreshCancelRootEl);
const providerRefreshCancelSettings = baseSettings("standard");
providerRefreshCancelSettings.defaultModel = "deepseek/deepseek-v4-flash";
providerRefreshCancelSettings.providers = [{
  name: "deepseek",
  builtIn: true,
  added: true,
  kind: "anthropic",
  baseUrl: "https://api.deepseek.com/anthropic",
  chatUrl: "",
  models: ["deepseek-v4-flash"],
  visionModels: [],
  visionModelsConfigured: true,
  visionCapability: "unsupported",
  modelsUrl: "https://api.deepseek.com/models",
  default: "deepseek-v4-flash",
  apiKeyEnv: "DEEPSEEK_API_KEY",
  keySet: true,
  requiresKey: true,
  configured: true,
  balanceUrl: "https://api.deepseek.com/user/balance",
  contextWindow: 1_000_000,
  reasoningProtocol: "",
  thinking: "enabled",
  webSearch: true,
  serverWebSearchCapability: true,
  supportedEfforts: [],
  defaultEffort: "",
}];
window.go = {
  main: {
    App: {
      Settings: async () => providerRefreshCancelSettings,
      FetchAllProviderModels: async () => ({}),
      FetchProviderModels: async () => ["deepseek-v4-flash", "deepseek-v4-pro"],
    } as Partial<AppBindings> as AppBindings,
  },
};

await act(async () => {
  providerRefreshCancelRoot.render(
    <LocaleProvider>
      <SettingsPanel initialTab="models" desktopPlatform="linux" onClose={() => {}} onChanged={() => {}} />
    </LocaleProvider>,
  );
  await flushPromises();
});
const providerRefreshCancelAccessButton = Array.from(providerRefreshCancelRootEl.querySelectorAll(".settings-subtab")).find(
  (button) => button.textContent?.trim() === "Access",
) as HTMLButtonElement | undefined;
if (!providerRefreshCancelAccessButton) throw new Error("provider refresh cancel Access subtab did not render");
await act(async () => {
  providerRefreshCancelAccessButton.click();
  await flushPromises();
});
const providerRefreshCancelButton = Array.from(providerRefreshCancelRootEl.querySelectorAll("button")).find(
  (button) => button.textContent?.trim() === "Refresh models",
) as HTMLButtonElement | undefined;
if (!providerRefreshCancelButton) throw new Error("provider refresh action did not render");
await act(async () => {
  providerRefreshCancelButton.click();
  await flushPromises();
});
await waitFor(
  "provider model draft",
  () => providerRefreshCancelRootEl.textContent?.includes("Found 2 models for DeepSeek Official. Review and save the enabled list.") === true,
);
const providerModelDraftCancelButton = providerRefreshCancelRootEl.querySelector<HTMLButtonElement>(
  ".provider-model-draft__actions button",
);
if (!providerModelDraftCancelButton) throw new Error("provider model draft cancel action did not render");
await act(async () => {
  providerModelDraftCancelButton.click();
  await flushPromises();
});
ok(
  providerRefreshCancelRootEl.textContent?.includes("Found 2 models for DeepSeek Official. Review and save the enabled list.") === false,
  "cancelling a provider model draft clears its stale save instruction",
);
ok(
  providerRefreshCancelRootEl.querySelector(".provider-model-draft") === null,
  "cancelling a provider model draft closes the candidate editor",
);
await act(async () => {
  providerRefreshCancelRoot.unmount();
});

// A settings mutation may persist before a workspace-specific runtime rebuild
// fails. The panel must re-read the authoritative snapshot on that error so an
// already-completed protocol upgrade is not offered again.
const upgradeFailureRootEl = document.createElement("div");
document.body.appendChild(upgradeFailureRootEl);
const upgradeFailureRoot = createRoot(upgradeFailureRootEl);
let upgradeFailureSettings = baseSettings("standard");
upgradeFailureSettings.defaultModel = "deepseek/deepseek-v4-flash";
upgradeFailureSettings.providers = [{
  name: "deepseek-flash",
  builtIn: true,
  added: true,
  kind: "openai",
  baseUrl: "https://api.deepseek.com",
  chatUrl: "",
  models: ["deepseek-v4-flash"],
  visionModels: [],
  visionModelsConfigured: true,
  visionCapability: "unsupported",
  modelsUrl: "https://api.deepseek.com/models",
  default: "deepseek-v4-flash",
  apiKeyEnv: "DEEPSEEK_API_KEY",
  keySet: true,
  requiresKey: true,
  configured: true,
  balanceUrl: "https://api.deepseek.com/user/balance",
  contextWindow: 1_000_000,
  reasoningProtocol: "deepseek",
  thinking: "enabled",
  webSearch: false,
  serverWebSearchCapability: false,
  supportedEfforts: ["low", "high", "max"],
  defaultEffort: "high",
  recommendedUpgradeAvailable: true,
}];
let upgradeFailureSettingsCalls = 0;
let upgradeFailureMutationCalls = 0;
let upgradeFailureChanged: SettingsView | undefined;
window.go = {
  main: {
    App: {
      Settings: async () => {
        upgradeFailureSettingsCalls += 1;
        return upgradeFailureSettings;
      },
      FetchAllProviderModels: async () => ({}),
      UpgradeDeepSeekProviderAccess: async () => {
        upgradeFailureMutationCalls += 1;
        upgradeFailureSettings = {
          ...upgradeFailureSettings,
          providers: upgradeFailureSettings.providers.map((provider) => ({
            ...provider,
            kind: "anthropic",
            baseUrl: "https://api.deepseek.com/anthropic",
            webSearch: true,
            serverWebSearchCapability: true,
            recommendedUpgradeAvailable: false,
          })),
        };
        throw new Error("workspace runtime boot failed after protocol upgrade");
      },
    } as Partial<AppBindings> as AppBindings,
  },
};

await act(async () => {
  upgradeFailureRoot.render(
    <LocaleProvider>
      <SettingsPanel
        initialTab="models"
        desktopPlatform="linux"
        onClose={() => {}}
        onChanged={(settings?: SettingsView) => {
          upgradeFailureChanged = settings;
        }}
      />
    </LocaleProvider>,
  );
  await flushPromises();
});
const upgradeFailureAccessButton = Array.from(upgradeFailureRootEl.querySelectorAll(".settings-subtab")).find(
  (button) => button.textContent?.trim() === "Access",
) as HTMLButtonElement | undefined;
if (!upgradeFailureAccessButton) throw new Error("upgrade failure Access subtab did not render");
await act(async () => {
  upgradeFailureAccessButton.click();
  await flushPromises();
});
await waitFor(
  "legacy DeepSeek protocol upgrade action",
  () => upgradeFailureRootEl.textContent?.includes("Upgrade to recommended protocol") === true,
);
let upgradeFailureButton = Array.from(upgradeFailureRootEl.querySelectorAll("button")).find(
  (button) => button.textContent?.includes("Upgrade to recommended protocol"),
) as HTMLButtonElement | undefined;
if (!upgradeFailureButton) throw new Error("DeepSeek protocol upgrade button did not render");
await act(async () => {
  upgradeFailureButton?.click();
  await flushPromises();
});
upgradeFailureButton = upgradeFailureRootEl.querySelector<HTMLButtonElement>(
  ".provider-protocol-upgrade .inline-confirm > button",
) ?? undefined;
if (upgradeFailureButton?.textContent?.trim() !== "Confirm") throw new Error("DeepSeek protocol upgrade confirmation did not render");
await act(async () => {
  upgradeFailureButton?.click();
  await flushPromises();
});
await waitFor("post-error settings reload", () => upgradeFailureSettingsCalls === 2);

eq(upgradeFailureMutationCalls, 1, "DeepSeek protocol upgrade mutation is invoked once");
ok(
  upgradeFailureRootEl.textContent?.includes("Upgrade to recommended protocol") === false,
  "persisted DeepSeek protocol upgrade disappears after a runtime refresh error",
);
ok(
  upgradeFailureRootEl.textContent?.includes("workspace runtime boot failed after protocol upgrade") === true,
  "post-mutation reload preserves the original runtime error",
);
ok(
  upgradeFailureChanged?.providers[0]?.kind === "anthropic",
  "onChanged receives the authoritative persisted protocol after a runtime error",
);

await act(async () => {
  upgradeFailureRoot.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
