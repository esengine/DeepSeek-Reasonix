// Run: tsx src/__tests__/settings-refresh-snapshot.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import {
  SettingsPanel,
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

function setInputValue(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, "value")?.set;
  setter?.call(input, value);
  const reactPropsKey = Object.keys(input).find((key) => key.startsWith("__reactProps$"));
  const reactProps = reactPropsKey
    ? (input as unknown as Record<string, { onChange?: (event: { target: HTMLInputElement; currentTarget: HTMLInputElement }) => void }>)[reactPropsKey]
    : undefined;
  reactProps?.onChange?.({ target: input, currentTarget: input });
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
    permissions: { mode: "ask", allow: [], ask: [], deny: [] },
    sandbox: { bash: "enforce", network: false, workspaceRoot: "", allowWrite: [], shell: "auto" },
    network: { proxyMode: "auto", proxyUrl: "", noProxy: "", proxy: { type: "socks5", server: "", port: 0, username: "", password: "" } },
    agent: { temperature: 0, maxSteps: 0, plannerMaxSteps: 0, systemPrompt: "", coldResumePrune: true, reasoningLanguage: "auto" },
    bot: {
      enabled: false,
      model: "",
      toolApprovalMode: "",
      maxSteps: 0,
      debounceMs: 0,
      allowlist: { enabled: false, allowAll: false, qqUsers: [], feishuUsers: [], weixinUsers: [], qqGroups: [], feishuGroups: [], weixinGroups: [] },
      qq: { enabled: false, appId: "", appSecretEnv: "", secretSet: false, sandbox: false },
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
    defaultToolApprovalMode: "ask",
    checkUpdates: true,
    telemetry: true,
    metrics: true,
    memoryCompilerEnabled: true,
    configPath: "/tmp/reasonix/config.toml",
    providerKinds: [],
    autoApproveTools: false,
    bypass: false,
  };
}

function customProviderSettings(): SettingsView {
  return {
    ...baseSettings("standard"),
    providerKinds: ["openai"],
  };
}

console.log("\nsettings refresh snapshot");

eq(providerEditorEffectiveKind(true, "anthropic", ["anthropic", "openai"]), "openai", "new custom providers ignore sorted providerKinds and default to OpenAI");
eq(providerEditorEffectiveKind(false, "anthropic", ["anthropic", "openai"]), "anthropic", "existing providers preserve their stored kind");
eq(providerChatURLPreview("https://proxy.example.com/v1", "", false), "https://proxy.example.com/v1/chat/completions", "base URL mode previews chat completions URL");
eq(providerChatURLPreview("", "https://proxy.example.com/custom/chat", true), "https://proxy.example.com/custom/chat", "full URL mode previews configured URL");
eq(providerBaseURLFromChatURL("https://proxy.example.com/v1/chat/completions"), "https://proxy.example.com/v1", "chat URL derives base URL for model discovery");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.HTMLInputElement = dom.window.HTMLInputElement;
globalThis.Event = dom.window.Event;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
window.scrollTo = () => {};

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

const keyRejectRootEl = document.createElement("div");
document.body.appendChild(keyRejectRootEl);
const keyRejectRoot = createRoot(keyRejectRootEl);
const activeWorkError = "finish or cancel the current turn, answer pending prompts, and stop background jobs before changing provider key";
let saveProviderCalls = 0;
let setProviderKeyCalls = 0;
const unhandledReasons: string[] = [];
const onUnhandledRejection = (reason: unknown) => {
  unhandledReasons.push(String((reason as Error)?.message ?? reason));
};
process.on("unhandledRejection", onUnhandledRejection);
window.go = {
  main: {
    App: {
      Settings: async () => customProviderSettings(),
      SetProviderKey: async () => {
        setProviderKeyCalls += 1;
        throw new Error(activeWorkError);
      },
      SaveProvider: async () => {
        saveProviderCalls += 1;
      },
    } as Partial<AppBindings> as AppBindings,
  },
};

try {
  await act(async () => {
    keyRejectRoot.render(
      <LocaleProvider>
        <SettingsPanel
          initialTab="models"
          onClose={() => {}}
          onChanged={() => {}}
          desktopPlatform="windows"
        />
      </LocaleProvider>,
    );
    await flushPromises();
  });
  await waitFor("provider access tab", () => Boolean(Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.trim() === "Access")));
  const accessButton = Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.trim() === "Access") as HTMLButtonElement | undefined;
  if (!accessButton) throw new Error("provider access button did not render");
  await act(async () => {
    accessButton.click();
    await flushPromises();
  });
  const customProviderButton = Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.trim() === "Custom provider") as HTMLButtonElement | undefined;
  if (!customProviderButton) throw new Error("custom provider button did not render");
  await act(async () => {
    customProviderButton.click();
    await flushPromises();
  });
  const editorInputs = Array.from(document.querySelectorAll(".provider-editor input.mem-input")) as HTMLInputElement[];
  const nameInput = editorInputs[0];
  const baseUrlInput = editorInputs.find((input) => input.placeholder.includes("base_url"));
  const keyInput = editorInputs.find((input) => input.type === "password");
  const modelsInput = editorInputs.find((input) => input.placeholder.includes("models"));
  if (!nameInput || !baseUrlInput || !keyInput || !modelsInput) throw new Error("custom provider editor inputs did not render");
  await act(async () => {
    setInputValue(nameInput, "custom-proxy");
    setInputValue(baseUrlInput, "https://proxy.example.com/v1");
    setInputValue(keyInput, "sk-test");
    setInputValue(modelsInput, "proxy-chat");
    await flushPromises();
  });
  const saveButton = document.querySelector(".provider-editor .prov-card__actions .btn--primary") as HTMLButtonElement | null;
  if (!saveButton) throw new Error("provider save button did not render");
  await act(async () => {
    saveButton.click();
    await flushPromises();
    await flushPromises();
  });
  ok(unhandledReasons.length === 0, "provider key save rejection is handled instead of becoming an unhandled rejection");
  ok(document.body.textContent?.includes(activeWorkError) === true, "provider key save rejection remains visible in settings");
  eq(setProviderKeyCalls, 1, "provider key save is attempted once");
  eq(saveProviderCalls, 0, "provider config is not saved after rejected key update");
} finally {
  process.off("unhandledRejection", onUnhandledRejection);
  await act(async () => {
    keyRejectRoot.unmount();
  });
}
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
