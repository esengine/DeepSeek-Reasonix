import { defaultSidebarWidth, SIDEBAR_MAX_WIDTH } from "../store/layout";
import type { Translator } from "../lib/i18n";
import type { Meta, TabMeta } from "../lib/types";
import type { SidebarImTopicSource } from "../app-runtime/sidebarImProjection";
import type { useSessionBannerCommands } from "../app-runtime/useSessionBannerCommands";
import type { useProjectTopicCommands } from "../app-runtime/useProjectTopicCommands";
import type { useOnboardingCommands } from "../app-runtime/useOnboardingCommands";
import type { useShellGeometry } from "../app-runtime/useShellGeometry";
import type { useAppShellStores } from "../app-runtime/useAppShellStores";
import type { SessionStatusBannersProps } from "./SessionStatusBanners";
import type { SidebarRegionProps } from "./SidebarRegion";
import { sessionIdentityRoute } from "../lib/sessionIdentity";

type BannerCommands = ReturnType<typeof useSessionBannerCommands>;
type ShellStores = ReturnType<typeof useAppShellStores>;
type ProjectTopicCommands = ReturnType<typeof useProjectTopicCommands>;
type OnboardingCommands = ReturnType<typeof useOnboardingCommands>;

/** Pure prop assembly for the chrome regions (sidebar, app chrome, status
 *  banners); store and hook ownership stays with the caller. */

export function buildSidebarRegionProps(input: {
  className: string;
  shell: ShellStores;
  t: Translator;
  geometry: ReturnType<typeof useShellGeometry>;
  projectTree: {
    activeTab: TabMeta | undefined;
    activeScope?: string;
    activeWorkspaceRoot?: string;
    imTopicSources: Record<string, SidebarImTopicSource>;
    refreshSignal: number;
    searchExpanded: boolean;
    searchFocusSignal: number;
    showShortcutBadges: boolean;
    shortcutPlatform: SidebarRegionProps["projectTree"]["shortcutPlatform"];
    onVisibleTopicsChange: SidebarRegionProps["projectTree"]["onVisibleTopicsChange"];
    draftSummaries: SidebarRegionProps["projectTree"]["draftSummaries"];
    onOpenDraft: SidebarRegionProps["projectTree"]["onOpenDraft"];
  };
  topics: ProjectTopicCommands;
  commands: {
    onNewSession: () => void;
    onOpenPalette: () => void;
    onOpenTrash: () => void;
    onOpenAutomation: () => void;
    onOpenSettings: SidebarRegionProps["onOpenSettings"];
    onOpenTopic: SidebarRegionProps["projectTree"]["onOpenTopic"];
  };
  paletteShortcut: string;
}): SidebarRegionProps {
  const { geometry, topics, commands } = input;
  const shell = input.shell;
  return {
    className: input.className,
    collapsed: shell.sidebarCollapsed,
    t: input.t,
    onNewSession: commands.onNewSession,
    onOpenPalette: commands.onOpenPalette,
    paletteShortcut: input.paletteShortcut,
    onOpenTrash: commands.onOpenTrash,
    onOpenAutomation: commands.onOpenAutomation,
    onOpenSettings: commands.onOpenSettings,
    resize: {
      min: geometry.sidebarResizeMinWidth, max: SIDEBAR_MAX_WIDTH, value: geometry.sidebarRenderWidth,
      onPointerDown: geometry.startSidebarResize, onKeyDown: geometry.resizeSidebarWithKeyboard,
      onReset: () => geometry.setExpandedSidebarWidth(defaultSidebarWidth()),
    },
    projectTree: {
      activeScope: input.projectTree.activeScope ?? input.projectTree.activeTab?.scope,
      activeWorkspaceRoot: input.projectTree.activeWorkspaceRoot ?? input.projectTree.activeTab?.workspaceRoot,
      activeTopicId: input.projectTree.activeTab?.topicId, activeSessionPath: sessionIdentityRoute(input.projectTree.activeTab),
      activeRemote: input.projectTree.activeTab?.remote
        ? { ...input.projectTree.activeTab.remote, sessionId: input.projectTree.activeTab.sessionId }
        : undefined,
      imTopicSources: input.projectTree.imTopicSources, onOpenTopic: commands.onOpenTopic,
      onCreateTopic: topics.onCreateTopic, onCreateIsolatedWorktree: topics.onCreateIsolatedWorktree,
      onTopicsChanged: topics.refreshProjectsAndTabs, onRenameTopic: topics.renameTopic, refreshSignal: input.projectTree.refreshSignal,
      onAddProject: topics.onAddProject,
      searchExpanded: input.projectTree.searchExpanded, searchFocusSignal: input.projectTree.searchFocusSignal,
      showShortcutBadges: input.projectTree.showShortcutBadges, shortcutPlatform: input.projectTree.shortcutPlatform,
      onVisibleTopicsChange: input.projectTree.onVisibleTopicsChange,
      draftSummaries: input.projectTree.draftSummaries,
      onOpenDraft: input.projectTree.onOpenDraft,
    },
  };
}

