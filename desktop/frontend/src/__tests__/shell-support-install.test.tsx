// Run: tsx src/__tests__/shell-support-install.test.tsx
//
// Sandbox settings shell support contract: Windows is detect-and-guide with an
// official manual download link, while macOS/Linux expose copy-only native
// package-manager guidance. No platform launches an installer from Settings.

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { SettingsPanel } from "../components/SettingsPanel";
import { gitForWindowsDownloadURL } from "../components/SettingsShellSupport";
import { LocaleProvider } from "../lib/i18n";
import type { AppBindings } from "../lib/bridge";
import type { SettingsView } from "../lib/types";
import { baseSettings, flushPromises, installCanvasMock, waitFor } from "../test-support/settingsTestFixtures";

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
  const same = actual === expected ||
    (Array.isArray(actual) && Array.isArray(expected) && JSON.stringify(actual) === JSON.stringify(expected));
  if (same) {
    ok(true, label);
  } else {
    ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
  }
}

console.log("\nshell support guidance");

const officialGitForWindowsURL = "https://git-scm.com/download/win";
eq(gitForWindowsDownloadURL(officialGitForWindowsURL), officialGitForWindowsURL,
  "accepts the exact official Git for Windows download URL");
eq(gitForWindowsDownloadURL("https://evil.example/?next=https://git-scm.com/download/win"), officialGitForWindowsURL,
  "rejects an official-looking URL embedded in an attacker-controlled query");
eq(gitForWindowsDownloadURL("https://git-scm.com.evil.example/download/win"), officialGitForWindowsURL,
  "rejects an attacker-controlled hostname with the official hostname as a prefix");
eq(gitForWindowsDownloadURL("http://git-scm.com/download/win"), officialGitForWindowsURL,
  "rejects a non-HTTPS download URL");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });
installCanvasMock(dom.window as unknown as Window);
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
const copiedCommands: string[] = [];
const openedURLs: string[] = [];
Object.defineProperty(dom.window.navigator, "clipboard", {
  configurable: true,
  value: { writeText: async (value: string) => { copiedCommands.push(value); } },
});
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
window.open = ((url?: string | URL) => {
  openedURLs.push(String(url));
  return null;
}) as typeof window.open;
localStorage.clear();

function windowsSettings(overrides: {
  shell?: string;
  gitBashAvailable?: boolean;
  reloadRequired?: boolean;
  manualUrl?: string;
}): SettingsView {
  const settings = baseSettings("standard");
  const gitBashAvailable = overrides.gitBashAvailable ?? false;
  settings.sandbox = {
    ...settings.sandbox,
    shell: overrides.shell ?? "auto",
    effectiveShell: "powershell",
    resolvedShell: overrides.reloadRequired ? "git-bash" : "powershell",
    shellReloadRequired: overrides.reloadRequired ?? false,
    shellCapabilities: [
      { id: "git-bash", variant: "git-for-windows", available: gitBashAvailable, ...(gitBashAvailable ? { path: "C:\\Program Files\\Git\\bin\\bash.exe", source: "standard-path" } : { reason: "not-installed" }) },
      { id: "powershell", available: true, path: "C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe", source: "standard-path" },
      { id: "pwsh", available: false, reason: "not-installed" },
    ],
    shellInstallAction: { id: "git-for-windows", mode: "manual", available: false, manualUrl: overrides.manualUrl ?? "https://git-scm.com/download/win" },
  };
  return settings;
}

