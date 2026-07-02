import { Minus, Search, Square, X } from "lucide-react";
import { TabBar } from "./TabBar";
import type { TabMeta } from "../lib/types";
import { useT } from "../lib/i18n";

type DesktopPlatform = "darwin" | "windows" | "linux";

interface AppChromeProps {
  platform: DesktopPlatform;
  browserPreviewChrome: boolean;
  workbenchChrome?: boolean;
  tabs: TabMeta[];
  activeTabId?: string;
  revealActiveSignal: number;
  commandCompact: boolean;
  onTabChange: (tabId: string) => void;
  onTabClose: (tabId: string) => void;
  onTabsClose: (tabIds: string[], nextActiveTabId?: string) => void;
  onTabsReorder: (tabIds: string[]) => void;
  onNewTab: () => void;
  onOpenPalette: () => void;
}

export function AppChrome({
  platform,
  browserPreviewChrome,
  workbenchChrome = false,
  tabs,
  activeTabId,
  revealActiveSignal,
  commandCompact,
  onTabChange,
  onTabClose,
  onTabsClose,
  onTabsReorder,
  onNewTab,
  onOpenPalette,
}: AppChromeProps) {
  const t = useT();
  const darwinChrome = platform === "darwin";
  const showWindowsPreviewControls = browserPreviewChrome && platform === "windows";
  const chromeClassName = [
    "app-chrome",
    "app-chrome--tabs",
    darwinChrome ? "app-chrome--darwin-tabs" : "app-chrome--native-tabs",
    workbenchChrome ? "app-chrome--workbench" : "",
    !darwinChrome ? "app-chrome--identityless" : "",
    showWindowsPreviewControls ? "app-chrome--preview-window-controls" : "",
    `app-chrome--platform-${platform}`,
  ].filter(Boolean).join(" ");
  const tabBar = (
    <TabBar
      tabs={tabs}
      activeTabId={activeTabId}
      revealActiveSignal={revealActiveSignal}
      onTabChange={onTabChange}
      onTabClose={onTabClose}
      onTabsClose={onTabsClose}
      onTabsReorder={onTabsReorder}
      onNewTab={onNewTab}
      onOpenPalette={undefined}
      commandCompact={commandCompact}
    />
  );

  return (
    <header className={chromeClassName}>
      {browserPreviewChrome && darwinChrome && (
        <div className="app-chrome__traffic" aria-hidden="true">
          <span />
          <span />
          <span />
        </div>
      )}
      {darwinChrome && <span className="app-chrome__drag-rail" aria-hidden="true" />}
      {/* 左侧侧边栏折叠按钮已移除：沿用 Ctrl/Cmd+B 快捷键（与 VSCode 一致），界面上不再显示 */}
      {workbenchChrome && (
        <button
          className="app-chrome__workbench-search"
          type="button"
          onClick={onOpenPalette}
          aria-label={t("palette.placeholder")}
        >
          <Search size={18} />
        </button>
      )}

      {workbenchChrome ? (
        <span className="app-chrome__spacer" aria-hidden="true" />
      ) : darwinChrome ? (
        <>
          <div className="app-chrome__tab-strip app-chrome__tab-strip--darwin">
            {tabBar}
          </div>
          <div
            className={[
              "app-chrome__tools",
              "app-chrome__tools--fixed",
            ].filter(Boolean).join(" ")}
            aria-label={t("tabBar.commandSearch")}
          >
            <button
              className={[
                "tabbar__command",
                "tabbar__command--compact",
                "app-chrome__command",
              ].filter(Boolean).join(" ")}
              type="button"
              onClick={onOpenPalette}
              aria-label={t("palette.placeholder")}
              title={t("palette.placeholder")}
            >
              <Search size={16} className="tabbar__command-icon" />
            </button>
          </div>
        </>
      ) : (
        <>
          <div className="app-chrome__tab-strip app-chrome__tab-strip--native">
            {tabBar}
          </div>
          <div
            className={[
              "app-chrome__tools",
            ].filter(Boolean).join(" ")}
            aria-label={t("tabBar.commandSearch")}
          >
            <button
              className={[
                "tabbar__command",
                "tabbar__command--compact",
                "app-chrome__command",
              ].filter(Boolean).join(" ")}
              type="button"
              onClick={onOpenPalette}
              aria-label={t("palette.placeholder")}
              title={t("palette.placeholder")}
            >
              <Search size={16} className="tabbar__command-icon" />
            </button>
          </div>
        </>
      )}

      {/* 展开工作区按钮已移至 topicbar 命令按钮最右侧 */}

      {showWindowsPreviewControls && (
        <div className="app-chrome__window-controls app-chrome__window-controls--windows" aria-hidden="true">
          <span className="app-chrome__window-control app-chrome__window-control--minimize">
            <Minus size={12} strokeWidth={1.9} />
          </span>
          <span className="app-chrome__window-control app-chrome__window-control--maximize">
            <Square size={10} strokeWidth={1.8} />
          </span>
          <span className="app-chrome__window-control app-chrome__window-control--close">
            <X size={12} strokeWidth={1.9} />
          </span>
        </div>
      )}
    </header>
  );
}
