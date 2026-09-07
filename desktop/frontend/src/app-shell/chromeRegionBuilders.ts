import { defaultCreationSidebarWidth, defaultSidebarWidth, SIDEBAR_MAX_WIDTH } from "../store/layout";
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

type BannerCommands = ReturnType<typeof useSessionBannerCommands>;
type ShellStores = ReturnType<typeof useAppShellStores>;
type ProjectTopicCommands = ReturnType<typeof useProjectTopicCommands>;
type OnboardingCommands = ReturnType<typeof useOnboardingCommands>;

/** Pure prop assembly for the chrome regions (sidebar, app chrome, status
 *  banners); store and hook ownership stays with the caller. */

export function buildSidebarRegionProps(input: {
  automation: boolean;
  className: string;
  toggleTitle: string;
  shell: ShellStores;
  t: Translator;
  geometry: ReturnType<typeof useShellGeometry>;
  projectTree: {
    activeTab: TabMeta | undefined;
    imTopicSources: Record<string, SidebarImTopicSource>;
    refreshSignal: number;
    timeFilter: SidebarRegionProps["projectTree"]["timeFilter"];
    onTimeFilterChange: SidebarRegionProps["projectTree"]["onTimeFilterChange"];
    searchExpanded: boolean;
    searchFocusSignal: number;
    showShortcutBadges: boolean;
    shortcutPlatform: SidebarRegionProps["projectTree"]["shortcutPlatform"];
    onVisibleTopicsChange: SidebarRegionProps["projectTree"]["onVisibleTopicsChange"];
  };
  topics: ProjectTopicCommands;
  commands: {
    onNewSession: () => void;
    onOpenTrash: () => void;
    onOpenAutomation: () => void;
    onOpenSettings: SidebarRegionProps["onOpenSettings"];
    onToggleSearch: () => void;
    onToggle: () => void;
    onOpenTopic: SidebarRegionProps["projectTree"]["onOpenTopic"];
  };
}): SidebarRegionProps {
  const { geometry, topics, commands } = input;
  const shell = input.shell;
  return {
    automation: input.automation,
    className: input.className,
    workbench: shell.sidebarWorkbench,
    creation: shell.sidebarCreation,
    collapsed: shell.sidebarCollapsed,
    navTooltipDisabled: !shell.sidebarCollapsed,
    searchOpen: shell.sidebarSearchOpen,
    togglePressed: shell.sidebarTogglePressed,
    toggleTitle: input.toggleTitle,
    t: input.t,
    onNewSession: commands.onNewSession,
    onOpenTrash: commands.onOpenTrash,
    onOpenAutomation: commands.onOpenAutomation,
    onOpenSettings: commands.onOpenSettings,
    onToggleSearch: commands.onToggleSearch,
    onToggle: commands.onToggle,
    resize: {
      min: geometry.sidebarResizeMinWidth, max: SIDEBAR_MAX_WIDTH, value: geometry.sidebarRenderWidth,
      onPointerDown: geometry.startSidebarResize, onKeyDown: geometry.resizeSidebarWithKeyboard,
      onReset: () => geometry.setExpandedSidebarWidth(shell.sidebarCreation ? defaultCreationSidebarWidth() : defaultSidebarWidth()),
    },
    projectTree: {
      activeScope: input.projectTree.activeTab?.scope, activeWorkspaceRoot: input.projectTree.activeTab?.workspaceRoot,
      activeTopicId: input.projectTree.activeTab?.topicId, activeSessionPath: input.projectTree.activeTab?.sessionPath,
      activeRemote: input.projectTree.activeTab?.remote, imTopicSources: input.projectTree.imTopicSources, onOpenTopic: commands.onOpenTopic,
      onCreateTopic: topics.onCreateTopic, onCreateIsolatedWorktree: topics.onCreateIsolatedWorktree,
      onTopicsChanged: topics.refreshProjectsAndTabs, onRenameTopic: topics.renameTopic, refreshSignal: input.projectTree.refreshSignal,
      onAddProject: topics.onAddProject,
      timeFilter: input.projectTree.timeFilter, onTimeFilterChange: input.projectTree.onTimeFilterChange,
      variant: shell.sidebarWorkbench ? "workbench" : shell.sidebarCreation ? "creation" : "classic",
      searchExpanded: input.projectTree.searchExpanded, searchFocusSignal: input.projectTree.searchFocusSignal,
      showShortcutBadges: input.projectTree.showShortcutBadges, shortcutPlatform: input.projectTree.shortcutPlatform,
      onVisibleTopicsChange: input.projectTree.onVisibleTopicsChange,
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
      shell.setProviderSetupNeeded(false);
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
  workbench: boolean;
  creation: boolean;
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
      input.workbench ? "app--workbench" : "",
      input.creation ? "app--creation" : "",
      !input.workbench && !input.creation ? "app--classic" : "",
    ].filter(Boolean).join(" "),
    layout: [
      "layout",
      input.workbench ? "layout--workbench" : "",
      input.workbench ? "layout--workbench-chrome-hidden" : "",
      input.creation ? "layout--creation-chrome-hidden" : "",
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
