// Run: tsx src/__tests__/shell-support-install.test.tsx
//
// Sandbox settings shell support contract: the Windows-only Git for Windows
// install entry, its race with a concurrent shell preference change, and the
// macOS/Linux detect-and-guide behavior (no install surface).

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { SettingsPanel } from "../components/SettingsPanel";
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

console.log("\nshell support install");

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
localStorage.clear();

function windowsSettings(overrides: {
  shell?: string;
  gitBashAvailable?: boolean;
  installMode?: "winget-user" | "manual";
  reloadRequired?: boolean;
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
    shellInstallAction: overrides.installMode === "manual"
      ? { id: "git-for-windows", mode: "manual", available: false, manualUrl: "https://git-scm.com/download/win" }
      : { id: "git-for-windows", mode: "winget-user", available: true },
  };
  return settings;
}

// Scenario 1: Windows with winget offers the install entry; a completed
// install refreshes detection only and never overwrites a shell preference
// the user picked while the install was running.
{
  const rootEl = document.createElement("div");
  document.body.appendChild(rootEl);
  const root = createRoot(rootEl);

  let settingsState = windowsSettings({ shell: "auto" });
  const shellPreferenceCalls: string[] = [];
  const installCalls: string[] = [];
  const cancelCalls: number[] = [];
  let reloadCalls = 0;
  let settingsCalls = 0;
  let resolveInstall: ((value: { status: string; path?: string }) => void) | null = null;
  // The panel only re-fetches Settings through apply()/reload, so the mock
  // applies this state on the next ReloadSettings call.
  let stateAfterReload = windowsSettings({ shell: "powershell", gitBashAvailable: true });

  window.go = {
    main: {
      App: {
        Settings: async () => {
          settingsCalls += 1;
          return settingsState;
        },
        SetShellPreference: async (prefer: string) => {
          shellPreferenceCalls.push(prefer);
          settingsState = windowsSettings({ shell: prefer, gitBashAvailable: false });
        },
        InstallShellSupport: (id: string) => {
          installCalls.push(id);
          return new Promise((resolve) => { resolveInstall = resolve; });
        },
        CancelShellInstall: async () => {
          cancelCalls.push(cancelCalls.length);
        },
        ReloadSettings: async () => {
          reloadCalls += 1;
          // Post-install refresh: detection now sees Git Bash. The preference
          // (powershell) and the session shell are untouched by the installer.
          settingsState = stateAfterReload;
        },
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
  await waitFor("install card", () => document.body.textContent?.includes("Install Git for Windows") === true);

  const installButton = Array.from(rootEl.querySelectorAll("button")).find((button) => button.textContent?.includes("Install Git for Windows"));
  ok(Boolean(installButton), "Windows without Git Bash renders the install entry");

  await act(async () => {
    installButton!.click();
    await flushPromises();
  });
  await waitFor("install running", () => document.body.textContent?.includes("Installing…") === true);
  eq(installCalls, ["git-for-windows"], "install entry invokes InstallShellSupport with the action id");

  // While the install is in flight the user switches to PowerShell; the
  // select must stay enabled and record the explicit choice.
  const shellSelect = Array.from(rootEl.querySelectorAll("select")).find((select) =>
    Array.from(select.options).some((option) => option.value === "pwsh"),
  ) as HTMLSelectElement;
  ok(shellSelect != null && !shellSelect.disabled, "shell preference select stays enabled during an install");
  await act(async () => {
    shellSelect.value = "powershell";
    shellSelect.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
    await flushPromises();
  });
  eq(shellPreferenceCalls, ["powershell"], "explicit shell switch goes through SetShellPreference");
  eq((rootEl.querySelector("select") as HTMLSelectElement) !== null, true, "shell select still rendered after switch");

  // Install completes late: detection refreshes, but the preference the user
  // just chose must survive the late result.
  const settingsCallsBeforeInstallCompletion = settingsCalls;
  settingsState = windowsSettings({ shell: "powershell", gitBashAvailable: true });
  await act(async () => {
    resolveInstall!({ status: "installed", path: "C:\\Program Files\\Git\\bin\\bash.exe" });
    await flushPromises();
  });
  await waitFor("install note", () => document.body.textContent?.includes("Shell detection has been refreshed.") === true);
  eq(settingsCalls, settingsCallsBeforeInstallCompletion + 1, "a successful install re-reads detection once");
  eq(reloadCalls, 0, "a successful install does not rebuild the current session");
  eq(shellPreferenceCalls, ["powershell"], "late install completion does not touch the shell preference");
  const shellSelectAfter = Array.from(rootEl.querySelectorAll("select")).find((select) =>
    Array.from(select.options).some((option) => option.value === "pwsh"),
  ) as HTMLSelectElement;
  eq(shellSelectAfter?.value, "powershell", "shell select still shows the user's explicit PowerShell choice");

  // Scenario 2: cancel during a run reaches the backend and stays idempotent.
  // Reset detection to "Git Bash missing" via the section's reload control so
  // the install card renders again.
  stateAfterReload = windowsSettings({ shell: "powershell", gitBashAvailable: false });
  await act(async () => {
    const reloadButton = Array.from(rootEl.querySelectorAll("button")).find((button) => button.textContent?.includes("Apply manual config changes"));
    reloadButton!.click();
    await flushPromises();
  });
  await waitFor("install card again", () => document.body.textContent?.includes("Install Git for Windows") === true);
  const installButton2 = Array.from(rootEl.querySelectorAll("button")).find((button) => button.textContent?.includes("Install Git for Windows"));
  await act(async () => {
    installButton2!.click();
    await flushPromises();
  });
  await waitFor("second install running", () => document.body.textContent?.includes("Installing…") === true);
  const cancelButton = Array.from(rootEl.querySelectorAll("button")).find((button) => button.textContent?.includes("Cancel"));
  ok(Boolean(cancelButton), "a running install offers cancellation");
  await act(async () => {
    cancelButton!.click();
    resolveInstall!({ status: "cancelled" });
    await flushPromises();
  });
  await waitFor("cancelled note", () => document.body.textContent?.includes("Installation cancelled.") === true);
  eq(cancelCalls.length, 1, "cancel button calls CancelShellInstall once");

  await act(async () => {
    root.unmount();
  });

  window.go = undefined as unknown as typeof window.go;
}

// Scenario 3: a genuinely late result after unmount is ignored before it can
// refresh settings or rebuild the current session.
{
  const rootEl = document.createElement("div");
  document.body.appendChild(rootEl);
  const root = createRoot(rootEl);
  let settingsCalls = 0;
  let reloadCalls = 0;
  let resolveInstall: ((value: { status: string; path?: string }) => void) | null = null;
  window.go = {
    main: {
      App: {
        Settings: async () => {
          settingsCalls += 1;
          return windowsSettings({ shell: "auto" });
        },
        SetShellPreference: async () => {},
        InstallShellSupport: () => new Promise((resolve) => { resolveInstall = resolve; }),
        CancelShellInstall: async () => {},
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
  await waitFor("late-result install card", () => rootEl.textContent?.includes("Install Git for Windows") === true);
  const installButton = Array.from(rootEl.querySelectorAll("button")).find((button) => button.textContent?.includes("Install Git for Windows"));
  await act(async () => {
    installButton!.click();
    await flushPromises();
  });
  await waitFor("late-result install running", () => rootEl.textContent?.includes("Installing…") === true);
  const settingsCallsBeforeUnmount = settingsCalls;
  await act(async () => { root.unmount(); });
  await act(async () => {
    resolveInstall!({ status: "installed", path: "C:\\Program Files\\Git\\bin\\bash.exe" });
    await flushPromises();
  });
  eq(settingsCalls, settingsCallsBeforeUnmount, "unmounted panel drops late install result before refreshing settings");
  eq(reloadCalls, 0, "unmounted panel never rebuilds the current session");
}

// Scenario 4: Windows without winget offers only the manual download link.
{
  const rootEl = document.createElement("div");
  document.body.appendChild(rootEl);
  const root = createRoot(rootEl);
  window.go = {
    main: {
      App: {
        Settings: async () => windowsSettings({ installMode: "manual" }),
        SetShellPreference: async () => {},
        InstallShellSupport: async () => ({ status: "failed", reason: "unexpected" }),
        CancelShellInstall: async () => {},
        ReloadSettings: async () => {},
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
  await waitFor("manual card", () => document.body.textContent?.includes("winget (App Installer) is unavailable") === true);
  ok(!Array.from(rootEl.querySelectorAll("button")).some((button) => button.textContent?.includes("Install Git for Windows")),
    "manual mode renders no install button");
  ok(Array.from(rootEl.querySelectorAll("button")).some((button) => button.textContent?.includes("git-scm.com")),
    "manual mode offers the official download link");
  await act(async () => {
    root.unmount();
  });
}

// Scenario 5: Linux never renders an install entry. It reports bash/zsh/sh,
// offers an allowlisted distro command for copying, and only re-detects after
// the user explicitly requests a session reload.
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
  await waitFor("linux detection", () => document.body.textContent?.includes("Bash") === true);
  ok(!Array.from(rootEl.querySelectorAll("button")).some((button) => button.textContent?.includes("Install Git for Windows")),
    "Linux never renders the Windows install entry");
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
  ok(Boolean(repairReloadButton), "manual repair offers an explicit re-detect and reload action");
  await act(async () => {
    repairReloadButton!.click();
    await flushPromises();
  });
  eq(reloadCalls, 1, "manual repair reload remains an explicit user action");
  await act(async () => {
    root.unmount();
  });
}

// Scenario 6: macOS reports Bash, zsh, and sh but does not show package-manager
// guidance while Bash is already available.
{
  const rootEl = document.createElement("div");
  document.body.appendChild(rootEl);
  const root = createRoot(rootEl);
  const macSettings = baseSettings("standard");
  macSettings.sandbox = {
    ...macSettings.sandbox,
    shellCapabilities: [
      { id: "bash", variant: "system", available: true, path: "/bin/bash", source: "standard-path" },
      { id: "zsh", variant: "system", available: true, path: "/bin/zsh", source: "standard-path" },
      { id: "sh", variant: "system", available: true, path: "/bin/sh", source: "standard-path" },
    ],
    shellInstallAction: null,
    shellRepairGuidance: { manager: "homebrew", command: "brew install bash" },
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
  ok(!rootEl.textContent?.includes("brew install bash"), "macOS hides repair guidance while Bash is available");
  ok(!Array.from(rootEl.querySelectorAll("button")).some((button) => button.textContent?.includes("Install Git for Windows")),
    "macOS never renders the Windows install entry");
  await act(async () => {
    root.unmount();
  });
}

if (failed > 0) {
  console.error(`\n${failed} failed, ${passed} passed`);
  process.exit(1);
}
console.log(`\n${passed} passed, 0 failed`);
