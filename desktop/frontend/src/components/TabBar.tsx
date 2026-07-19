// TabBar renders the browser-like workspace tab strip. Each tab represents one
// open project/global topic, so switching tabs switches the active conversation.
import { useEffect, useRef, useState } from "react";
import type { CSSProperties, DragEvent, KeyboardEvent as ReactKeyboardEvent, MouseEvent as ReactMouseEvent } from "react";
import { FileText, Plus, Search, X } from "lucide-react";
import { normalizeCollaborationMode, normalizeMode, normalizeToolApprovalMode, type Mode, type TabMeta } from "../lib/types";
import { projectColorValue } from "../lib/projectColors";
import { useT } from "../lib/i18n";
import { Tooltip } from "./Tooltip";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";
import { WorktreeBadge } from "./WorktreeBadge";

interface TabBarProps {
  tabs: TabMeta[];
  activeTabId?: string;
  onTabChange: (tabId: string) => void;
  onTabClose: (tabId: string) => void;
  onTabsClose: (tabIds: string[], nextActiveTabId?: string) => void;
  onTabsReorder: (tabIds: string[]) => void;
  onNewTab: () => void;
  onOpenPalette?: () => void;
  commandCompact?: boolean;
  revealActiveSignal?: number;
}

type DropSide = "before" | "after";

function tabDisplayTitle(tab: TabMeta): string {
  if (tab.tabType === "file" || tab.scope === "file") return tab.topicTitle?.trim() || tab.filePath?.split("/").filter(Boolean).pop() || "File";
  const title = tab.topicTitle?.trim();
  if (tab.scope === "global") return title || "Global";
  return title || "Untitled";
}

function tabFullTitle(tab: TabMeta): string {
  if (tab.tabType === "file" || tab.scope === "file") return tab.filePath || tabDisplayTitle(tab);
  if (tab.scope === "global") {
    const title = tabDisplayTitle(tab);
    const workspaceName = tab.workspaceName?.trim() || "Global";
    return title === workspaceName ? workspaceName : `${workspaceName} / ${title}`;
  }
  const workspaceName = tab.workspaceName?.trim() || "Project";
  return `${workspaceName} / ${tabDisplayTitle(tab)}`;
}

function tabMode(tab: TabMeta): Mode {
  return normalizeMode(tab.mode);
}

function projectAccentStyle(color?: string): CSSProperties | undefined {
  const value = projectColorValue(color);
  if (!value) return undefined;
  return { "--project-accent": value } as CSSProperties;
}

