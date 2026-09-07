// Transient overlays are independent of application page navigation.
import type { Dispatch, SetStateAction } from "react";
import { create } from "zustand";

import { shouldShowStartupSplash } from "../lib/startupSplashState";
import type { ExtensionActionView, SessionMeta } from "../lib/types";

import { applySetState } from "./setState";

export type OverlayState = {
  paletteOpen: boolean;
  paletteSessions: SessionMeta[];
  // Extension actions snapshotted when the palette opens (same staleness
  // contract as paletteSessions).
  paletteExtensionActions: ExtensionActionView[];
  shortcutsOpen: boolean;
  topicExportOpen: boolean;
  sidebarSearchOpen: boolean;
  sidebarSearchFocusSignal: number;
  transientOverlayDismissSignal: number;
  startupSplashVisible: boolean;
  needsOnboarding: boolean | null;
  takeoverDialogTab: string | null;
  reclaimBusyTab: string | null;
  providerSetupNeeded: boolean;
  setPaletteOpen: Dispatch<SetStateAction<boolean>>;
  setPaletteSessions: Dispatch<SetStateAction<SessionMeta[]>>;
  setPaletteExtensionActions: Dispatch<SetStateAction<ExtensionActionView[]>>;
  setShortcutsOpen: Dispatch<SetStateAction<boolean>>;
  setTopicExportOpen: Dispatch<SetStateAction<boolean>>;
  setSidebarSearchOpen: Dispatch<SetStateAction<boolean>>;
  setSidebarSearchFocusSignal: Dispatch<SetStateAction<number>>;
  setTransientOverlayDismissSignal: Dispatch<SetStateAction<number>>;
  setStartupSplashVisible: Dispatch<SetStateAction<boolean>>;
  setNeedsOnboarding: Dispatch<SetStateAction<boolean | null>>;
  setTakeoverDialogTab: Dispatch<SetStateAction<string | null>>;
  setReclaimBusyTab: Dispatch<SetStateAction<string | null>>;
  setProviderSetupNeeded: Dispatch<SetStateAction<boolean>>;
};

export const useOverlayStore = create<OverlayState>((set) => ({
  paletteOpen: false,
  paletteSessions: [],
  paletteExtensionActions: [],
  shortcutsOpen: false,
  topicExportOpen: false,
  sidebarSearchOpen: false,
  sidebarSearchFocusSignal: 0,
  transientOverlayDismissSignal: 0,
  startupSplashVisible: shouldShowStartupSplash(),
  needsOnboarding: null,
  takeoverDialogTab: null,
  reclaimBusyTab: null,
  providerSetupNeeded: false,
  setPaletteOpen: (update) => set((s) => ({ paletteOpen: applySetState(s.paletteOpen, update) })),
  setPaletteSessions: (update) => set((s) => ({ paletteSessions: applySetState(s.paletteSessions, update) })),
  setPaletteExtensionActions: (update) => set((s) => ({ paletteExtensionActions: applySetState(s.paletteExtensionActions, update) })),
  setShortcutsOpen: (update) => set((s) => ({ shortcutsOpen: applySetState(s.shortcutsOpen, update) })),
  setTopicExportOpen: (update) => set((s) => ({ topicExportOpen: applySetState(s.topicExportOpen, update) })),
  setSidebarSearchOpen: (update) => set((s) => ({ sidebarSearchOpen: applySetState(s.sidebarSearchOpen, update) })),
  setSidebarSearchFocusSignal: (update) => set((s) => ({ sidebarSearchFocusSignal: applySetState(s.sidebarSearchFocusSignal, update) })),
  setTransientOverlayDismissSignal: (update) => set((s) => ({ transientOverlayDismissSignal: applySetState(s.transientOverlayDismissSignal, update) })),
  setStartupSplashVisible: (update) => set((s) => ({ startupSplashVisible: applySetState(s.startupSplashVisible, update) })),
  setNeedsOnboarding: (update) => set((s) => ({ needsOnboarding: applySetState(s.needsOnboarding, update) })),
  setTakeoverDialogTab: (update) => set((s) => ({ takeoverDialogTab: applySetState(s.takeoverDialogTab, update) })),
  setReclaimBusyTab: (update) => set((s) => ({ reclaimBusyTab: applySetState(s.reclaimBusyTab, update) })),
  setProviderSetupNeeded: (update) => set((s) => ({ providerSetupNeeded: applySetState(s.providerSetupNeeded, update) })),
}));
