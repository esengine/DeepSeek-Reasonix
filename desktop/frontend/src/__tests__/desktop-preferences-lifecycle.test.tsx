import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useDesktopPreferences } from "../app-runtime/useDesktopPreferences";
import { getSessionExperience, hydrateSessionExperience } from "../lib/sessionExperience";
import { LocaleProvider } from "../lib/i18n";
import type { DesktopStartupSettingsView } from "../lib/types";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost", pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, localStorage: dom.window.localStorage,
  CustomEvent: dom.window.CustomEvent, IS_REACT_ACT_ENVIRONMENT: true });
window.matchMedia = (() => ({ matches: false, addEventListener() {}, removeEventListener() {} })) as typeof window.matchMedia;
const listeners = new Map<string, (...args: unknown[]) => void>();
let requests = 0;
let fullSettings = 0;
let resolveStartup!: (settings: DesktopStartupSettingsView) => void;
const startup = new Promise<DesktopStartupSettingsView>(resolve => { resolveStartup = resolve; });
Object.assign(window, {
  runtime: { EventsOn: (name: string, listener: (...args: unknown[]) => void) => {
    listeners.set(name, listener); return () => { listeners.delete(name); };
  } },
  go: { main: { App: {
    DesktopStartupSettings: () => { requests++; return startup; },
    Settings: () => { fullSettings++; throw new Error("startup must not request full Settings"); },
    BotRuntimeStatus: async () => null,
    SetTrayLocale: async () => {},
    GetThemeExperience: async () => ({ themeMode: "light", baseStyle: "graphite", effectiveStyle: "graphite" }),
  } } },
});
let current!: ReturnType<typeof useDesktopPreferences>;
function Probe() { current = useDesktopPreferences(); return <div>{current.configLoadWarnings.join("|")}</div>; }
const root = createRoot(document.getElementById("root")!);
const snapshot = { sessionExperience: "deep", desktopLayoutStyle: "creation", desktopTheme: "light", desktopThemeStyle: "graphite",
  desktopLanguage: "en", checkUpdates: true, configWarnings: ["warning"], configWarningsRevision: 3 } as DesktopStartupSettingsView;
try {
  localStorage.setItem("reasonix-process-fold", "auto");
  await act(async () => root.render(<LocaleProvider><Probe /></LocaleProvider>));
  assert.equal(requests, 1);
  await act(async () => { resolveStartup(snapshot); await import("../lib/themeExperience"); });
  assert.equal(current.desktopLayoutStyle, "creation");
  assert.equal(getSessionExperience(), "deep", "backend wins over an old localStorage mirror");
  assert.deepEqual(current.configLoadWarnings, ["warning"]);
  await act(async () => { listeners.get("config:load-warnings")?.(["stale"], 2); });
  assert.deepEqual(current.configLoadWarnings, ["warning"], "stale runtime warning cannot replace startup snapshot");
  await act(async () => { listeners.get("config:load-warnings")?.(["current"], 4); });
  assert.deepEqual(current.configLoadWarnings, ["current"]);
  await act(async () => { await current.reload({ ...snapshot, sessionExperience: undefined }); });
  assert.equal(getSessionExperience(), "standard", "old backend missing field resolves standard");
  assert.equal(fullSettings, 0, "preferences and IM projection never request full Settings");
  const oldReload = current.reload;
  await act(async () => root.unmount());
  assert.equal(listeners.size, 0);
  await oldReload(snapshot);
  assert.equal(getSessionExperience(), "standard", "disposed commands cannot mutate global experience");
  const originalWarn = console.warn;
  console.warn = () => {};
  const failedRoot = createRoot(document.getElementById("root")!);
  try {
    Object.assign(window.go!.main.App, { DesktopStartupSettings: async () => { throw new Error("offline"); } });
    hydrateSessionExperience("deep");
    await act(async () => failedRoot.render(<LocaleProvider><Probe /></LocaleProvider>));
    await act(async () => { await current.reload(); });
    assert.equal(getSessionExperience(), "standard", "failed first snapshot uses canonical standard, not a legacy local preference");
    assert.equal(current.startupUpdateChecksEnabled, true);
  } finally { await act(async () => failedRoot.unmount()); console.warn = originalWarn; }
  console.log("desktop preferences: lightweight snapshot, legacy mirror, warning revision and disposal passed");
} finally { dom.window.close(); }