// Scenario 1: Windows always exposes only the official manual link, even when
// the binding exists. Re-detection remains an explicit user action.
{
  const rootEl = document.createElement("div");
  document.body.appendChild(rootEl);
  const root = createRoot(rootEl);
  let installCalls = 0;
  let cancelCalls = 0;
  let reloadCalls = 0;
  let settingsCalls = 0;
  window.go = {
    main: {
      App: {
        Settings: async () => {
          settingsCalls += 1;
          return windowsSettings({ manualUrl: "https://evil.example/?next=https://git-scm.com/download/win" });
        },
        SetShellPreference: async () => {},
        InstallShellSupport: async () => {
          installCalls += 1;
          return { status: "manual_required", manualUrl: "https://git-scm.com/download/win" };
        },
        CancelShellInstall: async () => { cancelCalls += 1; },
        ReloadSettings: async () => { reloadCalls += 1; },
      } as Partial<AppBindings> as AppBindings,
    },
  };
  await act(async () => {
    root.render(
      <LocaleProvider>
        <SettingsPanel initialTab="sandbox" desktopPlatform="windows" onClose={() => {}} onChanged={() => {}} />
      </LocaleProvider>,
    );
    await flushPromises();
  });
  await waitFor("Windows manual card", () => rootEl.textContent?.includes("does not run the Git for Windows installer automatically") === true);
  ok(!Array.from(rootEl.querySelectorAll("button")).some((button) => button.textContent?.includes("Install Git for Windows")),
    "Windows renders no automatic install button");
  const manualLinkButton = rootEl.querySelector<HTMLButtonElement>(".shell-support__card .shell-support__actions button");
  ok(Boolean(manualLinkButton), "Windows offers the official Git for Windows download link");
  await act(async () => {
    manualLinkButton!.click();
    await flushPromises();
  });
  eq(openedURLs.at(-1), officialGitForWindowsURL,
    "Windows manual repair opens only the allowlisted official download URL");
  eq(installCalls, 0, "rendering Windows repair never calls InstallShellSupport");
  eq(cancelCalls, 0, "manual-only Windows repair never calls CancelShellInstall");

  const repairReloadButton = Array.from(rootEl.querySelectorAll("button")).find((button) => button.textContent?.includes("Re-detect and reload session"));
  ok(Boolean(repairReloadButton), "Windows manual repair offers explicit re-detection");
  await act(async () => {
    repairReloadButton!.click();
    await flushPromises();
  });
  eq(reloadCalls, 1, "Windows re-detection reloads only after the user requests it");
  eq(settingsCalls, 2, "Windows re-detection refreshes the Settings snapshot once");
  eq(installCalls, 0, "re-detection still never calls the install binding");
  await act(async () => { root.unmount(); });
}

// Scenario 2: Linux reports bash/zsh/sh, offers an allowlisted distro command
// for copying, and only re-detects after the user explicitly requests it.
{
  const rootEl = document.createElement("div");
  document.body.appendChild(rootEl);
  const root = createRoot(rootEl);
  const linuxSettings = baseSettings("standard");
  linuxSettings.sandbox = {
    ...linuxSettings.sandbox,
    shellCapabilities: [
      { id: "bash", variant: "system", available: false, reason: "not-found" },
      { id: "zsh", variant: "system", available: false, reason: "not-found" },
      { id: "sh", variant: "system", available: true, path: "/bin/sh", source: "standard-path" },
    ],
    gitCapability: { id: "git", available: true, path: "/usr/bin/git", source: "path" },
    shellInstallAction: null,
    shellRepairGuidance: { manager: "apt", command: "apt-get install bash" },
  };
  let reloadCalls = 0;
  window.go = {
    main: {
      App: {
        Settings: async () => linuxSettings,
        SetShellPreference: async () => {},
        InstallShellSupport: async () => ({ status: "unsupported_platform" }),
        CancelShellInstall: async () => {},
        ReloadSettings: async () => { reloadCalls += 1; },
      } as Partial<AppBindings> as AppBindings,
    },
  };
  await act(async () => {
    root.render(
      <LocaleProvider>
        <SettingsPanel initialTab="sandbox" desktopPlatform="linux" onClose={() => {}} onChanged={() => {}} />
      </LocaleProvider>,
    );
    await flushPromises();
  });
  await waitFor("Linux detection", () => rootEl.textContent?.includes("Bash") === true);
  ok(!Array.from(rootEl.querySelectorAll("button")).some((button) => button.textContent?.includes("Install Git for Windows")),
    "Linux never renders a Windows install entry");
  ok(rootEl.textContent?.includes("zsh") === true && rootEl.textContent?.includes("POSIX sh") === true,
    "Linux detection reports zsh and POSIX sh alongside Bash");
  ok(rootEl.textContent?.includes("apt-get install bash") === true, "Linux missing Bash shows the distro repair command");
  ok(!rootEl.textContent?.includes("sudo apt-get") && !rootEl.textContent?.includes("sudo"),
    "Linux repair guidance never prescribes sudo");
  const copyButton = Array.from(rootEl.querySelectorAll("button")).find((button) => button.textContent?.includes("Copy command"));
  ok(Boolean(copyButton), "Linux repair command is copyable");
  await act(async () => {
    copyButton!.click();
    await flushPromises();
  });
  eq(copiedCommands.at(-1), "apt-get install bash", "copy action writes the exact allowlisted command");
  const repairReloadButton = Array.from(rootEl.querySelectorAll("button")).find((button) => button.textContent?.includes("Re-detect and reload session"));
  ok(Boolean(repairReloadButton), "Linux manual repair offers explicit re-detection");
  await act(async () => {
    repairReloadButton!.click();
    await flushPromises();
  });
  eq(reloadCalls, 1, "Linux repair reload remains an explicit user action");
  await act(async () => { root.unmount(); });
}