export function buildSessionStatusBannerProps(input: {
  t: Translator;
  activeTab: TabMeta | undefined;
  leaseBlocked: SessionStatusBannersProps["leaseBlocked"];
  meta: Meta | null | undefined;
  configWarnings: SessionStatusBannersProps["configWarnings"];
  dismissConfigWarnings: () => void;
  updateChecksEnabled: boolean;
  shell: ShellStores;
  banners: BannerCommands;
  onboarding: OnboardingCommands;
}): SessionStatusBannersProps {
  const { banners, shell, onboarding } = input;
  return {
    t: input.t,
    takenOver: Boolean(input.activeTab?.takenOver),
    reclaimTabId: input.activeTab?.id ?? "",
    reclaimBusyTabId: shell.reclaimBusyTab,
    onReclaim: banners.reclaimSession,
    leaseBlocked: input.leaseBlocked,
    startupError: input.meta?.startupErr,
    takeoverDialogTabId: shell.takeoverDialogTab,
    onOpenTakeover: banners.openTakeoverDialog,
    onCloseTakeover: banners.closeTakeoverDialog,
    configWarnings: input.configWarnings,
    onOpenConfigFile: banners.openConfigFile,
    onReloadConfigFile: banners.reloadConfigFile,
    onDismissConfigWarnings: input.dismissConfigWarnings,
    providerSetupNeeded: shell.providerSetupNeeded,
    needsOnboarding: shell.needsOnboarding,
    onConfigureProvider: () => {
      onboarding.chooseOnboardingProvider();
    },
    updateChecksEnabled: input.updateChecksEnabled,
    onShowReleaseNotes: banners.showReleaseNotes,
  };
}

/** The app/layout frame class lists; flags arrive from the caller's stores. */
export function buildAppShellClassNames(input: {
  platform: string;
  windowsFrameless: boolean;
  browserPreview: boolean;
  imDetailActive: boolean;
  sidebarCollapsed: boolean;
  sidebarResizing: boolean;
  dockGridOpen: boolean;
  dockOverlay: boolean;
  terminalOpen: boolean;
  terminalResizing: boolean;
  dockOpen: boolean;
  dockMaximized: boolean;
  dockResizing: boolean;
}): { app: string; layout: string } {
  return {
    app: [
      "app",
      `app--${input.platform}`,
      input.windowsFrameless ? "app--windows-frameless" : "",
      input.browserPreview ? "app--browser-preview" : "",
      "app--workbench",
    ].filter(Boolean).join(" "),
    layout: [
      "layout",
      "layout--workbench",
      input.imDetailActive ? "layout--statusbar-hidden" : "",
      input.sidebarCollapsed ? "layout--sidebar-collapsed" : "",
      input.sidebarResizing ? "layout--resizing layout--sidebar-resizing" : "",
      input.dockGridOpen ? "layout--workspace-open" : "",
      input.dockOverlay ? "layout--workspace-overlay" : "",
      "layout--terminal-drawer-open",
      input.terminalOpen ? "layout--terminal-drawer-expanded" : "",
      input.terminalResizing ? "layout--terminal-resizing" : "",
      input.dockOpen && input.dockMaximized ? "layout--workspace-maximized" : "",
      input.dockResizing ? "layout--resizing layout--workspace-resizing" : "",
    ]
      .filter(Boolean)
      .join(" "),
  };
}
