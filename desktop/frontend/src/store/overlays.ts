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
//
// settingsDrafts holds in-progress provider forms so that accidental backdrop
// clicks or Esc presses don't destroy unsaved work. Drafts survive the panel's
// unmount and are only cleared on successful save or explicit discard.

import type { Dispatch, SetStateAction } from "react";
import { create } from "zustand";

import type { SettingsInitialFocus } from "../components/SettingsPanel";
import { shouldShowStartupSplash } from "../components/StartupSplash";
import type { ExtensionActionView, SessionMeta, SettingsTab } from "../lib/types";

import { applySetState } from "./setState";

// ProviderModelDraft is the curated model list produced after fetching models
// from a provider's API. It lives alongside the provider form draft.
export type ProviderModelDraft = {
  providerName: string;
  candidates: string[];
  selected: string[];
  visionModels: string[];
  visionCapability: "configurable" | "unsupported";
};

// SettingsProviderDraft holds every field of the ProviderEditor form so it can
// survive the panel unmounting. Matches the useState fields in ProviderEditor.
// keyDraft is the in-memory API key the user typed; it is cleared on save/discard.
export type SettingsProviderDraft = {
  name: string;
  kind: string;
  baseUrl: string;
  chatUrl: string;
  fullChatUrl: boolean;
  models: string;            // comma-separated text
  modelCandidates: string[]; // from fetch, kept for the model picker
  visionModels: string;      // comma-separated text
  visionModelsConfigured: boolean;
  modelsUrl: string;
  apiKeyEnv: string;
  keyDraft: string;
  balanceUrl: string;
  contextWindow: string;     // empty = unset; stored as string to match the UI
  headersDraft: string;      // textarea content
  extraBodyDraft: string;    // textarea content
  authHeader: boolean;
  modelContextWindows: Record<string, string>; // per-model context overrides
  reasoningProtocol: string;
  thinking: string;
  webSearch: boolean;
  editingProviderName: string | null; // null = new provider; non-null = editing existing
};

export type SettingsDrafts = {
  provider: SettingsProviderDraft | null;
  // providerModelDrafts holds per-provider-group curated model lists
  providerModelDrafts: Record<string, ProviderModelDraft>;
  // addProviderMode tracks whether the add-provider panel is in "official" or "custom" mode
  addProviderMode: "official" | "custom" | null;
};

export const EMPTY_PROVIDER_DRAFT: SettingsProviderDraft = {
  name: "",
  kind: "openai",
  baseUrl: "",
  chatUrl: "",
  fullChatUrl: false,
  models: "",
  modelCandidates: [],
  visionModels: "",
  visionModelsConfigured: false,
  modelsUrl: "",
  apiKeyEnv: "",
  keyDraft: "",
  balanceUrl: "",
  contextWindow: "",
  headersDraft: "",
  extraBodyDraft: "",
  authHeader: false,
  modelContextWindows: {},
  reasoningProtocol: "",
  thinking: "",
  webSearch: false,
  editingProviderName: null,
};

const EMPTY_SETTINGS_DRAFTS: SettingsDrafts = {
  provider: null,
  providerModelDrafts: {},
  addProviderMode: null,
};

export type OverlayState = {
  settingsTarget: SettingsTab | null;
  settingsFocus: SettingsInitialFocus | null;
  paletteOpen: boolean;
  paletteSessions: SessionMeta[];
  // Extension actions snapshotted when the palette opens (same staleness
  // contract as paletteSessions).
  paletteExtensionActions: ExtensionActionView[];
  shortcutsOpen: boolean;
  heartbeatOpen: boolean;
  topicExportOpen: boolean;
  sidebarSearchOpen: boolean;
  sidebarSearchFocusSignal: number;
  transientOverlayDismissSignal: number;
  startupSplashVisible: boolean;
  needsOnboarding: boolean | null;
  settingsDrafts: SettingsDrafts;
  setSettingsTarget: Dispatch<SetStateAction<SettingsTab | null>>;
  setSettingsFocus: Dispatch<SetStateAction<SettingsInitialFocus | null>>;
  setPaletteOpen: Dispatch<SetStateAction<boolean>>;
  setPaletteSessions: Dispatch<SetStateAction<SessionMeta[]>>;
  setPaletteExtensionActions: Dispatch<SetStateAction<ExtensionActionView[]>>;
  setShortcutsOpen: Dispatch<SetStateAction<boolean>>;
  setHeartbeatOpen: Dispatch<SetStateAction<boolean>>;
  setTopicExportOpen: Dispatch<SetStateAction<boolean>>;
  setSidebarSearchOpen: Dispatch<SetStateAction<boolean>>;
  setSidebarSearchFocusSignal: Dispatch<SetStateAction<number>>;
  setTransientOverlayDismissSignal: Dispatch<SetStateAction<number>>;
  setStartupSplashVisible: Dispatch<SetStateAction<boolean>>;
  setNeedsOnboarding: Dispatch<SetStateAction<boolean | null>>;
  setSettingsDrafts: Dispatch<SetStateAction<SettingsDrafts>>;
};

export const useOverlayStore = create<OverlayState>((set) => ({
  settingsTarget: null,
  settingsFocus: null,
  paletteOpen: false,
  paletteSessions: [],
  paletteExtensionActions: [],
  shortcutsOpen: false,
  heartbeatOpen: false,
  topicExportOpen: false,
  sidebarSearchOpen: false,
  sidebarSearchFocusSignal: 0,
  transientOverlayDismissSignal: 0,
  startupSplashVisible: shouldShowStartupSplash(),
  needsOnboarding: null,
  settingsDrafts: { ...EMPTY_SETTINGS_DRAFTS },
  setSettingsTarget: (update) => set((s) => ({ settingsTarget: applySetState(s.settingsTarget, update) })),
  setSettingsFocus: (update) => set((s) => ({ settingsFocus: applySetState(s.settingsFocus, update) })),
  setPaletteOpen: (update) => set((s) => ({ paletteOpen: applySetState(s.paletteOpen, update) })),
  setPaletteSessions: (update) => set((s) => ({ paletteSessions: applySetState(s.paletteSessions, update) })),
  setPaletteExtensionActions: (update) => set((s) => ({ paletteExtensionActions: applySetState(s.paletteExtensionActions, update) })),
  setShortcutsOpen: (update) => set((s) => ({ shortcutsOpen: applySetState(s.shortcutsOpen, update) })),
  setHeartbeatOpen: (update) => set((s) => ({ heartbeatOpen: applySetState(s.heartbeatOpen, update) })),
  setTopicExportOpen: (update) => set((s) => ({ topicExportOpen: applySetState(s.topicExportOpen, update) })),
  setSidebarSearchOpen: (update) => set((s) => ({ sidebarSearchOpen: applySetState(s.sidebarSearchOpen, update) })),
  setSidebarSearchFocusSignal: (update) => set((s) => ({ sidebarSearchFocusSignal: applySetState(s.sidebarSearchFocusSignal, update) })),
  setTransientOverlayDismissSignal: (update) => set((s) => ({ transientOverlayDismissSignal: applySetState(s.transientOverlayDismissSignal, update) })),
  setStartupSplashVisible: (update) => set((s) => ({ startupSplashVisible: applySetState(s.startupSplashVisible, update) })),
  setNeedsOnboarding: (update) => set((s) => ({ needsOnboarding: applySetState(s.needsOnboarding, update) })),
  setSettingsDrafts: (update) => set((s) => ({ settingsDrafts: applySetState(s.settingsDrafts, update) })),
}));
