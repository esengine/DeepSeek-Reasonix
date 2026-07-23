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
  providerBaseURLFromChatURL,
  providerChatURLPreview,
  providerEditorEffectiveKind,
} from "../components/SettingsPanel";
import { LocaleProvider } from "../lib/i18n";
import type { AppBindings } from "../lib/bridge";
import type { SettingsView } from "../lib/types";

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

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function installCanvasMock(win: Window) {
  Object.defineProperty(win.HTMLCanvasElement.prototype, "getContext", {
    configurable: true,
    value(type: string) {
      if (type !== "2d") return null;
      return {
        font: "",
        measureText: () => ({ width: 0 }),
      } as unknown as CanvasRenderingContext2D;
    },
  });
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    await act(async () => {
      await flushPromises();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

function baseSettings(displayMode: "standard" | "compact" = "standard"): SettingsView {
  return {
    defaultModel: "",
    plannerModel: "",
    subagentModel: "",
    subagentEffort: "",
    autoPlan: "off",
    providers: [],
    officialProviders: [],
    providerPresets: [],
    permissions: { mode: "ask", allow: [], ask: [], deny: [] },
    sandbox: { bash: "enforce", network: false, workspaceRoot: "", allowWrite: [], effectiveWorkspaceRoot: "/work", effectiveWriteRoots: ["/work"], shell: "auto" },
    network: { proxyMode: "auto", proxyUrl: "", noProxy: "", proxy: { type: "socks5", server: "", port: 0, username: "", password: "" } },
    agent: { temperature: 0, maxSteps: 0, plannerMaxSteps: 0, maxSubagentDepth: 2, maxSubagentConcurrency: 6, maxParallelWriters: 3, systemPrompt: "", coldResumePrune: true, reasoningLanguage: "auto" },
    bot: {
      enabled: false,
      model: "",
	      toolApprovalMode: "",
	      maxSteps: 0,
	      debounceMs: 0,
	      queueMode: "steer",
	      queueCap: 20,
	      queueDrop: "summarize",
	      ignoreSelfMessages: true,
	      selfUserIds: { qq: [], feishu: [], weixin: [] },
	      control: { enabled: false, addr: "127.0.0.1:37913", tokenEnv: "REASONIX_BOT_CONTROL_TOKEN" },
	      pairing: { enabled: true, requestTtlMinutes: 60, maxPendingPerPlatform: 3 },
	      routes: [],
	      allowlist: {
	        enabled: false,
	        allowAll: false,
	        qqUsers: [],
	        feishuUsers: [],
	        weixinUsers: [],
	        qqApprovers: [],
	        feishuApprovers: [],
	        weixinApprovers: [],
	        qqAdmins: [],
	        feishuAdmins: [],
	        weixinAdmins: [],
	        qqGroups: [],
	        feishuGroups: [],
	        weixinGroups: [],
	      },
      qq: {
        enabled: false,
        appId: "",
        appSecretEnv: "",
        secretSet: false,
        sandbox: false,
        model: "",
        toolApprovalMode: "ask",
        workspaceRoot: "",
        access: { enabled: true, allowAll: false, pairingEnabled: true, users: [], groups: [], approvers: [], admins: [] },
      },
      feishu: { enabled: false, domain: "feishu", appId: "", appSecretEnv: "", secretSet: false, verificationToken: "", mode: "webhook", webhookPort: 0, requireMention: false },
      weixin: { enabled: false, accountId: "", tokenEnv: "", tokenSet: false, apiBase: "" },
      connections: [],
    },
    desktopLanguage: "en",
    desktopLayoutStyle: "workbench",
    desktopTheme: "auto",
    desktopThemeStyle: "graphite",
    closeBehavior: "background",
    displayMode,
    statusBarStyle: "text",
    statusBarItems: ["model", "workspace", "git_branch", "cache", "balance"],
    defaultToolApprovalMode: "auto",
    checkUpdates: true,
    telemetry: true,
    metrics: true,
    configPath: "/tmp/reasonix/config.toml",
    providerKinds: [],
    autoApproveTools: false,
    bypass: false,
  };
}

console.log("\nsettings refresh snapshot");

eq(providerEditorEffectiveKind(true, "anthropic", ["anthropic", "openai"]), "anthropic", "new custom providers keep the selected Anthropic-compatible kind");
eq(providerEditorEffectiveKind(false, "anthropic", ["anthropic", "openai"]), "anthropic", "existing providers preserve their stored kind");
eq(providerChatURLPreview("https://proxy.example.com/v1", "", false), "https://proxy.example.com/v1/chat/completions", "base URL mode previews chat completions URL");
eq(providerChatURLPreview("", "https://proxy.example.com/custom/chat", true), "https://proxy.example.com/custom/chat", "full URL mode previews configured URL");
eq(providerBaseURLFromChatURL("https://proxy.example.com/v1/chat/completions"), "https://proxy.example.com/v1", "chat URL derives base URL for model discovery");
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
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
window.scrollTo = () => {};
localStorage.clear();

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
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
