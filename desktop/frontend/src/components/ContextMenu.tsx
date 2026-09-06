import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { KeyboardEvent as ReactKeyboardEvent, MouseEvent as ReactMouseEvent, ReactNode } from "react";
import { createPortal } from "react-dom";

export type ContextMenuPoint = {
  left: number;
  top: number;
  keyboardTarget?: HTMLElement;
};

export type ContextMenuItem =
  | {
      type?: "item";
      key: string;
      icon?: ReactNode;
      label: ReactNode;
      shortcut?: string;
      disabled?: boolean;
      danger?: boolean;
      variant?: "section";
      onSelect: () => void;
    }
  | {
      type: "separator";
      key: string;
    };

const EDGE_GAP = 8;

function clampMenuPoint(left: number, top: number, width: number, height: number): ContextMenuPoint {
  if (typeof window === "undefined") return { left, top };
  return {
    left: Math.min(Math.max(EDGE_GAP, left), Math.max(EDGE_GAP, window.innerWidth - width - EDGE_GAP)),
    top: Math.min(Math.max(EDGE_GAP, top), Math.max(EDGE_GAP, window.innerHeight - height - EDGE_GAP)),
  };
}

export function contextMenuPointFromEvent(
  event: ReactMouseEvent<HTMLElement> | ReactKeyboardEvent<HTMLElement>,
): ContextMenuPoint {
  if ("clientX" in event && event.clientX > 0 && event.clientY > 0) {
    return { left: event.clientX, top: event.clientY };
  }
  const rect = event.currentTarget.getBoundingClientRect();
  return {
    left: rect.left + 12,
    top: rect.bottom + 6,
    ...("key" in event ? { keyboardTarget: event.currentTarget } : {}),
  };
}

export function ContextMenu({
  open,
  point,
  items,
  onClose,
  className,
  minWidth = 180,
  ariaLabel = "Context menu",
}: {
  open: boolean;
  point: ContextMenuPoint | null;
  items: ContextMenuItem[];
  onClose: () => void;
  className?: string;
  minWidth?: number;
  ariaLabel?: string;
}) {
  const menuRef = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState<ContextMenuPoint | null>(point);

  useLayoutEffect(() => {
    if (!open || !point) return;
    const rect = menuRef.current?.getBoundingClientRect();
    if (!rect) {
      setPosition(point);
      return;
    }
    setPosition(clampMenuPoint(point.left, point.top, rect.width, rect.height));
  }, [open, point, items]);

  useLayoutEffect(() => {
    const target = point?.keyboardTarget;
    const menu = menuRef.current;
    if (!open || !target || !menu) return;
    menu.querySelector<HTMLButtonElement>('button[role="menuitem"]:not(:disabled)')?.focus({ preventScroll: true });
    return () => {
      // Do not steal focus from an outside click or an action opening a dialog.
      if (target.isConnected && (menu.contains(document.activeElement) || document.activeElement === document.body)) {
        target.focus({ preventScroll: true });
      }
    };
  }, [open, point]);

  const onMenuKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('button[role="menuitem"]:not(:disabled)'));
    const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
    let next: number;
    switch (event.key) {
      case "ArrowDown": next = (index + 1) % buttons.length; break;
      case "ArrowUp": next = index < 0 ? buttons.length - 1 : (index - 1 + buttons.length) % buttons.length; break;
      case "Home": next = 0; break;
      case "End": next = buttons.length - 1; break;
      case "Tab":
        // Let the browser continue from the invoking control in tab order.
        point?.keyboardTarget?.focus({ preventScroll: true });
        onClose();
        event.stopPropagation();
        return;
      case "Escape":
        event.preventDefault();
        event.stopPropagation();
        onClose();
        return;
      default: return;
    }
    event.preventDefault();
    event.stopPropagation();
    buttons[next]?.focus({ preventScroll: true });
  };

  useEffect(() => {
    if (!open) return;
    const closeOnOutsidePointerDown = (event: PointerEvent) => {
      const target = event.target;
      if (target instanceof Node && menuRef.current?.contains(target)) return;
      onClose();
    };
    const close = () => onClose();
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("pointerdown", closeOnOutsidePointerDown, true);
    window.addEventListener("resize", close);
    window.addEventListener("keydown", closeOnEscape);
    return () => {
      window.removeEventListener("pointerdown", closeOnOutsidePointerDown, true);
      window.removeEventListener("resize", close);
      window.removeEventListener("keydown", closeOnEscape);
    };
  }, [open, onClose]);

  if (!open || !point) return null;

  return createPortal(
    <div
      ref={menuRef}
      className={`context-menu${className ? ` ${className}` : ""}`}
      role="menu"
      aria-label={ariaLabel}
      style={{ left: (position ?? point).left, top: (position ?? point).top, minWidth }}
      onKeyDown={onMenuKeyDown}
      onMouseDown={(event) => {
        event.preventDefault();
        event.stopPropagation();
      }}
      onClick={(event) => event.stopPropagation()}
      onContextMenu={(event) => {
        event.preventDefault();
        event.stopPropagation();
      }}
    >
      {items.map((item) => {
        if (item.type === "separator") {
          return <div key={item.key} className="context-menu__separator" role="separator" />;
        }
        return (
          <button
            key={item.key}
            type="button"
            role="menuitem"
            disabled={item.disabled}
            className={`context-menu__item${item.danger ? " context-menu__item--danger" : ""}${item.variant ? ` context-menu__item--${item.variant}` : ""}`}
            onClick={(event) => {
              event.stopPropagation();
              if (!item.disabled) item.onSelect();
            }}
          >
            {item.icon}
            <span>{item.label}</span>
            {item.shortcut && <span className="context-menu__shortcut">{item.shortcut}</span>}
          </button>
        );
      })}
    </div>,
    document.body,
  );
}
