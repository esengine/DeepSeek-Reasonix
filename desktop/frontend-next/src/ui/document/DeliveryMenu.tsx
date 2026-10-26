import { useCallback, useRef, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useDismiss } from "../dismiss";
import { beside, pinToViewport, useFollow } from "../place";
import { StudioIcon, type StudioIconName } from "../StudioIcon";

export interface DeliveryAction {
  kind: "add" | "open" | "reveal" | "locate" | "copy";
  label: string;
  icon: StudioIconName;
  disabled?: boolean;
  title?: string;
  run: () => void;
}

export function DeliveryMenu({ anchor, label, target, actions, onClose }: {
  anchor: RefObject<HTMLButtonElement | null>; label: string;
  actions: DeliveryAction[]; target: string; onClose: () => void;
}) {
  const menu = useRef<HTMLDivElement>(null);
  const close = useCallback(() => {
    if (menu.current?.contains(document.activeElement)) anchor.current?.focus({ preventScroll: true });
    onClose();
  }, [anchor, onClose]);
  useDismiss(true, anchor, close, menu);
  const place = useCallback(() => {
    if (!anchor.current || !menu.current) return;
    const rect = anchor.current.getBoundingClientRect(), box = menu.current.getBoundingClientRect();
    pinToViewport(menu.current, rect.right - box.width, beside(rect, box.height, innerHeight, "below", 6));
  }, [anchor]);
  useFollow(true, place);
  return createPortal(<div className="delivery-menu" role="menu" aria-label={label} ref={menu} data-action="delivery.menu-navigation"
    onKeyDown={(event) => {
      if (event.key === "Tab") { close(); return; }
      if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
      event.preventDefault();
      const items = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)')];
      const at = items.indexOf(document.activeElement as HTMLButtonElement);
      const next = event.key === "Home" ? 0 : event.key === "End" ? items.length - 1
        : (at + (event.key === "ArrowDown" ? 1 : -1) + items.length) % items.length;
      items[next]?.focus();
    }}>
    {actions.map((action, index) => {
      const add = action.kind === "add", open = action.kind === "open", reveal = action.kind === "reveal", locate = action.kind === "locate";
      return <button key={action.label} type="button" role="menuitem" data-target={target}
      data-action={add ? "delivery.add-to-chat" : open ? "workbench.open-file" : reveal ? "workbench.reveal" : locate ? "workbench.locate" : "workbench.copy-path"}
      disabled={action.disabled} title={action.title} autoFocus={index === actions.findIndex((item) => !item.disabled)}
      onClick={() => { close(); action.run(); }}>
      <StudioIcon name={action.icon} /><span>{action.label}</span>
    </button>; })}
  </div>, document.body);
}
