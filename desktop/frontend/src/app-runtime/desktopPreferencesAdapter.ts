import { app } from "../lib/bridge";
import { clearLegacyLangPref, normalizeLangPref, readLegacyLangPref } from "../lib/i18n";
import { clearLegacyThemePreference, normalizeThemePreference, normalizeThemeStyleForTheme, readLegacyThemePreference } from "../lib/theme";
import { applyConfiguredBaseAppearance, applyThemePack, clearThemePack } from "../lib/themePack";
import { applyTerminalThemePreference } from "../lib/terminalTheme";
import { applyConversationWidth } from "../lib/conversationWidth";
import { hydrateReasoningDisplayMode } from "../lib/reasoningDisplayPreference";
import { hydrateSessionExperience } from "../lib/sessionExperience";
import { applyLayoutStyleDefaults } from "../store/layout";
import { loadBotRuntimeStatus } from "./botRuntimeAdapter";
import type { CommandAuthority } from "../lib/commandOutcome";
import type { BotRuntimeStatusView, DesktopStartupSettingsView, SettingsView } from "../lib/types";

export type DesktopPreferencesSnapshot = DesktopStartupSettingsView | SettingsView;
export function layoutStyleFromSnapshot(style?: string) {
  return style === "creation" ? "creation" : style === "classic" ? "classic" : "workbench";
}
export function applyPreferencesAppearance(settings: DesktopPreferencesSnapshot) {
  const theme = normalizeThemePreference(settings.desktopTheme);
  applyConfiguredBaseAppearance(theme, normalizeThemeStyleForTheme(settings.desktopThemeStyle, theme));
  applyTerminalThemePreference(settings.desktopTerminalTheme);
  applyConversationWidth(settings.conversationWidth);
  applyLayoutStyleDefaults(layoutStyleFromSnapshot(settings.desktopLayoutStyle));
  hydrateSessionExperience(settings.sessionExperience);
  hydrateReasoningDisplayMode(settings.sessionExperience === "deep" ? "expanded" : "auto", settings.sessionExperience === "deep");
  return normalizeLangPref(settings.desktopLanguage);
}
type Input = {
  provided?: DesktopPreferencesSnapshot | null;
  loadTheme: boolean;
  publish: (settings: DesktopPreferencesSnapshot, runtime: BotRuntimeStatusView | null) => void;
};

/** Every async boundary is fenced before publishing preferences or theme DOM. */
export async function synchronizeDesktopPreferences(input: Input, authority: CommandAuthority) {
  authority.checkpoint();
  const language = readLegacyLangPref();
  const theme = readLegacyThemePreference();
  if (language || theme.hasValue) {
    await app.MigrateDesktopPreferences(language, theme.theme, theme.style);
    authority.checkpoint();
    clearLegacyLangPref();
    clearLegacyThemePreference();
  }
  const [settings, runtime] = await Promise.all([
    input.provided ?? app.DesktopStartupSettings(), loadBotRuntimeStatus(),
  ]);
  authority.checkpoint();
  input.publish(settings, runtime);
  if (!input.loadTheme) return;
  try {
    const { loadThemeExperience, applyExperienceToDOM } = await import("../lib/themeExperience");
    authority.checkpoint();
    const experience = await loadThemeExperience();
    authority.checkpoint();
    applyExperienceToDOM(experience);
  } catch {
    authority.checkpoint();
    try {
      const active = await app.GetActiveThemePack();
      authority.checkpoint();
      if (active?.pack) applyThemePack(active.pack);
      else clearThemePack();
    } catch {
      authority.checkpoint();
      clearThemePack();
    }
  }
}
