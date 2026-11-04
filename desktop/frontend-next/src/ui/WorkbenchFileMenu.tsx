import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { pushNote } from "../state/toasts";
import { StudioIcon } from "./StudioIcon";
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

export function WorkbenchFileMenu({ menu, root, revealable, platform, onReveal, onOpen, onAddToChat, onClose }: {
  menu: FileMenu; root: string; revealable: boolean; platform: string;
  onReveal: (path: string) => void; onAddToChat?: (reference: string) => void; onClose: () => void;
  onOpen?: (path: string) => void;
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
  const copy = async (text: string, said: string) => {
    setState("copying");
    try {
      await copyText(text);
      pushNote(t("workbench.copied_text", { text: said }));
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
      <div className="tabmenu-head">
        <b>{menu.path.split("/").at(-1) + (menu.folder ? "/" : "")}</b>
        <span>{menu.path.includes("/") ? menu.path.slice(0, menu.path.lastIndexOf("/")) : t("workbench.root_folder")}</span>
      </div>
      {onAddToChat && (
        <button role="menuitem" autoFocus={!!reference} disabled={!reference} data-action="workbench.add-to-chat" data-target={menu.path}
          title={reference ? undefined : ambiguous ? t("workbench.this_name_could_resolve_to") : t("common.this_name_cannot_be_represented")}
          onClick={() => { close(); onAddToChat(reference); }}>
          <StudioIcon name="chat" /><span>{t("workbench.add_to_conversation")}</span>
        </button>
      )}
      <button role="menuitem" autoFocus={!onAddToChat && !!reference} disabled={!reference || state === "copying"} data-action="workbench.copy-ref" data-target={menu.path}
        aria-label={t("workbench.copy_reference")} title={reference ? undefined : ambiguous ? t("workbench.this_name_could_resolve_to") : t("common.this_name_cannot_be_represented")}
        onClick={() => void copy(reference, reference)}>
        <StudioIcon name="quote" />
        <span>{t("workbench.copy_reference")}{!reference && <small>{ambiguous ? t("workbench.this_name_could_resolve_to") : t("common.this_name_cannot_be_represented")}</small>}</span>
        {reference && <kbd>{reference}</kbd>}
      </button>
      <button role="menuitem" autoFocus={!reference} disabled={!absolute || state === "copying"} data-action="workbench.copy-path" data-target={menu.path}
        onClick={() => void copy(absolute, menu.path)}>
        <StudioIcon name="copy" /><span>{t("common.copy_path")}</span>
      </button>
      {(revealable || (onOpen && !menu.folder)) && <hr />}
      {revealable && (
        <button role="menuitem" data-action="workbench.reveal" data-target={menu.path} onClick={() => { close(); onReveal(menu.path); }}>
          <StudioIcon name="reveal" />
          <span>{platform === "darwin" ? t("workbench.reveal_in_finder") : platform === "windows" ? t("workbench.reveal_in_file_explorer") : t("workbench.show_in_file_manager")}</span>
        </button>
      )}
      {onOpen && !menu.folder && (
        <button role="menuitem" data-action="workbench.open-file" data-target={menu.path} onClick={() => { close(); onOpen(menu.path); }}>
          <StudioIcon name="external" /><span>{t("workbench.open_with_default_application")}</span>
        </button>
      )}
      {state === "failed" && <div role="alert">{t("common.copy_failed_try_again")}</div>}
    </div>, document.body,
  );
}
