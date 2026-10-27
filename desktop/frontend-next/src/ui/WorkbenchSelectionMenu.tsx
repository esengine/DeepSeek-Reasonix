import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { pinToViewport } from "./place";
import type { TextContextMenuRequest } from "../port/host";
import type { TextMenuAction } from "./workbench_text_menu";

export function WorkbenchSelectionMenu({ menu, onAction, onClose }: {
  menu: TextContextMenuRequest;
  onAction: (action: TextMenuAction | "quote") => Promise<void>;
  onClose: (restore: boolean) => void;
}) {
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const shut = (event: Event) => { if (!box.current?.contains(event.target as Node)) onClose(false); };
    const key = (e: KeyboardEvent) => {
      if (e.key !== "Escape" && e.key !== "Tab") return;
      if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); }
      onClose(true);
    };
    box.current?.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus();
    addEventListener("click", shut);
    addEventListener("keydown", key, true);
    addEventListener("blur", shut);
    return () => {
      removeEventListener("click", shut);
      removeEventListener("keydown", key, true);
      removeEventListener("blur", shut);
    };
  }, [menu, onClose]);
  return createPortal(
    <div
      className="tabmenu"
      role="menu"
      aria-label={t("文本操作")}
      data-action-keydown="layer.dismiss"
      ref={(el) => { box.current = el; if (el) pinToViewport(el, menu.x, menu.y); }}
      onKeyDown={(e) => {
        if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(e.key)) return;
        e.preventDefault();
        const buttons = [...e.currentTarget.querySelectorAll<HTMLButtonElement>("button:not(:disabled)")];
        const at = buttons.indexOf(document.activeElement as HTMLButtonElement);
        buttons[e.key === "Home" ? 0 : e.key === "End" ? buttons.length - 1 : (at + (e.key === "ArrowDown" ? 1 : -1) + buttons.length) % buttons.length]?.focus();
      }}
    >
      {menu.editable && <>
        <button role="menuitem" disabled={!menu.canUndo} data-action="workbench.text-undo" onClick={() => void onAction("undo")}>{t("撤销编辑")}</button>
        <button role="menuitem" disabled={!menu.canRedo} data-action="workbench.text-redo" onClick={() => void onAction("redo")}>{t("重做")}</button>
        <button role="menuitem" disabled={!menu.selected} data-action="workbench.text-cut" onClick={() => void onAction("cut")}>{t("剪切")}</button>
      </>}
      <button role="menuitem" disabled={!menu.selected} data-action="workbench.text-copy" onClick={() => void onAction("copy")}>{t("复制")}</button>
      {menu.editable && <button role="menuitem" data-action="workbench.text-paste" onClick={() => void onAction("paste")}>{t("粘贴")}</button>}
      <button role="menuitem" data-action="workbench.text-select-all" onClick={() => void onAction("selectAll")}>{t("全选")}</button>
      {menu.quote && menu.selected && <button role="menuitem" data-action="workbench.quote-selection" onClick={() => void onAction("quote")}>{t("加入对话")}</button>}
    </div>, document.body,
  );
}