// Scenario 3: macOS falls back to zsh when Bash is missing, while Git remains
// a separate capability with its own copy-only Homebrew repair command.
{
  const rootEl = document.createElement("div");
  document.body.appendChild(rootEl);
  const root = createRoot(rootEl);
  const macSettings = baseSettings("standard");
  macSettings.sandbox = {
    ...macSettings.sandbox,
    effectiveShell: "zsh",
    resolvedShell: "zsh",
    shellCapabilities: [
      { id: "bash", variant: "system", available: false, reason: "not-found" },
      { id: "zsh", variant: "system", available: true, path: "/bin/zsh", source: "standard-path" },
      { id: "sh", variant: "system", available: true, path: "/bin/sh", source: "standard-path" },
    ],
    gitCapability: { id: "git", available: false, reason: "not-found" },
    shellInstallAction: null,
    shellRepairGuidance: null,
    gitRepairGuidance: { manager: "homebrew", command: "brew install git" },
  };
  window.go = {
    main: {
      App: {
        Settings: async () => macSettings,
        SetShellPreference: async () => {},
        InstallShellSupport: async () => ({ status: "unsupported_platform" }),
        CancelShellInstall: async () => {},
        ReloadSettings: async () => {},
      } as Partial<AppBindings> as AppBindings,
    },
  };
  await act(async () => {
    root.render(
      <LocaleProvider>
        <SettingsPanel initialTab="sandbox" desktopPlatform="darwin" onClose={() => {}} onChanged={() => {}} />
      </LocaleProvider>,
    );
    await flushPromises();
  });
  await waitFor("macOS shell inventory", () => rootEl.textContent?.includes("POSIX sh") === true);
  ok(rootEl.textContent?.includes("Bash") === true && rootEl.textContent?.includes("zsh") === true,
    "macOS detection reports Bash and zsh");
  ok(!rootEl.textContent?.includes("brew install bash") && !rootEl.textContent?.includes("Bash is not detected"),
    "macOS native zsh fallback does not request a Bash install");
  ok(rootEl.textContent?.includes("Git") === true && rootEl.textContent?.includes("brew install git") === true,
    "macOS missing Git shows an independent Homebrew Git repair command");
  const gitCopyButton = Array.from(rootEl.querySelectorAll("button")).find((button) => button.textContent?.includes("Copy command"));
  await act(async () => {
    gitCopyButton!.click();
    await flushPromises();
  });
  eq(copiedCommands.at(-1), "brew install git", "macOS Git repair copies brew install git only");
  ok(!Array.from(rootEl.querySelectorAll("button")).some((button) => button.textContent?.includes("Install Git for Windows")),
    "macOS never renders the Windows install entry");
  await act(async () => { root.unmount(); });
}

if (failed > 0) {
  console.error(`\n${failed} failed, ${passed} passed`);
  process.exit(1);
}
console.log(`\n${passed} passed, 0 failed`);
