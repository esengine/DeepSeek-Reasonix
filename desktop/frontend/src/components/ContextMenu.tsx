import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { KeyboardEvent as ReactKeyboardEvent, MouseEvent as ReactMouseEvent, ReactNode } from "react";
import { createPortal } from "react-dom";

export type ContextMenuPoint = { left: number; top: number };

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
      /** Nested submenu items, shown on hover beside the parent item. */
      children?: ContextMenuItem[];
      onSelect?: () => void;
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
  return { left: rect.left + 12, top: rect.bottom + 6 };
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
  const wrapperRefs = useRef(new Map<string, HTMLDivElement>());
  const subMenuRefs = useRef(new Set<HTMLElement>());
  const subMenuTimerRef = useRef(0);
  const [position, setPosition] = useState<ContextMenuPoint | null>(point);
  const [openSubKey, setOpenSubKey] = useState<string | null>(null);
  const [subMenuPosition, setSubMenuPosition] = useState<ContextMenuPoint | null>(null);

  useLayoutEffect(() => {
    if (!open || !point) return;
    const rect = menuRef.current?.getBoundingClientRect();
    if (!rect) {
      setPosition(point);
      return;
    }
    setPosition(clampMenuPoint(point.left, point.top, rect.width, rect.height));
  }, [open, point, items]);

  // Position the open submenu beside its parent item (fixed, so the parent
  // menu's overflow does not clip it).
  useLayoutEffect(() => {
    if (!openSubKey) {
      setSubMenuPosition(null);
      return;
    }
    const wrapper = wrapperRefs.current.get(openSubKey);
    if (!wrapper) return;
    const rect = wrapper.getBoundingClientRect();
    setSubMenuPosition({ left: rect.right + 2, top: rect.top - 5 });
  }, [openSubKey, items]);

  useEffect(() => {
    return () => window.clearTimeout(subMenuTimerRef.current);
  }, []);

  const openSubmenu = (key: string) => {
    window.clearTimeout(subMenuTimerRef.current);
    setOpenSubKey(key);
  };
  const closeSubmenuDelayed = () => {
    window.clearTimeout(subMenuTimerRef.current);
    subMenuTimerRef.current = window.setTimeout(() => setOpenSubKey(null), 150);
  };

  useEffect(() => {
    if (!open) return;
    const closeOnOutsidePointerDown = (event: PointerEvent) => {
      const target = event.target;
      if (target instanceof Node) {
        if (menuRef.current?.contains(target)) return;
        for (const sub of subMenuRefs.current) {
          if (sub.contains(target)) return;
        }
      }
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

  if (!open || !point || !position) return null;

  const renderItem = (item: ContextMenuItem): ReactNode => {
    if (item.type === "separator") {
      return <div key={item.key} className="context-menu__separator" role="separator" />;
    }
    if (item.children && item.children.length > 0) {
      // Submenu parent: hovering reveals the nested menu beside this item
      // (fixed-positioned via portal so the parent's overflow never clips it).
      // The parent is a plain button — its children carry the actions.
      const submenuOpen = openSubKey === item.key;
      return (
        <div
          key={item.key}
          ref={(node) => {
            if (node) wrapperRefs.current.set(item.key, node);
            else wrapperRefs.current.delete(item.key);
          }}
          className="context-menu__item-wrapper"
          onMouseEnter={() => openSubmenu(item.key)}
          onMouseLeave={closeSubmenuDelayed}
        >
          <button
            type="button"
            role="menuitem"
            disabled={item.disabled}
            className={`context-menu__item${item.variant ? ` context-menu__item--${item.variant}` : ""}`}
          >
            {item.icon}
            <span>{item.label}</span>
            <span className="context-menu__chevron" aria-hidden="true">›</span>
          </button>
          {submenuOpen && subMenuPosition && createPortal(
            <div
              ref={(node) => {
                if (node) subMenuRefs.current.add(node);
              }}
              className="context-menu context-menu--sub"
              role="menu"
              style={{ left: subMenuPosition.left, top: subMenuPosition.top, minWidth: 180 }}
              onMouseEnter={() => openSubmenu(item.key)}
              onMouseLeave={closeSubmenuDelayed}
            >
              {item.children.map(renderItem)}
            </div>,
            document.body,
          )}
        </div>
      );
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
          if (!item.disabled && item.onSelect) item.onSelect();
        }}
      >
        {item.icon}
        <span>{item.label}</span>
        {item.shortcut && <span className="context-menu__shortcut">{item.shortcut}</span>}
      </button>
    );
  };

  return createPortal(
    <div
      ref={menuRef}
      className={`context-menu${className ? ` ${className}` : ""}`}
      role="menu"
      aria-label={ariaLabel}
      style={{ left: position.left, top: position.top, minWidth }}
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
      {items.map(renderItem)}
    </div>,
    document.body,
  );
}
