import { requestSessionVersions } from "../lib/sessionRecoveryVersionHostBridge";
import type { AppOverlayHostProps } from "./AppOverlayHost";
import type { HistoryViewState } from "../app-runtime/historyViewProjection";
import type { useHistoryCommands } from "../app-runtime/useHistoryCommands";
import type { useSessionNavigationCommands } from "../app-runtime/useSessionNavigationCommands";
import type { useAppChromeCommands } from "../app-runtime/useAppChromeCommands";
import type { useOnboardingCommands } from "../app-runtime/useOnboardingCommands";
import type { useWorktreeMergeCommands } from "../app-runtime/useWorktreeMergeCommands";
import type { useAppShellStores } from "../app-runtime/useAppShellStores";
import type { TabMeta } from "../lib/types";
import type { Translator } from "../lib/i18n";

type HistoryCommands = ReturnType<typeof useHistoryCommands>;
type NavigationCommands = ReturnType<typeof useSessionNavigationCommands>;
type ChromeCommands = ReturnType<typeof useAppChromeCommands>;
type OnboardingCommands = ReturnType<typeof useOnboardingCommands>;
type WorktreeMergeCommands = ReturnType<typeof useWorktreeMergeCommands>;

type OverlayPalette = NonNullable<AppOverlayHostProps["palette"]>;
type ShellStores = ReturnType<typeof useAppShellStores>;

/** Pure prop assembly for AppOverlayHost; overlay visibility flags come from
 *  the caller's stores, command identity from its owner hooks. */
export function buildOverlayHostProps(input: {
  t: Translator;
  running: boolean;
  histView: HistoryViewState | null;
  pageKind: string;
  automationTopic: NonNullable<AppOverlayHostProps["automation"]>["commands"]["onOpenTopic"];
  shell: ShellStores;
  activeTab: TabMeta | undefined;
  activeTabId: string | undefined;
  cwd: string | undefined;
  paletteItems: OverlayPalette["view"]["items"];
  startupSplashHold: boolean;
  selectionEnabled: boolean;
  history: HistoryCommands;
  navigation: NavigationCommands;
  chrome: ChromeCommands;
  onboarding: OnboardingCommands;
  worktree: WorktreeMergeCommands;
  onAddSelectedText: (text: string) => void;
  prefillSubagentCommand: (command: string) => void;

  sessionActions: {
    previewSession: NonNullable<AppOverlayHostProps["history"]>["commands"]["onPreview"];
    listTrashedSessions: NonNullable<AppOverlayHostProps["trash"]>["commands"]["list"];
    restoreSession: NonNullable<AppOverlayHostProps["trash"]>["commands"]["restore"];
    purgeTrashedSession: NonNullable<AppOverlayHostProps["trash"]>["commands"]["purge"];
  };
  setSettingsTarget: NonNullable<AppOverlayHostProps["settings"]>["commands"]["onNavigate"];
}): AppOverlayHostProps {
  const { t, history, navigation, chrome, onboarding, worktree, shell, sessionActions } = input;
  const histView = input.histView;
  const settingsTarget = shell.settingsTarget;
  return {
    history: histView ? {
      view: { kind: histView.kind, sessions: histView.sessions, running: input.running },
      commands: {
        onResume: navigation.onResumeSession, onPreview: sessionActions.previewSession, onDelete: history.onDeleteSession,
        onRename: history.onRenameHistorySession,
        onInspectVersions: requestSessionVersions, onClose: history.closeHistory,
      },
    } : undefined,
    trash: shell.visitedTrash ? { view: { active: input.pageKind === "trash" },
      commands: { onBack: shell.returnToWorkspace, list: sessionActions.listTrashedSessions, restore: sessionActions.restoreSession, purge: sessionActions.purgeTrashedSession } } : undefined,
    automation: shell.visitedAutomation ? { view: { active: input.pageKind === "automation" },
      commands: { onBack: shell.returnToWorkspace, onOpenTopic: input.automationTopic } } : undefined,
    recovery: {
      view: { sessions: histView?.sessions },
      commands: { onResumeSession: navigation.onResumeSession, onRecoveryCreated: navigation.onRecoveryCreated, onLineageChanged: navigation.onRecoveryLineageChanged },
    },
    settings: settingsTarget ? {
      view: {
        initialTab: settingsTarget, initialFocus: shell.settingsFocus ?? undefined,
        agentRunning: input.running, desktopPlatform: shell.desktopPlatform,
        activeWorkspaceKey: `${input.activeTab?.id ?? input.activeTabId ?? ""}\u0000${input.activeTab?.workspaceRoot ?? input.activeTab?.cwd ?? input.cwd ?? ""}`,
      },
      commands: { onUseSubagent: input.prefillSubagentCommand, onClose: chrome.closeSettings, onNavigate: input.setSettingsTarget, onChanged: chrome.handleSettingsChanged },
    } : undefined,
    palette: shell.paletteOpen ? {
      view: { open: true, items: input.paletteItems, placeholder: t("palette.placeholder"), emptyText: t("palette.empty") },
      commands: { onClose: () => shell.setPaletteOpen(false) },
    } : undefined,
    shortcuts: {
      view: { open: shell.shortcutsOpen, platform: shell.desktopPlatform, t },
      commands: { onClose: () => shell.setShortcutsOpen(false) },
    },
    startup: shell.startupSplashVisible ? {
      view: { hold: input.startupSplashHold }, commands: { onDone: () => shell.setStartupSplashVisible(false) },
    } : undefined,
    onboarding: shell.needsOnboarding ? {
      view: {}, commands: { onComplete: onboarding.completeOnboarding, onChooseProvider: onboarding.chooseOnboardingProvider, onSkip: onboarding.skipOnboarding },
    } : undefined,
    selection: {
      view: {
        enabled: input.selectionEnabled,
        resetKey: input.activeTabId ?? "",
      },
      commands: { onAddToChat: input.onAddSelectedText },
    },
    worktree: worktree.worktreeMergeTabId ? {
      view: { tabId: worktree.worktreeMergeTabId, isOpen: true },
      commands: { onClose: worktree.closeWorktreeMerge, onMerged: worktree.handleWorktreeMerged },
    } : undefined,
  };
}
