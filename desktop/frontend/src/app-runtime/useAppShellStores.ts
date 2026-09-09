import { useLayoutStore } from "../store/layout";
import { useOverlayStore } from "../store/overlays";
import { useAppNavigationStore } from "../store/appNavigation";
import { useRemoteStore } from "../store/remote";
import { useWindowChromeStore } from "../store/windowChrome";
import { useDesktopPreferences } from "./useDesktopPreferences";

/**
 * Single subscription surface for the store-backed shell state AppRuntime
 * wires into regions: overlay visibility, navigation page, layout geometry
 * flags, remote catalogs, window chrome and desktop preferences. Controller
 * state never flows through here — this hook only reads stores.
 */
export function useAppShellStores() {
  const startupSplashVisible = useOverlayStore((s) => s.startupSplashVisible);
  const setStartupSplashVisible = useOverlayStore((s) => s.setStartupSplashVisible);
  // null until the mount probe resolves; true shows the first-run guide.
  const needsOnboarding = useOverlayStore((s) => s.needsOnboarding);
  const providerSetupNeeded = useOverlayStore((s) => s.providerSetupNeeded);
  const setProviderSetupNeeded = useOverlayStore((s) => s.setProviderSetupNeeded);
  const paletteOpen = useOverlayStore((s) => s.paletteOpen);
  const setPaletteOpen = useOverlayStore((s) => s.setPaletteOpen);
  const shortcutsOpen = useOverlayStore((s) => s.shortcutsOpen);
  const setShortcutsOpen = useOverlayStore((s) => s.setShortcutsOpen);
  const takeoverDialogTab = useOverlayStore((s) => s.takeoverDialogTab);
  const reclaimBusyTab = useOverlayStore((s) => s.reclaimBusyTab);
  const transientOverlayDismissSignal = useOverlayStore((s) => s.transientOverlayDismissSignal);
  const setTransientOverlayDismissSignal = useOverlayStore((s) => s.setTransientOverlayDismissSignal);
  const sidebarSearchOpen = useOverlayStore((s) => s.sidebarSearchOpen);
  const setSidebarSearchOpen = useOverlayStore((s) => s.setSidebarSearchOpen);
  const sidebarSearchFocusSignal = useOverlayStore((s) => s.sidebarSearchFocusSignal);
  const setSidebarSearchFocusSignal = useOverlayStore((s) => s.setSidebarSearchFocusSignal);

  const page = useAppNavigationStore((s) => s.page);
  const openPage = useAppNavigationStore((s) => s.openPage);
  const returnToWorkspace = useAppNavigationStore((s) => s.returnToWorkspace);
  const enterConversation = useAppNavigationStore((s) => s.enterConversation);
  const visitedTrash = useAppNavigationStore((s) => s.visitedTrash);
  const visitedAutomation = useAppNavigationStore((s) => s.visitedAutomation);
  const automationReturn = useAppNavigationStore((s) => s.automationReturn);
  const setSettingsTarget = useAppNavigationStore((s) => s.setSettingsTarget);
  const settingsFocus = useAppNavigationStore((s) => s.settingsFocus);
  const setSettingsFocus = useAppNavigationStore((s) => s.setSettingsFocus);

  const sidebarCollapsed = useLayoutStore((s) => s.sidebarCollapsed);
  const sidebarResizing = useLayoutStore((state) => state.sidebarResizing);
  const sidebarTogglePressed = useLayoutStore((state) => state.sidebarTogglePressed);
  const workspacePanelOpen = useLayoutStore((s) => s.workspacePanelOpen);
  const rightDockTreeWidth = useLayoutStore((s) => s.rightDockTreeWidth);
  const setRightDockTreeWidth = useLayoutStore((s) => s.setRightDockTreeWidth);
  const rightDockPreviewWidth = useLayoutStore((s) => s.rightDockPreviewWidth);
  const workspacePanelResizing = useLayoutStore((state) => state.workspacePanelResizing);
  const liveTerminalHeight = useLayoutStore((state) => state.liveTerminalHeight);
  const setLiveWorkspacePanelRenderWidth = useLayoutStore((state) => state.setLiveWorkspacePanelRenderWidth);
  const workspacePanelMaximized = useLayoutStore((s) => s.workspacePanelMaximized);
  const rightDockMode = useLayoutStore((s) => s.rightDockMode);
  const terminalPanelOpen = useLayoutStore((s) => s.terminalPanelOpen);

  const remoteHosts = useRemoteStore((s) => s.hosts);
  const remoteStatuses = useRemoteStore((s) => s.statuses);
  const requestRemoteExplorer = useRemoteStore((s) => s.openExplorer);

  const desktopPlatform = useWindowChromeStore((state) => state.platform);
  const mainWindowMaximised = useWindowChromeStore((state) => state.mainWindowMaximised);

  const preferences = useDesktopPreferences();

  const managementActive = page.kind !== "workspace";
  const settingsTarget = page.kind === "settings" ? page.tab : null;
  const desktopLayoutStyle = preferences.desktopLayoutStyle;
  const singleSurfaceLayout = desktopLayoutStyle === "workbench" || desktopLayoutStyle === "creation";
  const sidebarWorkbench = desktopLayoutStyle === "workbench";
  const sidebarCreation = desktopLayoutStyle === "creation";
  const windowsFramelessChrome = desktopPlatform === "windows";
  const terminalResizing = liveTerminalHeight !== null;

  return {
    startupSplashVisible, setStartupSplashVisible,
    needsOnboarding, providerSetupNeeded, setProviderSetupNeeded,
    paletteOpen, setPaletteOpen, shortcutsOpen, setShortcutsOpen,
    takeoverDialogTab, reclaimBusyTab,
    transientOverlayDismissSignal, setTransientOverlayDismissSignal,
    sidebarSearchOpen, setSidebarSearchOpen, sidebarSearchFocusSignal, setSidebarSearchFocusSignal,
    page, openPage, returnToWorkspace, enterConversation,
    visitedTrash, visitedAutomation, automationReturn,
    settingsTarget, settingsFocus, setSettingsTarget, setSettingsFocus,
    sidebarCollapsed, sidebarResizing, sidebarTogglePressed,
    workspacePanelOpen, rightDockTreeWidth, setRightDockTreeWidth, rightDockPreviewWidth,
    workspacePanelResizing, liveTerminalHeight, setLiveWorkspacePanelRenderWidth,
    workspacePanelMaximized, rightDockMode, terminalPanelOpen,
    remoteHosts, remoteStatuses, requestRemoteExplorer,
    desktopPlatform, mainWindowMaximised,
    preferences,
    managementActive, desktopLayoutStyle, singleSurfaceLayout, sidebarWorkbench, sidebarCreation,
    windowsFramelessChrome, terminalResizing,
  };
}
