import { useEffect, useLayoutEffect, useState } from "react";
import type { CSSProperties, ReactNode, RefObject } from "react";
import { createPortal } from "react-dom";

type PopoverPosition = {
  left: number;
  top: number;
};

const EDGE_GAP = 8;
const DEFAULT_OFFSET = 8;

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
}: {
  open: boolean;
  anchorRef: RefObject<HTMLElement>;
  onClose: () => void;
  className: string;
  children: ReactNode;
  align?: "start" | "end";
  offset?: number;
  placement?: "auto" | "bottom";
  style?: CSSProperties;
}) {
  const [position, setPosition] = useState<PopoverPosition | null>(null);

  useLayoutEffect(() => {
    if (!open) {
      setPosition(null);
      return;
    }
    const anchor = anchorRef.current?.getBoundingClientRect();
    const menu = document.querySelector<HTMLElement>("[data-anchored-popover='active']")?.getBoundingClientRect();
    if (!anchor || !menu) return;
    const next = calculatePosition(anchor, menu, align, offset, placement);
    setPosition((current) => (samePosition(current, next) ? current : next));
  });

  useEffect(() => {
    if (!open) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    const closeOnViewportChange = () => onClose();
    window.addEventListener("keydown", closeOnEscape);
    window.addEventListener("resize", closeOnViewportChange);
    window.addEventListener("scroll", closeOnViewportChange, true);
    return () => {
      window.removeEventListener("keydown", closeOnEscape);
      window.removeEventListener("resize", closeOnViewportChange);
      window.removeEventListener("scroll", closeOnViewportChange, true);
    };
  }, [onClose, open]);

  if (!open) return null;

  return createPortal(
    <>
      <div className="anchored-popover__backdrop" onMouseDown={onClose} />
      <div
        data-anchored-popover="active"
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
      </div>
    </>,
    document.body,
  );
}
