import type { MouseEvent as ReactMouseEvent, Dispatch, SetStateAction } from "react";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { isMacOSWorkbenchSidebarTitlebar, type DesktopPlatform } from "../lib/desktopPlatform";
import { nativeWindowCommands, syncMainWindowMaximised } from "./useNativeWindowController";
import type { SettingsTab, SettingsView } from "../lib/types";
import type { SettingsInitialFocus } from "../components/SettingsPanel";

export type AppChromeCommandsInput = {
  platform: DesktopPlatform;
  windowsFrameless: boolean;
  closeTransientOverlays: () => void;
  clearImDetail: () => void;
  setSettingsFocus: Dispatch<SetStateAction<SettingsInitialFocus | null>>;
  setSettingsTarget: Dispatch<SetStateAction<SettingsTab | null>>;
  setSidebarSearchOpen: Dispatch<SetStateAction<boolean>>;
  setSidebarSearchFocusSignal: Dispatch<SetStateAction<number>>;
  refreshMeta: () => Promise<unknown>;
  refreshProviderSetupState: () => Promise<unknown>;
  reloadDesktopPreferences: (settings?: SettingsView | null) => Promise<unknown>;
};

/**
 * Owns the window-chrome and settings-surface commands: native window
 * minimize/toggle/close with the maximised re-sync, the frameless titlebar
 * double-click zoom, settings open/close/changed, bot settings entries and
 * the sidebar search toggle. All are stable committed commands; consumers in
 * the chrome, sidebar, IM detail and overlay regions only wire them.
 */
export function useAppChromeCommands(input: AppChromeCommandsInput) {
  const openBotSettings = useCommittedCommand(() => {
    input.closeTransientOverlays();
    input.clearImDetail();
    input.setSettingsFocus(null);
    input.setSettingsTarget("bots");
  });

  const openBotAllowlistSettings = useCommittedCommand((connectionId: string) => {
    input.closeTransientOverlays();
    input.clearImDetail();
    input.setSettingsFocus({ target: "bot-allowlist", connectionId });
    input.setSettingsTarget("bots");
  });

  // The Wails drag runtime ignores anything with detail !== 1, so a double click
  // on a --wails-draggable region never reaches the OS. Both platforms that hide
  // their native title bar need this handled here.
  const chromeDoubleClickZooms = input.windowsFrameless || input.platform === "darwin";
  const handleChromeTitlebarDoubleClick = useCommittedCommand((event: ReactMouseEvent<HTMLDivElement>) => {
    if (!chromeDoubleClickZooms) return;
    const target = event.target as HTMLElement | null;
    const onChromeSurface = target?.closest(".app-chrome, .topicbar, .workbench-dock__tools, .management-screen__chrome");
    const onMacOSWorkbenchSidebarTitlebar = isMacOSWorkbenchSidebarTitlebar(target, event.clientY, input.platform);
    if (!onChromeSurface && !onMacOSWorkbenchSidebarTitlebar) return;
    if (target?.closest("button, input, textarea, select, a, [role='button'], [role='tab'], .windows-window-controls")) return;
    event.preventDefault();
    void nativeWindowCommands.toggleMaximize().then(syncMainWindowMaximised).catch(() => undefined);
  });
  const minimizeMainWindow = useCommittedCommand(() => { void nativeWindowCommands.minimize(); });
  const toggleMainWindowMaximized = useCommittedCommand(() => {
    void nativeWindowCommands.toggleMaximize().then(syncMainWindowMaximised).catch(() => undefined);
  });
  const closeMainWindow = useCommittedCommand(() => { void nativeWindowCommands.close(); });
  const closeSettings = useCommittedCommand(() => {
    input.setSettingsFocus(null);
    input.setSettingsTarget(null);
  });
  const handleSettingsChanged = useCommittedCommand((settings?: SettingsView | null) => {
    void input.refreshMeta();
    void input.refreshProviderSetupState().catch(() => {});
    void input.reloadDesktopPreferences(settings);
  });
  const openSidebarSettings = useCommittedCommand((tab: SettingsTab) => {
    input.closeTransientOverlays();
    input.setSettingsTarget(tab);
  });
  const toggleSidebarSearch = useCommittedCommand(() => {
    input.setSidebarSearchOpen((open) => !open);
    input.setSidebarSearchFocusSignal((signal) => signal + 1);
  });

  return {
    openBotSettings,
    openBotAllowlistSettings,
    handleChromeTitlebarDoubleClick,
    minimizeMainWindow,
    toggleMainWindowMaximized,
    closeMainWindow,
    closeSettings,
    handleSettingsChanged,
    openSidebarSettings,
    toggleSidebarSearch,
  };
}
