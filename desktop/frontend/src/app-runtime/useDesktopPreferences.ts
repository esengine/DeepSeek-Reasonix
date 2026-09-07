import { useEffect, useMemo, useState } from "react";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { useCommittedAsyncCommand } from "../lib/useCommittedAsyncCommand";
import { useConfigLoadWarnings } from "../lib/useConfigLoadWarnings";
import { useI18n, useT } from "../lib/i18n";
import { DEFAULT_STATUS_BAR_ITEMS, normalizeStatusBarItems } from "../lib/statusBarItems";
import { hydrateReasoningDisplayMode, setReasoningDisplayPending } from "../lib/reasoningDisplayPreference";
import { hydrateSessionExperience } from "../lib/sessionExperience";
import type { BotRuntimeStatusView } from "../lib/types";
import { app } from "../lib/bridge";
import { applyPreferencesAppearance, layoutStyleFromSnapshot, synchronizeDesktopPreferences, type DesktopPreferencesSnapshot } from "./desktopPreferencesAdapter";
import { sidebarImConnectionsFromBot, sidebarImTopicSourcesFromBot } from "./sidebarImProjection";

export function useDesktopPreferences() {
  const { locale, setPref } = useI18n();
  const t = useT();
  const warnings = useConfigLoadWarnings();
  const [snapshot, setSnapshot] = useState<DesktopPreferencesSnapshot | null>(null);
  const [botRuntime, setBotRuntime] = useState<BotRuntimeStatusView | null>(null);
  const [startupFailed, setStartupFailed] = useState(false);
  const publish = useCommittedCommand((settings: DesktopPreferencesSnapshot, runtime: BotRuntimeStatusView | null) => {
    setPref(applyPreferencesAppearance(settings));
    if ("configWarnings" in settings) warnings.applySnapshot(settings.configWarnings, settings.configWarningsRevision);
    setSnapshot(settings);
    setBotRuntime(runtime);
    setStartupFailed(false);
  });
  const synchronize = useCommittedAsyncCommand((provided?: DesktopPreferencesSnapshot | null, loadTheme: boolean = false) => ({ provided, publish, loadTheme }), synchronizeDesktopPreferences);
  const failed = useCommittedCommand((error: unknown) => {
    setStartupFailed(true);
    if (!snapshot) {
      hydrateSessionExperience("standard");
      hydrateReasoningDisplayMode("auto", false);
    }
    console.warn("desktop preferences sync failed", error);
  });
  const reload = useCommittedCommand(async (provided?: DesktopPreferencesSnapshot | null, loadTheme = false) => {
    const result = await synchronize(provided, loadTheme);
    if (result.status === "failed") failed(result.error);
  });
  useEffect(() => {
    setReasoningDisplayPending();
    void reload(undefined, true);
  }, [reload]);
  useEffect(() => { void app.SetTrayLocale(locale).catch(() => {}); }, [locale]);
  const nativeRuntime = typeof window === "undefined" || Boolean(window.runtime);
  const sidebarImConnections = useMemo(() => snapshot ? sidebarImConnectionsFromBot(snapshot.bot, t, botRuntime, nativeRuntime) : [], [snapshot, t, botRuntime, nativeRuntime]);
  const imTopicSources = useMemo(() => snapshot ? sidebarImTopicSourcesFromBot(snapshot.bot, t) : {}, [snapshot, t]);
  return {
    desktopLayoutStyle: layoutStyleFromSnapshot(snapshot?.desktopLayoutStyle),
    startupUpdateChecksEnabled: snapshot ? snapshot.checkUpdates !== false : startupFailed ? true : null,
    statusBarStyle: snapshot ? snapshot.statusBarStyle === "text" ? "text" as const : "icon" as const : "text" as const,
    statusBarItems: snapshot ? normalizeStatusBarItems(snapshot.statusBarItems) : DEFAULT_STATUS_BAR_ITEMS,
    sidebarImConnections, imTopicSources,
    configLoadWarnings: warnings.configLoadWarnings, reloadConfigWarnings: warnings.reload, dismissConfigWarnings: warnings.dismiss,
    reload,
  };
}
