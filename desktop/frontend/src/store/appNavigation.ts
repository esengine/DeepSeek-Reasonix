import type { Dispatch, SetStateAction } from "react";
import { create } from "zustand";
import type { SettingsInitialFocus } from "../components/SettingsPanel";
import type { SettingsTab } from "../lib/types";
import { applySetState } from "./setState";

export type AppPage = { kind: "workspace" } | { kind: "settings"; tab: SettingsTab } | { kind: "trash" } | { kind: "automation" };
type NavigationState = {
  page: AppPage;
  workspaceFocus: HTMLElement | null;
  generation: number;
  visitedTrash: boolean;
  visitedAutomation: boolean;
  lastSettingsTarget: SettingsTab;
  settingsFocus: SettingsInitialFocus | null;
  automationReturn: boolean;
  openPage: (page: AppPage) => void;
  returnToWorkspace: () => void;
  setSettingsTarget: Dispatch<SetStateAction<SettingsTab | null>>;
  setSettingsFocus: Dispatch<SetStateAction<SettingsInitialFocus | null>>;
  enterConversation: () => void;
  returnFromAutomationLink: (generation: number) => void;
};
export const useAppNavigationStore = create<NavigationState>((set, get) => ({
  page: { kind: "workspace" }, workspaceFocus: null, generation: 0, visitedTrash: false, visitedAutomation: false,
  lastSettingsTarget: "general", settingsFocus: null, automationReturn: false,
  openPage: (page) => set((state) => ({
    page: page.kind === "settings" ? { ...page, tab: page.tab === "providers" ? "models" : page.tab } : page,
    workspaceFocus: state.page.kind === "workspace" && page.kind !== "workspace" && typeof document !== "undefined" ? document.activeElement as HTMLElement | null : state.workspaceFocus,
    generation: state.generation + 1,
    visitedTrash: state.visitedTrash || page.kind === "trash",
    visitedAutomation: state.visitedAutomation || page.kind === "automation",
    lastSettingsTarget: page.kind === "settings" ? (page.tab === "providers" ? "models" : page.tab) : state.lastSettingsTarget,
    automationReturn: false,
  })),
  returnToWorkspace: () => get().openPage({ kind: "workspace" }),
  enterConversation: () => get().openPage({ kind: "workspace" }),
  setSettingsTarget: (update) => {
    const state = get();
    const target = applySetState(state.page.kind === "settings" ? state.page.tab : null, update);
    if (target === null) state.returnToWorkspace();
    else state.openPage({ kind: "settings", tab: target });
  },
  setSettingsFocus: (update) => set((state) => ({ settingsFocus: applySetState(state.settingsFocus, update) })),
  returnFromAutomationLink: (generation) => {
    if (get().generation === generation) set({ page: { kind: "workspace" }, automationReturn: true });
  },
}));
