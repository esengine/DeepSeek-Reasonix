import { useCallback, useState } from "react";
import { createPortal } from "react-dom";
import { Plus, X } from "lucide-react";
import type { MouseEvent as ReactMouseEvent, RefObject } from "react";
import { DOCK_ENTRY_ICONS } from "../dockEntryIcons";
import { useT } from "../../lib/i18n";
import type { TabItem } from "../../store/activityBar";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "../ContextMenu";
import { useDockTabDrag } from "../../lib/useDockTabDrag";
import { TabOverviewMenu } from "./TabOverviewMenu";

interface TabBarProps {
  tabs: TabItem[];
  activeTabId: string | null;
  onActivate: (tabId: string) => void;
  onClose: (tabId: string) => void;
  onMoveTab: (fromId: string, toId: string, side: "left" | "right") => void;
  onAdd: () => void;
  addButtonRef: RefObject<HTMLButtonElement | null>;
}

export function TabBar({ tabs, activeTabId, onActivate, onClose, onMoveTab, onAdd, addButtonRef }: TabBarProps) {
  const t = useT();
  const [menuTabId, setMenuTabId] = useState<string | null>(null);
  const [menuPoint, setMenuPoint] = useState<ContextMenuPoint | null>(null);
  const {
    draggingTabId, dragElRefs, floatingRef, tabsRef, tabsOverflow, suppressClickRef, startTabDrag, clearDragState, ghost, slotWidthFor,
  } = useDockTabDrag({ tabs, onActivate, onMoveTab });

  const closeMenu = useCallback(() => {
    setMenuTabId(null);
    setMenuPoint(null);
  }, []);

  const openTabMenu = useCallback((event: ReactMouseEvent<HTMLDivElement>, tab: TabItem) => {
    event.preventDefault();
    event.stopPropagation();
    setMenuTabId(tab.id);
    setMenuPoint(contextMenuPointFromEvent(event));
  }, []);

  const menuTabIndex = menuTabId ? tabs.findIndex((tab) => tab.id === menuTabId) : -1;

  const closeThen = useCallback((action: () => void) => {
    closeMenu();
    action();
  }, [closeMenu]);

  const menuItems = useCallback((): ContextMenuItem[] => {
    const items: ContextMenuItem[] = [];
    items.push(
      {
        key: "close-current",
        label: t("tabBar.closeTab"),
        disabled: menuTabIndex < 0,
        onSelect: () => closeThen(() => { if (menuTabId) onClose(menuTabId); }),
      },
      {
        key: "close-others",
        label: t("tabBar.closeOtherTabs"),
        disabled: menuTabIndex < 0 || tabs.length <= 1,
        onSelect: () => closeThen(() => {
          if (!menuTabId) return;
          tabs.filter((tab) => tab.id !== menuTabId).forEach((tab) => onClose(tab.id));
          onActivate(menuTabId);
        }),
      },
      {
        key: "close-right",
        label: t("tabBar.closeTabsToRight"),
        disabled: menuTabIndex < 0 || menuTabIndex >= tabs.length - 1,
        onSelect: () => closeThen(() => {
          if (menuTabIndex < 0) return;
          tabs.slice(menuTabIndex + 1).forEach((tab) => onClose(tab.id));
          onActivate(menuTabId ?? tabs[menuTabIndex].id);
        }),
      },
    );
    return items;
  }, [closeThen, menuTabId, menuTabIndex, onActivate, onClose, t, tabs]);

  return (
    <div className="workbench-dock__tools">
      <TabOverviewMenu />
      <div
        ref={tabsRef}
        className={["workbench-dock__tabs", tabsOverflow ? "workbench-dock__tabs--overflow" : ""].filter(Boolean).join(" ")}
        role="tablist"
        aria-label={t("rightDock.views")}
      >
        {tabs.map((tab) => {
          const TabIcon = DOCK_ENTRY_ICONS[tab.type];
          const active = tab.id === activeTabId;
          const dragging = draggingTabId === tab.id;
          if (dragging) {
            const slotWidth = slotWidthFor(tab.id);
            return (
              <div
                key={tab.id}
                className="workbench-dock__tab-slot"
                style={{ width: slotWidth }}
                aria-hidden="true"
              />
            );
          }
          return (
            <div
              key={tab.id}
              ref={(node) => {
                if (node) dragElRefs.current.set(tab.id, node);
                else dragElRefs.current.delete(tab.id);
              }}
              role="tab"
              aria-selected={active}
              aria-label={tab.label}
              className={[
                "workbench-dock__tab",
                active ? "workbench-dock__tab--active" : "",
              ].filter(Boolean).join(" ")}
              onClick={() => {
                if (suppressClickRef.current) {
                  suppressClickRef.current = false;
                  return;
                }
                onActivate(tab.id);
              }}
              tabIndex={active ? 0 : -1}
              onContextMenu={(event) => openTabMenu(event, tab)}
              onKeyDown={(event) => {
                if (event.target !== event.currentTarget) return;
                if (event.key === "Enter" || event.key === " ") { event.preventDefault(); onActivate(tab.id); }
                if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) {
                  event.preventDefault();
                  const rect = event.currentTarget.getBoundingClientRect();
                  setMenuTabId(tab.id);
                  setMenuPoint({ left: rect.left, top: rect.bottom, keyboardTarget: event.currentTarget });
                }
                const index = tabs.indexOf(tab);
                const next = event.key === "ArrowRight" ? tabs[(index + 1) % tabs.length]
                  : event.key === "ArrowLeft" ? tabs[(index + tabs.length - 1) % tabs.length] : null;
                if (next) { event.preventDefault(); onActivate(next.id); dragElRefs.current.get(next.id)?.focus(); }
              }}
              onPointerDown={(event) => startTabDrag(event, tab.id)}
            >
              <TabIcon size={13} />
              <span className="workbench-dock__tab-label">{tab.label}</span>
              <button
                type="button"
                className="workbench-dock__tab-close"
                aria-label={t("rightDock.closeTab")}
                onPointerDown={(event) => event.stopPropagation()}
                onClick={(event) => {
                  event.stopPropagation();
                  onClose(tab.id);
                }}
              >
                <X size={12} />
              </button>
            </div>
          );
        })}
      </div>
      <button
        ref={addButtonRef}
        type="button"
        className="workbench-dock__tab-add"
        aria-label={t("rightDock.addTab")}
        onClick={onAdd}
      >
        <Plus size={14} />
      </button>
      {draggingTabId !== null && (() => {
        const draggedTab = tabs.find((tab) => tab.id === draggingTabId);
        if (!draggedTab) return null;
        const FloatIcon = DOCK_ENTRY_ICONS[draggedTab.type];
        return createPortal(
          <div
            ref={floatingRef}
            className={[
              "workbench-dock__tab",
              "workbench-dock__tab--floating",
              draggedTab.id === activeTabId ? "workbench-dock__tab--active" : "",
            ].filter(Boolean).join(" ")}
            role="presentation"
            style={{ left: ghost.left, top: ghost.top, width: ghost.width }}
          >
            <FloatIcon size={13} />
            <span className="workbench-dock__tab-label">{draggedTab.label}</span>
            {/* Keep the whole tab (including its close button) in the drag
                ghost so it looks like the tab itself is being dragged. */}
            <button
              type="button"
              className="workbench-dock__tab-close"
              aria-label={t("rightDock.closeTab")}
              onClick={(event) => {
                event.stopPropagation();
                clearDragState();
                onClose(draggedTab.id);
              }}
            >
              <X size={12} />
            </button>
          </div>,
          document.body,
        );
      })()}
      <ContextMenu
        open={menuPoint !== null}
        point={menuPoint}
        items={menuItems()}
        onClose={closeMenu}
        ariaLabel={t("rightDock.views")}
      />
    </div>
  );
}
