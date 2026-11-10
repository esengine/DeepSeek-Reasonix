import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { keyOf, type Surface } from "./workbench_tabs";
import { listenAction } from "./listen";
import { pinToViewport } from "./place";

interface Props {
  menu: { surface: Surface; x: number; y: number; anchor: HTMLElement };
  // How many tabs each choice closes, said beside it; label names the tab the menu is for.
  others: number; right: number; all: number; label: string; onDismiss: () => void; onClose: (mode: "one" | "others" | "right" | "all") => void;
}

export function BrowserTabMenu({ menu, others, right, all, label, onDismiss, onClose }: Props) {
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
    <div className="tabmenu" data-action-keydown="layer.dismiss" role="menu" aria-label={t("workbench.tabs")} ref={(el) => { box.current = el; if (el) pinToViewport(el, menu.x, menu.y); }} onKeyDown={(e) => {
      if (e.key === "Escape" || e.key === "Tab") { e.preventDefault(); e.stopPropagation(); onDismiss(); }
      if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(e.key)) return;
      e.preventDefault();
      const items = [...e.currentTarget.querySelectorAll<HTMLButtonElement>("button:not(:disabled)")];
      const at = items.indexOf(document.activeElement as HTMLButtonElement);
      const next = e.key === "Home" ? 0 : e.key === "End" ? items.length - 1 : (at + (e.key === "ArrowUp" ? -1 : 1) + items.length) % items.length;
      items[next]?.focus();
    }}>
      <div className="tabmenu-head"><span>{t("workbench.tab_named", { name: label })}</span></div>
      <button role="menuitem" data-action="workbench.close" data-target={keyOf(menu.surface)} onClick={() => onClose("one")}><span>{t("common.close")}</span></button>
      <button role="menuitem" data-action="workbench.browser-close" data-target={keyOf(menu.surface)} data-value="others" aria-label={t("workbench.close_other_tabs")} disabled={!others} onClick={() => onClose("others")}>
        <span>{t("workbench.close_other_tabs")}</span>{others > 0 && <kbd>{others}</kbd>}
      </button>
      <button role="menuitem" data-action="workbench.browser-close" data-target={keyOf(menu.surface)} data-value="right" aria-label={t("workbench.close_tabs_to_the_right")} disabled={!right} onClick={() => onClose("right")}>
        <span>{t("workbench.close_tabs_to_the_right")}</span>{right > 0 && <kbd>{right}</kbd>}
      </button>
      <button role="menuitem" data-action="workbench.browser-close" data-target={keyOf(menu.surface)} data-value="all" aria-label={t("workbench.close_all_tabs")} onClick={() => onClose("all")}>
        <span>{t("workbench.close_all_tabs")}</span>{all > 0 && <kbd>{all}</kbd>}
      </button>
    </div>, document.body,
  );
}
