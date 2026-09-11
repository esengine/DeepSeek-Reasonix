// Pointer-driven tab reordering for the dock tab strip, plus the strip's
// overflow detection. Lifted out of TabBar so the interaction state machine
// (press → threshold → live reorder → FLIP slide → teardown) is one unit and
// the component stays a view.
//
// The window listeners installed at drag start must read the *latest* handlers,
// not the render in which startTabDrag ran, so a stable pair of window
// callbacks forwards through refs.
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { PointerEvent as ReactPointerEvent, RefObject } from "react";
import { useActivityBarStore, type TabItem } from "../store/activityBar";

// Matches .workbench-dock__tabs' theme CSS gap; the drag layout math inserts
// the slot between tabs with the same spacing so it never overlaps a neighbor.
const DOCK_TAB_GAP = 8;

interface DockTabDragInput {
  tabs: TabItem[];
  onActivate: (tabId: string) => void;
  onMoveTab: (fromId: string, toId: string, side: "left" | "right") => void;
}

export function useDockTabDrag({ tabs, onActivate, onMoveTab }: DockTabDragInput) {
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

  // React StrictMode replays mount effects in development; reset the guard so
  // the replayed mount can still accept opener discoveries.
  useEffect(() => {
    return () => {
      if (dragRafRef.current) {
        cancelAnimationFrame(dragRafRef.current);
        dragRafRef.current = 0;
      }
    };
  }, []);

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
  }, [maybeReorder, onActivate]);

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
  }, [clearDragState, onWindowPointerMove, onWindowPointerUp]);

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
    const flipRafs = flipRafIdsRef.current;
    return () => {
      for (const rafId of flipRafs.values()) cancelAnimationFrame(rafId);
      for (const el of flipRafs.keys()) {
        el.style.transition = "";
        el.style.transform = "";
      }
      flipRafs.clear();
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

  return {
    draggingTabId,
    dragElRefs,
    floatingRef: floatingRef as RefObject<HTMLDivElement | null>,
    tabsRef: tabsRef as RefObject<HTMLDivElement | null>,
    tabsOverflow,
    suppressClickRef,
    startTabDrag,
    clearDragState,
    // The floating ghost anchors at the dragged tab's base rect; both parts
    // are stable for the duration of the gesture.
    ghost: {
      left: dragContainerLeftRef.current + dragStartLeftRef.current,
      top: dragContainerTopRef.current + dragStartTopRef.current,
      width: dragBaseWidthRef.current.get(draggingTabId ?? "") ?? 0,
    },
    /** Width the dragged tab held before the drag, for its placeholder slot. */
    slotWidthFor: (tabId: string) => dragBaseWidthRef.current.get(tabId) ?? 0,
  };
}
