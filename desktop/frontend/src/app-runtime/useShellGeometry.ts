import { useEffect, useRef, type KeyboardEvent, type PointerEvent as ReactPointerEvent, type RefObject } from "react";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { createPointerResizeLifecycle, createRafResizeUpdater } from "../lib/resizeDrag";
import { availableWorkspacePanelWidth, resolveLiveWorkspacePanelWidth, resolveWorkspacePanelPlacement } from "../lib/workspaceLayout";
import { useDesktopPreferences } from "./useDesktopPreferences";
import { useOverlayStore } from "../store/overlays";
import { useWindowChromeStore } from "../store/windowChrome";
import {
  clampCreationRightDockTreeWidth,
  clampCreationSidebarWidth,
  clampRightDockTreeWidth,
  clampSidebarWidth,
  clampTerminalHeight,
  CREATION_RIGHT_DOCK_MIN_RENDER_WIDTH,
  CREATION_RIGHT_DOCK_TREE_MIN_WIDTH,
  CREATION_SIDEBAR_MIN_WIDTH,
  RIGHT_DOCK_MIN_RENDER_WIDTH,
  RIGHT_DOCK_TREE_MIN_WIDTH,
  saveRightDockTreeWidth,
  saveSidebarCollapsed,
  saveSidebarWidth,
  saveTerminalHeight,
  SIDEBAR_MAX_WIDTH,
  SIDEBAR_MIN_WIDTH,
  terminalMaxHeight,
  TERMINAL_MIN_HEIGHT,
  useLayoutStore,
} from "../store/layout";

const CHAT_MIN_WIDTH = 400;
const WORKSPACE_RESIZER_WIDTH = 8;

/**
 * Owns the shell geometry commands and their read projections: sidebar and
 * right-dock/terminal pointer and keyboard resizing, the sidebar toggle, and
 * the derived widths consumed by App JSX and the region prop builders. All
 * transient geometry lives on the layout store; the refs are injected because
 * the resizers drive CSS variables on the root layout element.
 */
