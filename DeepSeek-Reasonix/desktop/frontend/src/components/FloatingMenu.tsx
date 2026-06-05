import type { MouseEvent as ReactMouseEvent, ReactNode } from "react";
import { useMemo } from "react";

const FLOATING_MENU_MARGIN = 8;

export interface FloatingMenuItem {
  icon?: ReactNode;
  label: ReactNode;
  onSelect: () => void;
  disabled?: boolean;
}

function clampFloatingMenuPosition(x: number, y: number, width: number, height: number): { left: number; top: number } {
  if (typeof window === "undefined") return { left: x, top: y };
  const maxLeft = Math.max(FLOATING_MENU_MARGIN, window.innerWidth - width - FLOATING_MENU_MARGIN);
  const maxTop = Math.max(FLOATING_MENU_MARGIN, window.innerHeight - height - FLOATING_MENU_MARGIN);
  return {
    left: Math.min(maxLeft, Math.max(FLOATING_MENU_MARGIN, x)),
    top: Math.min(maxTop, Math.max(FLOATING_MENU_MARGIN, y)),
  };
}

export function FloatingMenu({
  x,
  y,
  width = 240,
  estimatedHeight,
  className = "",
  children,
}: {
  x: number;
  y: number;
  width?: number;
  estimatedHeight: number;
  className?: string;
  children: ReactNode;
}) {
  const pos = useMemo(() => clampFloatingMenuPosition(x, y, width, estimatedHeight), [estimatedHeight, width, x, y]);
  return (
    <div
      className={`floating-menu${className ? ` ${className}` : ""}`}
      style={{ left: pos.left, top: pos.top }}
      onMouseDown={(e) => {
        e.preventDefault();
        e.stopPropagation();
      }}
      onClick={(e) => e.stopPropagation()}
    >
      {children}
    </div>
  );
}

export function FloatingMenuItems({ items }: { items: FloatingMenuItem[] }) {
  return (
    <>
      {items.map((item, index) => (
        <button
          key={index}
          type="button"
          disabled={item.disabled}
          onClick={(event: ReactMouseEvent<HTMLButtonElement>) => {
            event.stopPropagation();
            if (!item.disabled) item.onSelect();
          }}
        >
          {item.icon}
          <span>{item.label}</span>
        </button>
      ))}
    </>
  );
}
