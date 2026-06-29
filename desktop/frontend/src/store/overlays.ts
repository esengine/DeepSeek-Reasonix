// overlays owns the transient surfaces layered over the workspace — the command
// palette, the settings panel's open target/focus, the shortcuts / heartbeat /
// topic-export dialogs, sidebar search, the startup splash, and the onboarding
// gate — plus two imperative coordination signals (dismiss-all overlays, and
// sidebar-search focus) that callers bump to trigger an effect downstream.
//
// None of this is persisted (the splash uses sessionStorage internally via
// shouldShowStartupSplash). Every atom initializes to the same value the prior
// App-local useState used, and setters mirror Dispatch<SetStateAction<T>>, so
// the migrated call sites — including functional toggles/bumps — are drop-in and
// behavior is unchanged.

import type { Dispatch, SetStateAction } from "react";
import { create } from "zustand";

import type { SettingsInitialFocus } from "../components/SettingsPanel";
import { shouldShowStartupSplash } from "../components/StartupSplash";
import type { SessionMeta, SettingsTab } from "../lib/types";

import { applySetState } from "./setState";

export type OverlayState = {
  settingsTarget: SettingsTab | null;
  settingsFocus: SettingsInitialFocus | null;
  paletteOpen: boolean;
  paletteSessions: SessionMeta[];
  shortcutsOpen: boolean;
  heartbeatOpen: boolean;
  topicExportOpen: boolean;
  sidebarSearchOpen: boolean;
  sidebarSearchFocusSignal: number;
  transientOverlayDismissSignal: number;
  startupSplashVisible: boolean;
  needsOnboarding: boolean | null;
  setSettingsTarget: Dispatch<SetStateAction<SettingsTab | null>>;
  setSettingsFocus: Dispatch<SetStateAction<SettingsInitialFocus | null>>;
  setPaletteOpen: Dispatch<SetStateAction<boolean>>;
  setPaletteSessions: Dispatch<SetStateAction<SessionMeta[]>>;
  setShortcutsOpen: Dispatch<SetStateAction<boolean>>;
  setHeartbeatOpen: Dispatch<SetStateAction<boolean>>;
  setTopicExportOpen: Dispatch<SetStateAction<boolean>>;
  setSidebarSearchOpen: Dispatch<SetStateAction<boolean>>;
  setSidebarSearchFocusSignal: Dispatch<SetStateAction<number>>;
  setTransientOverlayDismissSignal: Dispatch<SetStateAction<number>>;
  setStartupSplashVisible: Dispatch<SetStateAction<boolean>>;
  setNeedsOnboarding: Dispatch<SetStateAction<boolean | null>>;
};

export const useOverlayStore = create<OverlayState>((set) => ({
  settingsTarget: null,
  settingsFocus: null,
  paletteOpen: false,
  paletteSessions: [],
  shortcutsOpen: false,
  heartbeatOpen: false,
  topicExportOpen: false,
  sidebarSearchOpen: false,
  sidebarSearchFocusSignal: 0,
  transientOverlayDismissSignal: 0,
  startupSplashVisible: shouldShowStartupSplash(),
  needsOnboarding: null,
  setSettingsTarget: (update) => set((s) => ({ settingsTarget: applySetState(s.settingsTarget, update) })),
  setSettingsFocus: (update) => set((s) => ({ settingsFocus: applySetState(s.settingsFocus, update) })),
  setPaletteOpen: (update) => set((s) => ({ paletteOpen: applySetState(s.paletteOpen, update) })),
  setPaletteSessions: (update) => set((s) => ({ paletteSessions: applySetState(s.paletteSessions, update) })),
  setShortcutsOpen: (update) => set((s) => ({ shortcutsOpen: applySetState(s.shortcutsOpen, update) })),
  setHeartbeatOpen: (update) => set((s) => ({ heartbeatOpen: applySetState(s.heartbeatOpen, update) })),
  setTopicExportOpen: (update) => set((s) => ({ topicExportOpen: applySetState(s.topicExportOpen, update) })),
  setSidebarSearchOpen: (update) => set((s) => ({ sidebarSearchOpen: applySetState(s.sidebarSearchOpen, update) })),
  setSidebarSearchFocusSignal: (update) => set((s) => ({ sidebarSearchFocusSignal: applySetState(s.sidebarSearchFocusSignal, update) })),
  setTransientOverlayDismissSignal: (update) => set((s) => ({ transientOverlayDismissSignal: applySetState(s.transientOverlayDismissSignal, update) })),
  setStartupSplashVisible: (update) => set((s) => ({ startupSplashVisible: applySetState(s.startupSplashVisible, update) })),
  setNeedsOnboarding: (update) => set((s) => ({ needsOnboarding: applySetState(s.needsOnboarding, update) })),
}));
