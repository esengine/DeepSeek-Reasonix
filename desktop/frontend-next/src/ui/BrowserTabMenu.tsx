import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { keyOf, type Surface } from "./workbench_tabs";
import { listenAction } from "./listen";
import { pinToViewport } from "./place";

interface Props {
  menu: { surface: Surface; x: number; y: number; anchor: HTMLElement };
  others: boolean; onDismiss: () => void; onClose: (mode: "one" | "others" | "all") => void;
}

export function BrowserTabMenu({ menu, others, onDismiss, onClose }: Props) {
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const dismiss = (event: Event) => { if (!box.current?.contains(event.target as Node)) onDismiss(); };
    const blur = () => onDismiss();
    const off = listenAction(document, "pointerdown", { action: "layer.dismiss", listener: dismiss });
    window.addEventListener("blur", blur);
    box.current?.querySelector<HTMLButtonElement>("button")?.focus();
    return () => {
      off();
      window.removeEventListener("blur", blur);
      if (menu.anchor.isConnected) menu.anchor.focus();
    };
  }, [menu, onDismiss]);
  return createPortal(
    <div className="tabmenu" data-action-keydown="layer.dismiss" role="menu" aria-label={t("浏览器标签")} ref={(el) => { box.current = el; if (el) pinToViewport(el, menu.x, menu.y); }} onKeyDown={(e) => {
      if (e.key === "Escape" || e.key === "Tab") { e.preventDefault(); e.stopPropagation(); onDismiss(); }
      if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(e.key)) return;
      e.preventDefault();
      const items = [...e.currentTarget.querySelectorAll<HTMLButtonElement>("button:not(:disabled)")];
      const at = items.indexOf(document.activeElement as HTMLButtonElement);
      const next = e.key === "Home" ? 0 : e.key === "End" ? items.length - 1 : (at + (e.key === "ArrowUp" ? -1 : 1) + items.length) % items.length;
      items[next]?.focus();
    }}>
      <button role="menuitem" data-action="workbench.close" data-target={keyOf(menu.surface)} onClick={() => onClose("one")}>{t("关闭")}</button>
      <button role="menuitem" data-action="workbench.browser-close" data-target={keyOf(menu.surface)} data-value="others" disabled={!others} onClick={() => onClose("others")}>{t("关闭其他浏览器标签")}</button>
      <button role="menuitem" data-action="workbench.browser-close" data-target={keyOf(menu.surface)} data-value="all" onClick={() => onClose("all")}>{t("关闭全部浏览器标签")}</button>
    </div>, document.body,
  );
}
