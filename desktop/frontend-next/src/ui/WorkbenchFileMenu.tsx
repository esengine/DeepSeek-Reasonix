import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { copyText } from "./CopyButton";
import { pinToViewport } from "./place";
import { fileReference } from "./fileReference";

export type FileMenu = { path: string; folder: boolean; x: number; y: number; anchor: HTMLElement };

export function absoluteWorkspacePath(root: string, path: string): string {
  if (!root.startsWith("/") && !/^[a-z]:[\\/]/i.test(root) && !/^\\\\[^\\]+\\[^\\]+/.test(root)) return "";
  const separator = root.startsWith("\\\\") || /^[a-z]:\\/i.test(root) ? "\\" : "/";
  const base = root.replace(separator === "\\" ? /[\\/]+$/ : /\/+$/, "");
  return base + separator + (separator === "\\" ? path.replaceAll("/", "\\") : path);
}

export function WorkbenchFileMenu({ menu, root, revealable, platform, onReveal, onLocate, onOpen, onAddToChat, onClose }: {
  menu: FileMenu; root: string; revealable: boolean; platform: string;
  onReveal: (path: string) => void; onAddToChat?: (reference: string) => void; onClose: () => void;
  onOpen?: (path: string) => void;
  onLocate?: (path: string) => void;
}) {
  const [state, setState] = useState<"idle" | "copying" | "failed">("idle");
  const live = useRef(false);
  const close = () => { menu.anchor.focus({ preventScroll: true }); onClose(); };
  useEffect(() => {
    live.current = true;
    const shut = () => onClose();
    const key = (e: KeyboardEvent) => {
      if (e.key !== "Escape" && e.key !== "Tab") return;
      if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); }
      menu.anchor.focus({ preventScroll: true });
      onClose();
    };
    addEventListener("click", shut);
    addEventListener("keydown", key, true);
    addEventListener("blur", shut);
    return () => {
      live.current = false;
      removeEventListener("click", shut);
      removeEventListener("keydown", key, true);
      removeEventListener("blur", shut);
    };
  }, [menu, onClose]);
  const copy = async (text: string) => {
    setState("copying");
    try {
      await copyText(text);
      if (live.current) close();
    } catch {
      if (live.current) setState("failed");
    }
  };
  const absolute = absoluteWorkspacePath(root, menu.path);
  // Keep the same token spelling as control.EscapeRefPath and RefDirItems.
  const path = menu.path + (menu.folder ? "/" : "");
  const ambiguous = path.includes(":");
  const reference = fileReference(path);
  return createPortal(
    <div
      className="tabmenu"
      data-action-keydown="layer.dismiss"
      role="menu"
      ref={(el) => { if (el) pinToViewport(el, menu.x, menu.y); }}
      onClick={(e) => e.stopPropagation()}
      onKeyDown={(e) => {
        if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
        e.preventDefault();
        const items = [...e.currentTarget.querySelectorAll<HTMLButtonElement>("button:not(:disabled)")];
        const at = items.indexOf(document.activeElement as HTMLButtonElement);
        items[(at + (e.key === "ArrowDown" ? 1 : -1) + items.length) % items.length]?.focus();
      }}>
      {revealable && (
        <button role="menuitem" autoFocus data-action="workbench.reveal" data-target={menu.path}
          onClick={() => { close(); onReveal(menu.path); }}>
          {platform === "darwin" ? t("在访达中显示") : platform === "windows" ? t("在文件资源管理器中显示") : t("在系统文件管理器中显示")}
        </button>
      )}
      {onLocate && (
        <button role="menuitem" autoFocus={!revealable} data-action="workbench.locate" data-target={menu.path}
          onClick={() => { close(); onLocate(menu.path); }}>{t("在文件树中显示")}</button>
      )}
      <button
        role="menuitem"
        autoFocus={!revealable && !onLocate && !!absolute}
        disabled={!absolute || state === "copying"}
        data-action="workbench.copy-path"
        data-target={menu.path}
        onClick={() => void copy(absolute)}
      >{t("复制路径")}</button>
      <button
        role="menuitem"
        autoFocus={!revealable && !onLocate && !absolute}
        disabled={!reference || state === "copying"}
        title={reference ? undefined : ambiguous ? t("此名称可能被识别为 MCP 资源") : t("此名称不能表示为 @ 引用")}
        data-action="workbench.copy-ref"
        data-target={menu.path}
        onClick={() => void copy(reference)}
      >{t("复制引用")}</button>
      {onAddToChat && (
        <button
          role="menuitem"
          disabled={!reference}
          title={reference ? undefined : ambiguous ? t("此名称可能被识别为 MCP 资源") : t("此名称不能表示为 @ 引用")}
          data-action="workbench.add-to-chat"
          data-target={menu.path}
          onClick={() => { close(); onAddToChat(reference); }}
        >{t("加入对话")}</button>
      )}
      {onOpen && !menu.folder && (
        <button role="menuitem" data-action="workbench.open-file" data-target={menu.path}
          onClick={() => { close(); onOpen(menu.path); }}>{t("使用系统默认应用打开")}</button>
      )}
      {state === "failed" && <div role="alert">{t("复制失败，请重试")}</div>}
    </div>, document.body,
  );
}
