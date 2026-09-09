// TabBar is the dock's header row. It keeps the upstream workbench-dock
// chrome (a draggable tools strip + the rounded tab capsule) so the header
// height and tab styling match the original; the only addition is a per-tab
// close button and a + button that opens the add-tab menu.
//
// File tabs mirror the workspace panel's current preview: a single tab whose
// label shows the open file's name (or 文件 while no file is selected). Every
// tab carries a leading type icon so the view kind is recognizable at a
// glance.
//
// File tabs also get a context menu (VS Code style): open with the default
// app or a listed opener, save as, copy path / content, reveal in Finder,
// plus close / close-others / close-to-right. The file operations go through
// the workspace-scoped bridge methods, so they need the active session tab id.

import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Activity, FileText, GitBranch, Plus, X } from "lucide-react";
import type { ComponentType, MouseEvent as ReactMouseEvent, PointerEvent as ReactPointerEvent, RefObject } from "react";
import { WorkspaceFileIcon } from "../WorkspaceFileIcon";
import { app } from "../../lib/bridge";
import { writeClipboardText } from "../../lib/clipboard";
import { useT } from "../../lib/i18n";
import { useToast } from "../../lib/toast";
import type { ExternalOpenersView } from "../../lib/types";
import { useActivityBarStore, type TabItem } from "../../store/activityBar";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "../ContextMenu";

// Type icon map: every tab type gets a leading icon so the tab kind is
// identifiable (mirrors the launcher's icon assignment).
const TAB_TYPE_ICONS: Record<string, ComponentType<{ size?: number | string }>> = {
  file: FileText,
  changed: GitBranch,
  context: Activity,
  remote: FileText,
  instructions: FileText,
  terminal: FileText,
  browser: FileText,
};

// Matches .workbench-dock__tabs' theme CSS gap; the drag layout math inserts
// the slot between tabs with the same spacing so it never overlaps a neighbor.
// The theme strip's gap is 8px (the base non-theme rule is 3px, but the
// theme skin is this component's primary scenario).
const DOCK_TAB_GAP = 8;

function localPathErrorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

interface TabBarProps {
  tabs: TabItem[];
  activeTabId: string | null;
  onActivate: (tabId: string) => void;
  onClose: (tabId: string) => void;
  onMoveTab: (fromId: string, toId: string, side: "left" | "right") => void;
  onAdd: () => void;
  addButtonRef: RefObject<HTMLButtonElement | null>;
  /** Active session tab id — required for workspace-scoped file operations. */
  workspaceTabId?: string;
}