export function useShellGeometry(input: { appRef: RefObject<HTMLDivElement | null>; layoutRef: RefObject<HTMLDivElement | null> }) {
  const { appRef, layoutRef } = input;
  const { desktopLayoutStyle } = useDesktopPreferences();
  const viewportWidth = useWindowChromeStore((state) => state.viewportWidth);
  const viewportHeight = useWindowChromeStore((state) => state.viewportHeight);
  const sidebarCollapsed = useLayoutStore((state) => state.sidebarCollapsed);
  const sidebarWidth = useLayoutStore((state) => state.sidebarWidth);
  const liveSidebarWidth = useLayoutStore((state) => state.liveSidebarWidth);
  const rightDockTreeWidth = useLayoutStore((state) => state.rightDockTreeWidth);
  const liveWorkspacePanelRenderWidth = useLayoutStore((state) => state.liveWorkspacePanelRenderWidth);
  const workspacePanelOpen = useLayoutStore((state) => state.workspacePanelOpen);
  const workspacePanelMaximized = useLayoutStore((state) => state.workspacePanelMaximized);
  const workspacePreviewActive = useLayoutStore((state) => state.workspacePreviewActive);
  const rightDockMode = useLayoutStore((state) => state.rightDockMode);
  const terminalPanelOpen = useLayoutStore((state) => state.terminalPanelOpen);
  const terminalHeight = useLayoutStore((state) => state.terminalHeight);
  const setSidebarCollapsed = useLayoutStore((state) => state.setSidebarCollapsed);
  const setSidebarWidth = useLayoutStore((state) => state.setSidebarWidth);
  const setRightDockTreeWidth = useLayoutStore((state) => state.setRightDockTreeWidth);
  const setTerminalHeight = useLayoutStore((state) => state.setTerminalHeight);
  const setSidebarTogglePressed = useLayoutStore((state) => state.setSidebarTogglePressed);
  const setSidebarResizing = useLayoutStore((state) => state.setSidebarResizing);
  const setLiveSidebarWidth = useLayoutStore((state) => state.setLiveSidebarWidth);
  const setWorkspacePanelResizing = useLayoutStore((state) => state.setWorkspacePanelResizing);
  const setLiveWorkspacePanelRenderWidth = useLayoutStore((state) => state.setLiveWorkspacePanelRenderWidth);
  const setLiveTerminalHeight = useLayoutStore((state) => state.setLiveTerminalHeight);
  const setSidebarSearchOpen = useOverlayStore((state) => state.setSidebarSearchOpen);
  const setTransientOverlayDismissSignal = useOverlayStore((state) => state.setTransientOverlayDismissSignal);

  const closeTransientOverlays = useCommittedCommand(() => {
    setTransientOverlayDismissSignal((signal) => signal + 1);
  });

  const rightDockDetailActive = rightDockMode !== "context" && workspacePreviewActive;
  // The dock keeps one width across tab switches (context/files/changed):
  // the tree width is the single source so toggling tabs never resizes the
  // sidebar. Preview detail stays inside the dock without widening it.
  const preferredWorkspacePanelWidth = rightDockTreeWidth;
  const rightDockTreeMinWidth = desktopLayoutStyle === "creation" ? CREATION_RIGHT_DOCK_TREE_MIN_WIDTH : RIGHT_DOCK_TREE_MIN_WIDTH;
  const rightDockTreeWidthClamp = desktopLayoutStyle === "creation" ? clampCreationRightDockTreeWidth : clampRightDockTreeWidth;
  const rightDockMinRenderWidth = desktopLayoutStyle === "creation" && !rightDockDetailActive
    ? CREATION_RIGHT_DOCK_MIN_RENDER_WIDTH
    : RIGHT_DOCK_MIN_RENDER_WIDTH;
  const workspacePanelMinWidth = rightDockTreeMinWidth;
  const chatReservedWidth = CHAT_MIN_WIDTH;
  const workspacePanelAvailableWidth = availableWorkspacePanelWidth({
    viewportWidth,
    sidebarCollapsed,
    sidebarWidth,
    chatMinWidth: chatReservedWidth,
    resizerWidth: WORKSPACE_RESIZER_WIDTH,
  });
  const {
    renderWidth: workspacePanelRenderWidth,
    overlay: workspacePanelOverlay,
    renderable: workspacePanelRenderable,
    gridOpen: workspacePanelGridOpen,
  } = resolveWorkspacePanelPlacement({
    viewportWidth, sidebarCollapsed, sidebarWidth, chatMinWidth: chatReservedWidth,
    resizerWidth: WORKSPACE_RESIZER_WIDTH, open: workspacePanelOpen,
    maximized: workspacePanelMaximized, preferredWidth: preferredWorkspacePanelWidth,
    minWidth: workspacePanelMinWidth, minRenderWidth: rightDockMinRenderWidth,
    liveWidth: liveWorkspacePanelRenderWidth,
  });
  const resolveLiveWorkspacePanelRenderWidth = useCommittedCommand((preferredWidth: number, nextSidebarWidth = sidebarWidth) =>
    resolveLiveWorkspacePanelWidth({
      viewportWidth,
      sidebarCollapsed,
      sidebarWidth: nextSidebarWidth,
      chatMinWidth: chatReservedWidth,
      resizerWidth: WORKSPACE_RESIZER_WIDTH,
      open: workspacePanelOpen,
      maximized: workspacePanelMaximized,
      preferredWidth,
      minWidth: workspacePanelMinWidth,
    }));

  const sidebarWidthClamp = desktopLayoutStyle === "creation" ? clampCreationSidebarWidth : clampSidebarWidth;
  const sidebarRenderWidth = liveSidebarWidth ?? sidebarWidth;
  const sidebarResizeMinWidth = desktopLayoutStyle === "creation" ? CREATION_SIDEBAR_MIN_WIDTH : SIDEBAR_MIN_WIDTH;
  const terminalRenderHeight = clampTerminalHeight(terminalHeight, viewportHeight);
  const terminalResizeMaxHeight = terminalMaxHeight(viewportHeight);

  const sidebarTogglePressTimerRef = useRef<number | null>(null);
  const workspacePanelResizeFinishRef = useRef<(() => void) | null>(null);
  const anchorPinTimerRef = useRef<number | null>(null);
  const anchorPinFrameRef = useRef<number | null>(null);
  useEffect(() => () => {
    if (sidebarTogglePressTimerRef.current !== null) window.clearTimeout(sidebarTogglePressTimerRef.current);
    if (anchorPinTimerRef.current !== null) window.clearTimeout(anchorPinTimerRef.current);
    if (anchorPinFrameRef.current !== null) window.cancelAnimationFrame(anchorPinFrameRef.current);
    workspacePanelResizeFinishRef.current?.();
  }, []);

  const pulseSidebarToggle = useCommittedCommand(() => {
    if (typeof window === "undefined") return;
    if (sidebarTogglePressTimerRef.current !== null) {
      window.clearTimeout(sidebarTogglePressTimerRef.current);
    }
    setSidebarTogglePressed(true);
    sidebarTogglePressTimerRef.current = window.setTimeout(() => {
      sidebarTogglePressTimerRef.current = null;
      setSidebarTogglePressed(false);
    }, 260);
  });

  const anchorAppScrollToChat = useCommittedCommand(() => {
    if (typeof window === "undefined") return;
    const el = appRef.current;
    if (!el) return;
    const pin = () => {
      el.scrollLeft = 0;
    };
    pin();
    anchorPinFrameRef.current = window.requestAnimationFrame(pin);
    anchorPinTimerRef.current = window.setTimeout(pin, 300);
  });

  const toggleSidebar = useCommittedCommand(() => {
    closeTransientOverlays();
    pulseSidebarToggle();
    anchorAppScrollToChat();
    const nextCollapsed = !sidebarCollapsed;
    if (nextCollapsed) setSidebarSearchOpen(false);
    setSidebarCollapsed(nextCollapsed);
    saveSidebarCollapsed(nextCollapsed);
  });

  const setExpandedSidebarWidth = useCommittedCommand((width: number) => {
    closeTransientOverlays();
    const next = sidebarWidthClamp(width);
    setSidebarWidth(next);
    saveSidebarWidth(next);
  });

  const startSidebarResize = useCommittedCommand((event: ReactPointerEvent<HTMLButtonElement>) => {
    if (sidebarCollapsed) return;
    const layout = layoutRef.current;
    if (!layout) return;
    event.preventDefault();
    closeTransientOverlays();
    setSidebarResizing(true);
    let nextWidth = sidebarWidth;
    const liveResize = createRafResizeUpdater({
      target: layout,
      separator: event.currentTarget,
      cssVar: "--sidebar-expanded-width",
      onApply: setLiveSidebarWidth,
    });
    const dockLiveResize = createRafResizeUpdater({
      target: layout,
      cssVar: "--workspace-width",
      onApply: setLiveWorkspacePanelRenderWidth,
    });
    const onMove = (moveEvent: PointerEvent) => {
      nextWidth = sidebarWidthClamp(moveEvent.clientX);
      liveResize.schedule(nextWidth);
      dockLiveResize.schedule(resolveLiveWorkspacePanelRenderWidth(preferredWorkspacePanelWidth, nextWidth));
    };
    const onDone = () => {
      liveResize.flush();
      dockLiveResize.flush();
      setSidebarWidth(nextWidth);
      saveSidebarWidth(nextWidth);
      setLiveSidebarWidth(null);
      setLiveWorkspacePanelRenderWidth(null);
      setSidebarResizing(false);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onDone);
      window.removeEventListener("pointercancel", onDone);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onDone);
    window.addEventListener("pointercancel", onDone);
  });

  const resizeSidebarWithKeyboard = useCommittedCommand((event: KeyboardEvent<HTMLButtonElement>) => {
    if (sidebarCollapsed) return;
    if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
      event.preventDefault();
      setExpandedSidebarWidth(sidebarWidth + (event.key === "ArrowRight" ? 16 : -16));
    } else if (event.key === "Home") {
      event.preventDefault();
      setExpandedSidebarWidth(sidebarResizeMinWidth);
    } else if (event.key === "End") {
      event.preventDefault();
      setExpandedSidebarWidth(SIDEBAR_MAX_WIDTH);
    }
  });

  const setSavedWorkspacePanelWidth = useCommittedCommand((width: number) => {
    closeTransientOverlays();
    const next = rightDockTreeWidthClamp(width, workspacePanelAvailableWidth);
    setRightDockTreeWidth(next);
    saveRightDockTreeWidth(next);
  });

  const ensureWorkspacePanelWidth = useCommittedCommand((width: number) => {
    closeTransientOverlays();
    if (rightDockMode === "context") return;
    const next = rightDockTreeWidthClamp(width, workspacePanelAvailableWidth);
    setRightDockTreeWidth(next);
    saveRightDockTreeWidth(next);
  });

  const startWorkspacePanelResize = useCommittedCommand((event: ReactPointerEvent<HTMLButtonElement>) => {
    if (event.button !== 0 || !workspacePanelOpen) return;
    const layout = layoutRef.current;
    if (!layout) return;
    event.preventDefault();
    workspacePanelResizeFinishRef.current?.();
    closeTransientOverlays();
    setWorkspacePanelResizing(true);
    const separator = event.currentTarget;
    const pointerId = event.pointerId;
    const startX = event.clientX;
    const startDockWidth = workspacePanelRenderWidth;
    let nextDockWidth = startDockWidth;
    const liveResize = createRafResizeUpdater({
      target: layout,
      separator,
      cssVar: "--workspace-width",
      onApply: setLiveWorkspacePanelRenderWidth,
    });
    const onMove = (moveEvent: PointerEvent) => {
      const delta = moveEvent.clientX - startX;
      nextDockWidth = startDockWidth - delta;
      nextDockWidth = rightDockTreeWidthClamp(nextDockWidth, workspacePanelAvailableWidth);
      liveResize.schedule(resolveLiveWorkspacePanelRenderWidth(nextDockWidth));
    };
    const lifecycle = createPointerResizeLifecycle({
      separator,
      pointerId,
      onMove,
      onFinish: () => {
        liveResize.flush();
        setSavedWorkspacePanelWidth(nextDockWidth);
        setLiveWorkspacePanelRenderWidth(null);
        setWorkspacePanelResizing(false);
        workspacePanelResizeFinishRef.current = null;
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      },
    });
    workspacePanelResizeFinishRef.current = lifecycle.finish;
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
  });

  const resizeWorkspacePanelWithKeyboard = useCommittedCommand((event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
      event.preventDefault();
      setSavedWorkspacePanelWidth(workspacePanelRenderWidth + (event.key === "ArrowLeft" ? 16 : -16));
    } else if (event.key === "Home") {
      event.preventDefault();
      setSavedWorkspacePanelWidth(rightDockTreeMinWidth);
    } else if (event.key === "End") {
      event.preventDefault();
      setSavedWorkspacePanelWidth(workspacePanelAvailableWidth);
    }
  });

  const setSavedTerminalHeight = useCommittedCommand((height: number) => {
    const next = clampTerminalHeight(height, viewportHeight);
    setTerminalHeight(next);
    saveTerminalHeight(next);
  });

  const startTerminalResize = useCommittedCommand((event: ReactPointerEvent<HTMLButtonElement>) => {
    if (!terminalPanelOpen) return;
    const layout = layoutRef.current;
    if (!layout) return;
    event.preventDefault();
    closeTransientOverlays();
    const startY = event.clientY;
    const startHeight = terminalRenderHeight;
    let nextHeight = startHeight;
    const liveResize = createRafResizeUpdater({
      target: layout,
      separator: event.currentTarget,
      cssVar: "--terminal-height",
      onApply: setLiveTerminalHeight,
    });
    const onMove = (moveEvent: PointerEvent) => {
      const delta = startY - moveEvent.clientY;
      nextHeight = clampTerminalHeight(startHeight + delta, viewportHeight);
      liveResize.schedule(nextHeight);
    };
    const onDone = () => {
      liveResize.flush();
      setLiveTerminalHeight(null);
      setSavedTerminalHeight(nextHeight);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onDone);
      window.removeEventListener("pointercancel", onDone);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    document.body.style.cursor = "row-resize";
    document.body.style.userSelect = "none";
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onDone);
    window.addEventListener("pointercancel", onDone);
  });

  const resizeTerminalWithKeyboard = useCommittedCommand((event: KeyboardEvent<HTMLButtonElement>) => {
    if (!terminalPanelOpen) return;
    if (event.key === "ArrowUp" || event.key === "ArrowDown") {
      event.preventDefault();
      setSavedTerminalHeight(terminalRenderHeight + (event.key === "ArrowUp" ? 16 : -16));
    } else if (event.key === "Home") {
      event.preventDefault();
      setSavedTerminalHeight(TERMINAL_MIN_HEIGHT);
    } else if (event.key === "End") {
      event.preventDefault();
      setSavedTerminalHeight(terminalResizeMaxHeight);
    }
  });

  return {
    toggleSidebar,
    setExpandedSidebarWidth,
    startSidebarResize,
    resizeSidebarWithKeyboard,
    setSavedWorkspacePanelWidth,
    ensureWorkspacePanelWidth,
    startWorkspacePanelResize,
    resizeWorkspacePanelWithKeyboard,
    setSavedTerminalHeight,
    startTerminalResize,
    resizeTerminalWithKeyboard,
    rightDockTreeMinWidth,
    rightDockTreeWidthClamp,
    workspacePanelMinWidth,
    chatReservedWidth,
    workspacePanelAvailableWidth,
    workspacePanelRenderWidth,
    workspacePanelOverlay,
    workspacePanelRenderable,
    workspacePanelGridOpen,
    sidebarRenderWidth,
    sidebarResizeMinWidth,
    sidebarWidthClamp,
    terminalRenderHeight,
    terminalResizeMaxHeight,
  };
}
