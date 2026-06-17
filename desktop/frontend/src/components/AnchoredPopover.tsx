import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { CSSProperties, ReactNode, RefObject } from "react";
import { createPortal } from "react-dom";

type PopoverPosition = {
  left: number;
  top: number;
};
type PopoverPhase = "closed" | "open" | "closing";

const EDGE_GAP = 8;
const DEFAULT_OFFSET = 8;
export const ANCHORED_POPOVER_CLOSE_MS = 140;

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function samePosition(a: PopoverPosition | null, b: PopoverPosition): boolean {
  return !!a && Math.abs(a.left - b.left) < 0.5 && Math.abs(a.top - b.top) < 0.5;
}

function calculatePosition(
  anchor: DOMRect,
  menu: DOMRect,
  align: "start" | "end",
  offset: number,
  placement: "auto" | "bottom",
): PopoverPosition {
  const viewportWidth = window.innerWidth;
  const viewportHeight = window.innerHeight;
  const preferredTop = anchor.top - menu.height - offset;
  const fallbackTop = anchor.bottom + offset;
  const top = placement === "bottom"
    ? Math.min(fallbackTop, Math.max(EDGE_GAP, viewportHeight - menu.height - EDGE_GAP))
    : preferredTop >= EDGE_GAP
    ? preferredTop
    : Math.min(fallbackTop, Math.max(EDGE_GAP, viewportHeight - menu.height - EDGE_GAP));
  const rawLeft = align === "end" ? anchor.right - menu.width : anchor.left;
  const left = clamp(rawLeft, EDGE_GAP, Math.max(EDGE_GAP, viewportWidth - menu.width - EDGE_GAP));
  return { left, top: clamp(top, EDGE_GAP, Math.max(EDGE_GAP, viewportHeight - menu.height - EDGE_GAP)) };
}

export function AnchoredPopover({
  open,
  anchorRef,
  onClose,
  className,
  children,
  align = "start",
  offset = DEFAULT_OFFSET,
  placement = "auto",
  style,
  closing = false,
}: {
  open: boolean;
  anchorRef: RefObject<HTMLElement | null>;
  onClose: () => void;
  className: string;
  children: ReactNode;
  align?: "start" | "end";
  offset?: number;
  placement?: "auto" | "bottom";
  style?: CSSProperties;
  closing?: boolean;
}) {
  const [phase, setPhase] = useState<PopoverPhase>(open ? "open" : "closed");
  const [position, setPosition] = useState<PopoverPosition | null>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  const phaseRef = useRef<PopoverPhase>(phase);

  useLayoutEffect(() => {
    let id: number | undefined;
    if (open) {
      phaseRef.current = "open";
      setPhase("open");
      return undefined;
    }
    if (phaseRef.current === "closed") return undefined;
    phaseRef.current = "closing";
    setPhase("closing");
    const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    id = window.setTimeout(() => {
      phaseRef.current = "closed";
      setPhase("closed");
      setPosition(null);
    }, reduceMotion ? 0 : ANCHORED_POPOVER_CLOSE_MS);
    return () => {
      if (id !== undefined) window.clearTimeout(id);
    };
  }, [open]);

  const rendered = closing || phase !== "closed";

  useLayoutEffect(() => {
    if (!rendered) {
      setPosition(null);
      return;
    }
    let frame: number | null = null;
    const updatePosition = () => {
      frame = null;
      const anchor = anchorRef.current?.getBoundingClientRect();
      const menu = popoverRef.current?.getBoundingClientRect();
      if (!anchor || !menu) return;
      const next = calculatePosition(anchor, menu, align, offset, placement);
      setPosition((current) => (samePosition(current, next) ? current : next));
    };
    const scheduleUpdate = () => {
      if (frame !== null) return;
      frame = window.requestAnimationFrame(updatePosition);
    };
    updatePosition();
    scheduleUpdate();

    const anchor = anchorRef.current;
    const menu = popoverRef.current;
    let observer: ResizeObserver | null = null;
    if (typeof ResizeObserver !== "undefined") {
      observer = new ResizeObserver(scheduleUpdate);
      if (anchor) observer.observe(anchor);
      if (menu) observer.observe(menu);
    }
    // Portaled popovers use viewport coordinates; scrollable ancestors move the anchor.
    window.addEventListener("scroll", scheduleUpdate, true);
    window.visualViewport?.addEventListener("scroll", scheduleUpdate);
    window.visualViewport?.addEventListener("resize", scheduleUpdate);

    return () => {
      if (frame !== null) window.cancelAnimationFrame(frame);
      observer?.disconnect();
      window.removeEventListener("scroll", scheduleUpdate, true);
      window.visualViewport?.removeEventListener("scroll", scheduleUpdate);
      window.visualViewport?.removeEventListener("resize", scheduleUpdate);
    };
  }, [rendered, anchorRef, align, offset, placement]);

  useEffect(() => {
    if (!open || closing) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    const closeOnOutsideClick = (event: MouseEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      if (popoverRef.current?.contains(target) || anchorRef.current?.contains(target)) return;
      onClose();
    };
    const closeOnViewportChange = () => onClose();
    window.addEventListener("keydown", closeOnEscape);
    document.addEventListener("click", closeOnOutsideClick);
    window.addEventListener("resize", closeOnViewportChange);
    return () => {
      window.removeEventListener("keydown", closeOnEscape);
      document.removeEventListener("click", closeOnOutsideClick);
      window.removeEventListener("resize", closeOnViewportChange);
    };
  }, [anchorRef, onClose, open]);

  if (!rendered) return null;

  return createPortal(
    <div
      ref={popoverRef}
      data-anchored-popover="active"
      data-ready={position ? "true" : "false"}
      data-state={closing || phase === "closing" ? "closing" : "open"}
      aria-hidden={closing || phase === "closing" ? true : undefined}
      className={`anchored-popover ${className}`}
      style={{
        ...style,
        left: position?.left ?? -9999,
        top: position?.top ?? -9999,
        visibility: position ? "visible" : "hidden",
      }}
      onMouseDown={(event) => {
        event.stopPropagation();
      }}
      onClick={(event) => {
        event.stopPropagation();
      }}
    >
      {children}
    </div>,
    document.body,
  );
}