export function TabBar({ tabs, activeTabId, onActivate, onClose, onMoveTab, onAdd, addButtonRef, workspaceTabId }: TabBarProps) {
  const t = useT();
  const { showToast } = useToast();
  const [menuTabId, setMenuTabId] = useState<string | null>(null);
  const [menuPoint, setMenuPoint] = useState<ContextMenuPoint | null>(null);
  const [openers, setOpeners] = useState<ExternalOpenersView>({ openers: [], preferred: "" });
  const [draggingTabId, setDraggingTabId] = useState<string | null>(null);
  // Mirrors draggingTabId but updates synchronously at drag start, so the
  // pointer handlers branch correctly even before React repaints (a repaint
  // delayed by the activation switch would otherwise keep resetting the
  // previous-pointer anchor and edge crossings would never fire).
  const draggingTabIdRef = useRef<string | null>(null);
  const dragStartXRef = useRef(0);
  const dragStartYRef = useRef(0);
  const dragPressedTabRef = useRef<string | null>(null);
  const dragMovedRef = useRef(false);
  const dragOffsetRef = useRef(0);
  const dragOffsetYRef = useRef(0);
  // The floating ghost's position is written straight to its DOM transform
  // (rAF-throttled) instead of React state: a setState on every pointermove
  // re-renders the whole tab strip per frame, which is the drag jank. Reorder
  // checks read refs, so they stay frame-accurate regardless.
  const dragRafRef = useRef(0);
  const floatingRef = useRef<HTMLDivElement | null>(null);
  // The right-edge fade overlay must only appear when the strip actually
  // overflows (every tab at its 85px min and still too wide). A width:
  // max-content strip's right edge sits at the last tab, so an always-on
  // overlay would fade the last tab even with plenty of room.
  const tabsRef = useRef<HTMLDivElement | null>(null);
  const [tabsOverflow, setTabsOverflow] = useState(false);
  const dragBaseLeftRef = useRef(new Map<string, number>());
  const dragBaseTopRef = useRef(new Map<string, number>());
  const dragBaseWidthRef = useRef(new Map<string, number>());
  // The floating ghost's anchor: the dragged tab's base position when the
  // drag started. Reorders change dragBaseLeftRef (live layout math), but the
  // ghost must keep following the pointer from where it started.
  const dragStartLeftRef = useRef(0);
  const dragStartTopRef = useRef(0);
  const dragContainerLeftRef = useRef(0);
  const dragContainerTopRef = useRef(0);
  // Pointer X of the previous pointermove (container-relative), used to
  // detect edge crossings instead of "inside the box" so reordering is
  // symmetric: dragging back across the same edge swaps the tabs back.
  const lastPointerXRef = useRef<number | null>(null);
  // Last measured left of each rendered tab, for the FLIP slide animation:
  // when tabs reorder (or are added/removed) the layout jumps instantly, so
  // we pin each moved tab at its previous left via transform and let the
  // transition glide it into place.
  const prevTabLeftRef = useRef(new Map<string, number>());
  // Pending FLIP release frames per element, so a new reorder cancels the
  // previous release and no transform is ever left pinned (a stuck transform
  // would make tabs look offset after unrelated re-renders).
  const flipRafIdsRef = useRef(new Map<HTMLElement, number>());
  const dragElRefs = useRef(new Map<string, HTMLDivElement>());
  const suppressClickRef = useRef(false);
  const openerRequestRef = useRef(0);
  const mountedRef = useRef(true);

  // React StrictMode replays mount effects in development; reset the guard so
  // the replayed mount can still accept opener discoveries.
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      if (dragRafRef.current) {
        cancelAnimationFrame(dragRafRef.current);
        dragRafRef.current = 0;
      }
      openerRequestRef.current += 1;
    };
  }, []);

  const closeMenu = useCallback(() => {
    setMenuTabId(null);
    setMenuPoint(null);
  }, []);

  const refreshOpeners = useCallback(() => {
    if (!workspaceTabId) return;
    const request = ++openerRequestRef.current;
    void app.ExternalOpenersForTab(workspaceTabId).then((next) => {
      if (!mountedRef.current || request !== openerRequestRef.current) return;
      setOpeners({
        openers: Array.isArray(next.openers) ? next.openers : [],
        preferred: next.preferred ?? "",
      });
    }).catch(() => {});
  }, [workspaceTabId]);

  const openFileTabMenu = useCallback((event: ReactMouseEvent<HTMLDivElement>, tab: TabItem) => {
    event.preventDefault();
    event.stopPropagation();
    setMenuTabId(tab.id);
    setMenuPoint(contextMenuPointFromEvent(event));
    refreshOpeners();
  }, [refreshOpeners]);

  const clearDragState = useCallback(() => {
    if (dragRafRef.current) {
      cancelAnimationFrame(dragRafRef.current);
      dragRafRef.current = 0;
    }
    setDraggingTabId(null);
    draggingTabIdRef.current = null;
    dragOffsetRef.current = 0;
    dragOffsetYRef.current = 0;
    dragPressedTabRef.current = null;
    lastPointerXRef.current = null;
  }, []);

  // The window listeners installed at drag start must read the *latest*
  // handlers, not the render in which startTabDrag ran. Keep a stable pair
  // of window callbacks that forward through refs so pointerup always lands
  // in the current handler and clears the floating layer.
  const dragMoveHandlerRef = useRef<(event: PointerEvent) => void>(() => {});
  const dragUpHandlerRef = useRef<() => void>(() => {});

  const onWindowPointerMove = useCallback((event: PointerEvent) => {
    dragMoveHandlerRef.current(event);
  }, []);

  const onWindowPointerUp = useCallback(() => {
    dragUpHandlerRef.current();
  }, []);

  const startTabDrag = useCallback((event: ReactPointerEvent<HTMLDivElement>, tabId: string) => {
    if (event.button !== 0 || event.pointerType === "touch") return;
    event.preventDefault();
    dragPressedTabRef.current = tabId;
    dragStartXRef.current = event.clientX;
    dragStartYRef.current = event.clientY;
    dragMovedRef.current = false;
    // Capture each tab's layout position and width (no transform) so the
    // floating layer and the slot both use stable, unshifted coordinates.
    const container = dragElRefs.current.get(tabId)?.parentElement;
    const containerRect = container?.getBoundingClientRect();
    const baseLeft = new Map<string, number>();
    const baseTop = new Map<string, number>();
    const baseWidth = new Map<string, number>();
    if (containerRect) {
      for (const [id, el] of dragElRefs.current) {
        const rect = el.getBoundingClientRect();
        baseLeft.set(id, rect.left - containerRect.left);
        baseTop.set(id, rect.top - containerRect.top);
        baseWidth.set(id, el.offsetWidth);
      }
    }
    dragBaseLeftRef.current = baseLeft;
    dragBaseTopRef.current = baseTop;
    dragBaseWidthRef.current = baseWidth;
    dragContainerLeftRef.current = containerRect?.left ?? 0;
    dragContainerTopRef.current = containerRect?.top ?? 0;
    dragStartLeftRef.current = baseLeft.get(tabId) ?? 0;
    dragStartTopRef.current = baseTop.get(tabId) ?? 0;
    // Listen on the window so the gesture survives the dragged tab leaving
    // the strip (its element is replaced by the slot mid-drag).
    window.addEventListener("pointermove", onWindowPointerMove);
    window.addEventListener("pointerup", onWindowPointerUp);
    window.addEventListener("pointercancel", onWindowPointerUp);
  }, [onWindowPointerMove, onWindowPointerUp]);

  // After a live reorder the tabs' DOM order changed but React may not have
  // repainted yet; rebuild the left coordinates from the store's new order so
  // the pointer math stays correct frame-to-frame (widths never change).
  const recomputeDragBase = useCallback(() => {
    const order = useActivityBarStore.getState().tabs;
    const left = new Map<string, number>();
    let x = 0;
    for (const tab of order) {
      left.set(tab.id, x);
      x += (dragBaseWidthRef.current.get(tab.id) ?? 0) + DOCK_TAB_GAP;
    }
    dragBaseLeftRef.current = left;
  }, []);

  // Live reorder: reordering triggers when the pointer CROSSES a neighbor
  // tab's edge (enters its box from either side), not when it merely hovers
  // inside — so dragging back across the same edge swaps the tabs back.
  // The swap direction follows where the dragged tab currently sits relative
  // to the crossed tab (behind → move before it; ahead → move after it).
  // Tab widths are measured live (they flex-compress when the strip is tight).
  const maybeReorder = useCallback((fromId: string, pointerX: number) => {
    const order = useActivityBarStore.getState().tabs;
    const dragIndex = order.findIndex((tab) => tab.id === fromId);
    if (dragIndex < 0) return;
    const previousX = lastPointerXRef.current;
    lastPointerXRef.current = pointerX;
    if (previousX === null) return;
    for (let i = 0; i < order.length; i++) {
      const tab = order[i];
      if (tab.id === fromId) continue;
      const otherLeft = dragBaseLeftRef.current.get(tab.id);
      if (otherLeft === undefined) continue;
      const otherRight = otherLeft + (dragBaseWidthRef.current.get(tab.id) ?? 0);
      // Entered from the right (pointer crossed the tab's right edge) or from
      // the left (crossed its left edge) since the previous move.
      const crossedInto = (previousX >= otherRight && pointerX < otherRight)
        || (previousX <= otherLeft && pointerX > otherLeft);
      if (!crossedInto) continue;
      const tabIndex = order.findIndex((entry) => entry.id === tab.id);
      // Swap direction: the dragged tab sits behind the crossed tab
      // (dragIndex > tabIndex) → move before it; ahead → move after it.
      // This is what makes both crossing directions swap correctly.
      const side: "left" | "right" = dragIndex > tabIndex ? "left" : "right";
      const toId = tab.id;
      if (toId === fromId) return;
      const without = order.filter((entry) => entry.id !== fromId);
      const targetIndex = without.findIndex((entry) => entry.id === toId);
      const insertAt = side === "right" ? targetIndex + 1 : targetIndex;
      const predicted = [...without];
      predicted.splice(insertAt, 0, order[dragIndex]);
      const predictedIndex = predicted.findIndex((entry) => entry.id === fromId);
      if (predictedIndex === dragIndex) return;
      onMoveTab(fromId, toId, side);
      recomputeDragBase();
      return;
    }
  }, [onMoveTab, recomputeDragBase]);

  const handleWindowPointerMove = useCallback((event: PointerEvent) => {
    const tabId = dragPressedTabRef.current;
    if (!tabId) return;
    const dx = event.clientX - dragStartXRef.current;
    const dy = event.clientY - dragStartYRef.current;
    // A plain click (press + release without moving) must not enter the
    // dragging state: only cross the threshold before floating the tab.
    if (draggingTabIdRef.current !== tabId) {
      if (Math.abs(dx) <= 4 && Math.abs(dy) <= 4) return;
      dragMovedRef.current = true;
      suppressClickRef.current = true;
      dragOffsetRef.current = dx;
      lastPointerXRef.current = event.clientX - dragContainerLeftRef.current;
      // Dragging a tab makes it the active one (same as a plain click would).
      onActivate(tabId);
      draggingTabIdRef.current = tabId;
      // Baseline the FLIP positions at drag start so the first reorder
      // animates too.
      const dragStartPositions = new Map<string, number>();
      for (const [id, el] of dragElRefs.current) dragStartPositions.set(id, el.offsetLeft);
      prevTabLeftRef.current = dragStartPositions;
      setDraggingTabId(tabId);
      return;
    }
    dragOffsetRef.current = dx;
    dragOffsetYRef.current = dy;
    if (!dragRafRef.current) {
      dragRafRef.current = requestAnimationFrame(() => {
        dragRafRef.current = 0;
        const el = floatingRef.current;
        if (el) {
          el.style.transform = `translate3d(${dragOffsetRef.current}px, ${dragOffsetYRef.current}px, 0)`;
        }
      });
    }
    // Live reorder while dragging: crossing a neighbor tab's edge moves the
    // tab in the store so the strip reflows immediately.
    maybeReorder(tabId, event.clientX - dragContainerLeftRef.current);
  }, [draggingTabId, maybeReorder]);

  const handleWindowPointerUp = useCallback(() => {
    const tabId = dragPressedTabRef.current;
    if (!tabId) return;
    window.removeEventListener("pointermove", onWindowPointerMove);
    window.removeEventListener("pointerup", onWindowPointerUp);
    window.removeEventListener("pointercancel", onWindowPointerUp);
    dragPressedTabRef.current = null;
    // A click without motion never entered the dragging state, so the native
    // click activation runs untouched. Otherwise the live reorder already
    // settled the final order during pointermove — just tear the drag down.
    if (draggingTabIdRef.current !== tabId) return;
    clearDragState();
    // Drag start set suppressClickRef to swallow the click that trails a
    // release. If the pointer came up outside any tab (or the gesture was
    // cancelled) no click fires, so clear the flag on the next tick — the
    // trailing click, if any, has already been consumed by then, and a stale
    // true would swallow the user's next real tab click.
    window.setTimeout(() => {
      suppressClickRef.current = false;
    }, 0);
  }, [clearDragState, draggingTabId, onWindowPointerMove, onWindowPointerUp]);

  // Keep the forwarding refs pointing at the current handlers every render.
  dragMoveHandlerRef.current = handleWindowPointerMove;
  dragUpHandlerRef.current = handleWindowPointerUp;

  // Watch the strip for overflow (tab count / width / dock width changes) so
  // the fade overlay only shows while content is actually clipped.
  useEffect(() => {
    const el = tabsRef.current;
    if (!el) return;
    const update = () => {
      const next = el.scrollWidth > el.clientWidth + 1;
      setTabsOverflow((prev) => (prev === next ? prev : next));
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // The ghost mounts at its base position; snap it to the pointer offset the
  // moment it appears (the drag-start pointermove returned before the portal
  // painted, so its offset is only in the refs here).
  useLayoutEffect(() => {
    if (draggingTabId && floatingRef.current) {
      floatingRef.current.style.transform =
        `translate3d(${dragOffsetRef.current}px, ${dragOffsetYRef.current}px, 0)`;
    }
  }, [draggingTabId]);

  // FLIP slide: while dragging, after a reorder repaints, move each tab whose
  // left changed back to its previous position (transition disabled), then
  // release it on the next frame so the 120ms transform transition glides it
  // into place. Dragged tabs render as slots (absent from dragElRefs), so the
  // floating ghost is untouched. Only runs during a drag: activation clicks /
  // file-tab label updates also change the tabs array but must never animate.
  // Positions come from offsetLeft (layout, transform-free) so an in-flight
  // or stuck transform can't poison the delta and re-trigger a phantom slide.
  useLayoutEffect(() => {
    if (!draggingTabIdRef.current) return;
    const prev = prevTabLeftRef.current;
    const next = new Map<string, number>();
    for (const [id, el] of dragElRefs.current) {
      const left = el.offsetLeft;
      next.set(id, left);
      const oldLeft = prev.get(id);
      if (oldLeft === undefined || Math.abs(oldLeft - left) < 1) continue;
      const delta = oldLeft - left;
      const pending = flipRafIdsRef.current.get(el);
      if (pending !== undefined) cancelAnimationFrame(pending);
      el.style.transition = "none";
      el.style.transform = `translateX(${delta}px)`;
      const rafId = requestAnimationFrame(() => {
        flipRafIdsRef.current.delete(el);
        el.style.transition = "";
        el.style.transform = "";
      });
      flipRafIdsRef.current.set(el, rafId);
    }
    prevTabLeftRef.current = next;
  }, [tabs]);

  // Unmount: release any pinned FLIP transforms so tabs never stay offset.
  useEffect(() => {
    return () => {
      for (const rafId of flipRafIdsRef.current.values()) cancelAnimationFrame(rafId);
      for (const el of flipRafIdsRef.current.keys()) {
        el.style.transition = "";
        el.style.transform = "";
      }
      flipRafIdsRef.current.clear();
    };
  }, []);

  // Drop the window listeners if the component unmounts mid-gesture (the
  // dragged tab's element is replaced by the slot, so listeners can outlive
  // the tab that started them).
  useEffect(() => {
    return () => {
      window.removeEventListener("pointermove", onWindowPointerMove);
      window.removeEventListener("pointerup", onWindowPointerUp);
      window.removeEventListener("pointercancel", onWindowPointerUp);
    };
  }, [onWindowPointerMove, onWindowPointerUp]);

  const menuTabIndex = menuTabId ? tabs.findIndex((tab) => tab.id === menuTabId) : -1;  const menuTab = menuTabId ? tabs.find((tab) => tab.id === menuTabId) ?? null : null;
  const menuPath = typeof menuTab?.meta?.path === "string" ? menuTab.meta.path : null;

  const closeThen = useCallback((action: () => void) => {
    closeMenu();
    action();
  }, [closeMenu]);

  const openWith = useCallback((openerId: string, openerName: string) => {
    if (!workspaceTabId || !menuPath) return;
    closeThen(() => {
      void app.OpenWorkspaceInExternalOpenerForTab(workspaceTabId, openerId).catch((error) => {
        showToast(t("externalOpener.failed", { name: openerName, error: localPathErrorText(error) }), "error");
      });
    });
  }, [closeThen, menuPath, showToast, t, workspaceTabId]);

  const menuItems = useCallback((): ContextMenuItem[] => {
    const items: ContextMenuItem[] = [];
    if (menuPath && workspaceTabId) {
      const openerItems: ContextMenuItem[] = openers.openers.filter((opener) => opener.kind !== "file-manager").map((opener) => ({
        key: `open-with-${opener.id}`,
        label: t("externalOpener.openIn", { name: opener.name }),
        onSelect: () => openWith(opener.id, opener.name),
      }));
      items.push(
        {
          key: "open-default",
          label: t("externalOpener.openDefault"),
          onSelect: () => closeThen(() => {
            void app.OpenWorkspacePathForTab(workspaceTabId, menuPath).catch((error) => {
              showToast(t("externalOpener.failed", { name: t("externalOpener.openDefault"), error: localPathErrorText(error) }), "error");
            });
          }),
        },
        ...(openerItems.length > 0
          ? [{
              key: "open-with",
              label: t("externalOpener.openWith"),
              children: openerItems,
            }]
          : []),
        { type: "separator" as const, key: "file-actions-separator" },
        {
          key: "reveal",
          label: t("workspace.revealInFileManager"),
          onSelect: () => closeThen(() => {
            void app.RevealWorkspacePathForTab(workspaceTabId, menuPath).catch((error) => {
              showToast(t("externalOpener.failed", { name: t("workspace.revealInFileManager"), error: localPathErrorText(error) }), "error");
            });
          }),
        },
        {
          key: "save-as",
          label: t("externalOpener.saveAs"),
          onSelect: () => closeThen(() => {
            void app.ResolveWorkspacePathForTab(workspaceTabId, menuPath)
              .then((absolutePath) => app.SaveLocalPathAs(absolutePath))
              .then((savedPath) => {
                if (savedPath) showToast(t("externalOpener.saved", { path: savedPath }), "info");
              })
              .catch((error) => {
                showToast(t("externalOpener.failed", { name: t("externalOpener.saveAs"), error: localPathErrorText(error) }), "error");
              });
          }),
        },
        {
          key: "copy-relative-path",
          label: t("workspace.copyRelativePath"),
          onSelect: () => closeThen(() => { void writeClipboardText(menuPath); }),
        },
        {
          key: "copy-absolute-path",
          label: t("workspace.copyAbsolutePath"),
          onSelect: () => closeThen(() => {
            void app.ResolveWorkspacePathForTab(workspaceTabId, menuPath).then((absolutePath) => {
              if (absolutePath) void writeClipboardText(absolutePath);
            }).catch(() => {});
          }),
        },
        {
          key: "copy-content",
          label: t("workspace.copyFileContent"),
          onSelect: () => closeThen(() => {
            void app.ReadFileForTab(workspaceTabId, menuPath).then((preview) => {
              if (preview?.body) {
                void writeClipboardText(preview.body);
                showToast(t("workspace.fileContentCopied"), "info");
              }
            }).catch((error) => {
              showToast(t("externalOpener.failed", { name: t("workspace.copyFileContent"), error: localPathErrorText(error) }), "error");
            });
          }),
        },
        { type: "separator" as const, key: "tab-actions-separator" },
      );
    }
    items.push(
      {
        key: "close-current",
        label: t("tabBar.closeTab"),
        disabled: tabs.length <= 1,
        onSelect: () => closeThen(() => { if (menuTabId) onClose(menuTabId); }),
      },
      {
        key: "close-others",
        label: t("tabBar.closeOtherTabs"),
        disabled: tabs.length <= 1,
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
  }, [closeThen, menuPath, menuTabId, menuTabIndex, onActivate, onClose, openWith, openers.openers, showToast, t, tabs, workspaceTabId]);

  return (
    <div className="workbench-dock__tools">
      <div
        ref={tabsRef}
        className={["workbench-dock__tabs", tabsOverflow ? "workbench-dock__tabs--overflow" : ""].filter(Boolean).join(" ")}
        role="tablist"
        aria-label={t("rightDock.views")}
      >
        {/* Tabs render in store order (insertion order), so a newly added file
            tab appears at the end. File tabs carry the context menu; file
            tabs with a preview path show the file name, ones without show
            文件 (the file-list view awaiting a selection). */}
        {tabs.map((tab) => {
          const TabIcon = TAB_TYPE_ICONS[tab.type] ?? FileText;
          const active = tab.id === activeTabId;
          const dragging = draggingTabId === tab.id;
          if (dragging) {
            const slotWidth = dragBaseWidthRef.current.get(tab.id) ?? 0;
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
              onContextMenu={tab.type === "file" ? (event) => openFileTabMenu(event, tab) : undefined}
              onPointerDown={(event) => startTabDrag(event, tab.id)}
            >
              {tab.type === "file" ? (
                <WorkspaceFileIcon fileName={tab.label} />
              ) : (
                <TabIcon size={13} />
              )}
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
        const FloatIcon = TAB_TYPE_ICONS[draggedTab.type] ?? FileText;
        return createPortal(
          <div
            ref={floatingRef}
            className={[
              "workbench-dock__tab",
              "workbench-dock__tab--floating",
              draggedTab.id === activeTabId ? "workbench-dock__tab--active" : "",
            ].filter(Boolean).join(" ")}
            role="presentation"
            style={{
              left: dragContainerLeftRef.current + dragStartLeftRef.current,
              top: dragContainerTopRef.current + dragStartTopRef.current,
              width: dragBaseWidthRef.current.get(draggingTabId) ?? 0,
            }}
          >
            {draggedTab.type === "file" ? (
              <WorkspaceFileIcon fileName={draggedTab.label} />
            ) : (
              <FloatIcon size={13} />
            )}
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
        ariaLabel={t("rightDock.fileTabMenu")}
      />
    </div>
  );
}