export function TabBar({ tabs, activeTabId, onTabChange, onTabClose, onTabsClose, onTabsReorder, onNewTab, onOpenPalette, commandCompact = false, revealActiveSignal = 0 }: TabBarProps) {
  const t = useT();
  const [draggingTabId, setDraggingTabId] = useState<string | null>(null);
  const [dropTarget, setDropTarget] = useState<{ id: string; side: DropSide } | null>(null);
  const [menuTabId, setMenuTabId] = useState<string | null>(null);
  const [menuPoint, setMenuPoint] = useState<ContextMenuPoint | null>(null);
  const suppressClickRef = useRef(false);
  const tabRefs = useRef(new Map<string, HTMLButtonElement>());
  const backendActiveTabId = tabs.find((tab) => tab.active)?.id;
  const activeTabIdExists = Boolean(activeTabId && tabs.some((tab) => tab.id === activeTabId));
  const resolvedActiveTabId = activeTabIdExists ? activeTabId : backendActiveTabId;
  const tabOrderKey = tabs.map((tab) => tab.id).join("\u0000");

  useEffect(() => {
    if (!resolvedActiveTabId) return;
    const frame = window.requestAnimationFrame(() => {
      tabRefs.current.get(resolvedActiveTabId)?.scrollIntoView({
        block: "nearest",
        inline: "nearest",
      });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [backendActiveTabId, resolvedActiveTabId, revealActiveSignal, tabOrderKey]);

  const handleClose = (tabId: string) => {
    onTabClose(tabId);
  };

  const clearDragState = () => {
    setDraggingTabId(null);
    setDropTarget(null);
  };

  const dropSideForEvent = (event: DragEvent<HTMLButtonElement>): DropSide => {
    const rect = event.currentTarget.getBoundingClientRect();
    return event.clientX > rect.left + rect.width / 2 ? "after" : "before";
  };

  const reorderTabIds = (draggedId: string, targetId: string, side: DropSide): string[] => {
    const ids = tabs.map((tab) => tab.id);
    const from = ids.indexOf(draggedId);
    const target = ids.indexOf(targetId);
    if (from < 0 || target < 0 || draggedId === targetId) return ids;
    const next = ids.filter((id) => id !== draggedId);
    const targetAfterRemoval = next.indexOf(targetId);
    const insertAt = side === "after" ? targetAfterRemoval + 1 : targetAfterRemoval;
    next.splice(insertAt, 0, draggedId);
    return next;
  };

  const handleDragStart = (event: DragEvent<HTMLButtonElement>, tabId: string) => {
    setDraggingTabId(tabId);
    setDropTarget(null);
    event.dataTransfer.effectAllowed = "move";
    event.dataTransfer.setData("text/plain", tabId);
  };

  const handleDragOver = (event: DragEvent<HTMLButtonElement>, tabId: string) => {
    if (!draggingTabId || draggingTabId === tabId) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
    setDropTarget({ id: tabId, side: dropSideForEvent(event) });
  };

  const handleDrop = (event: DragEvent<HTMLButtonElement>, tabId: string) => {
    event.preventDefault();
    const draggedId = draggingTabId || event.dataTransfer.getData("text/plain");
    const side = dropTarget?.id === tabId ? dropTarget.side : dropSideForEvent(event);
    clearDragState();
    if (!draggedId || draggedId === tabId) return;
    const next = reorderTabIds(draggedId, tabId, side);
    if (next.join("\u0000") !== tabs.map((tab) => tab.id).join("\u0000")) {
      suppressClickRef.current = true;
      onTabsReorder(next);
    }
  };

  const handleTabClick = (tabId: string) => {
    if (suppressClickRef.current) {
      suppressClickRef.current = false;
      return;
    }
    onTabChange(tabId);
  };

  const openTabMenu = (event: ReactMouseEvent<HTMLButtonElement> | ReactKeyboardEvent<HTMLButtonElement>, tabId: string) => {
    event.preventDefault();
    event.stopPropagation();
    setMenuTabId(tabId);
    setMenuPoint(contextMenuPointFromEvent(event));
  };

  const closeTabMenu = () => {
    setMenuTabId(null);
    setMenuPoint(null);
  };

  const closeTabsFromMenu = (tabIds: string[], nextActiveTabId?: string) => {
    closeTabMenu();
    onTabsClose(tabIds, nextActiveTabId);
  };

  const menuTabIndex = menuTabId ? tabs.findIndex((tab) => tab.id === menuTabId) : -1;
  const tabMenuItems: ContextMenuItem[] = menuTabId && menuTabIndex >= 0
    ? [
        {
          key: "close-current",
          label: t("tabBar.closeTab"),
          disabled: tabs.length <= 1,
          onSelect: () => closeTabsFromMenu([menuTabId]),
        },
        {
          key: "close-other",
          label: t("tabBar.closeOtherTabs"),
          disabled: tabs.length <= 1,
          onSelect: () => closeTabsFromMenu(tabs.filter((tab) => tab.id !== menuTabId).map((tab) => tab.id), menuTabId),
        },
        {
          key: "close-right",
          label: t("tabBar.closeTabsToRight"),
          disabled: menuTabIndex >= tabs.length - 1,
          onSelect: () => {
            const rightTabIds = tabs.slice(menuTabIndex + 1).map((tab) => tab.id);
            const nextActiveTabId = resolvedActiveTabId && rightTabIds.includes(resolvedActiveTabId) ? menuTabId : undefined;
            closeTabsFromMenu(rightTabIds, nextActiveTabId);
          },
        },
      ]
    : [];

  return (
    <div className="tabbar">
      <div className="tabbar__tabs">
        {tabs.map((tab) => {
          const displayTitle = tabDisplayTitle(tab);
          const fullTitle = tabFullTitle(tab);
          const mode = tabMode(tab);
          const collaborationMode = normalizeCollaborationMode(tab.collaborationMode, tab.goal, mode);
          const planMode = collaborationMode === "plan";
          const goalMode = collaborationMode === "goal";
          const toolApprovalMode = normalizeToolApprovalMode(tab.toolApprovalMode, mode);
          const stateTitle = [
            tab.running ? "Running" : "",
            planMode ? "Plan" : "",
            goalMode ? "Goal" : "",
            toolApprovalMode === "auto" ? "Auto approve" : "",
            toolApprovalMode === "yolo" ? "YOLO approval" : "",
          ].filter(Boolean).join(" · ");
          const annotatedTitle = stateTitle ? `${stateTitle} · ${fullTitle}` : fullTitle;
          return (
            <button
              key={tab.id}
              ref={(node) => {
                if (node) {
                  tabRefs.current.set(tab.id, node);
                } else {
                  tabRefs.current.delete(tab.id);
                }
              }}
              draggable
              className={[
                "tabbar__tab",
                tab.id === resolvedActiveTabId ? "tabbar__tab--active" : "",
                tab.running ? "tabbar__tab--running" : "",
                toolApprovalMode === "yolo" ? "tabbar__tab--yolo" : "",
                draggingTabId === tab.id ? "tabbar__tab--dragging" : "",
                dropTarget?.id === tab.id ? `tabbar__tab--drop-${dropTarget.side}` : "",
              ].filter(Boolean).join(" ")}
              title={annotatedTitle}
              aria-label={annotatedTitle}
              style={projectAccentStyle(tab.projectColor)}
              onClick={() => handleTabClick(tab.id)}
              onContextMenu={(event) => openTabMenu(event, tab.id)}
              onKeyDown={(event) => {
                if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) {
                  openTabMenu(event, tab.id);
                }
              }}
              onDragStart={(event) => handleDragStart(event, tab.id)}
              onDragOver={(event) => handleDragOver(event, tab.id)}
              onDrop={(event) => handleDrop(event, tab.id)}
              onDragEnd={clearDragState}
            >
              {tab.tabType === "file" || tab.scope === "file" ? (
                <FileText size={12} className="tabbar__file-icon" />
              ) : (
                <span
                  className={[
                    "tabbar__status",
                    tab.running ? "tabbar__status--running" : "",
                  ].filter(Boolean).join(" ")}
                />
              )}
              <span className="tabbar__tab-label">{displayTitle}</span>
              {tab.isolatedWorktree && <WorktreeBadge size={11} />}
              {planMode && <span className="tabbar__mode-badge tabbar__mode-badge--plan">plan</span>}
              {goalMode && <span className="tabbar__mode-badge tabbar__mode-badge--plan">goal</span>}
              {toolApprovalMode === "auto" && <span className="tabbar__mode-badge tabbar__mode-badge--plan">auto</span>}
              {toolApprovalMode === "yolo" && <span className="tabbar__mode-badge tabbar__mode-badge--yolo">yolo</span>}
              <span
                className="tabbar__tab-close"
                onClick={(e) => {
                  e.stopPropagation();
                  handleClose(tab.id);
                }}
              >
                <X size={10} />
              </span>
            </button>
          );
        })}
      </div>
      <Tooltip label={t("tabBar.newSession")}>
        <button className="tabbar__new" type="button" aria-label={t("tabBar.newSession")} onClick={onNewTab}>
          <Plus size={13} />
        </button>
      </Tooltip>
      {onOpenPalette && <span className="tabbar__spacer" aria-hidden="true" />}
      {onOpenPalette && (
        <button
          className={["tabbar__command", commandCompact ? "tabbar__command--compact" : ""].filter(Boolean).join(" ")}
          type="button"
          onClick={onOpenPalette}
          aria-label={t("palette.placeholder")}
          title={t("palette.placeholder")}
        >
          <Search size={commandCompact ? 16 : 13} className="tabbar__command-icon" />
          {!commandCompact && (
            <>
              <span className="tabbar__command-text tabbar__command-text--full">{t("tabBar.commandSearch")}</span>
              <span className="tabbar__command-text tabbar__command-text--compact">{t("tabBar.commandSearchCompact")}</span>
              <kbd className="tabbar__command-kbd">⌘K</kbd>
            </>
          )}
        </button>
      )}
      <ContextMenu
        open={Boolean(menuTabId)}
        point={menuPoint}
        items={tabMenuItems}
        minWidth={170}
        ariaLabel={t("tabBar.tabActions")}
        onClose={closeTabMenu}
      />
    </div>
  );
}
