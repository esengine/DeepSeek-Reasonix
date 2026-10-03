import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { HubPort } from "../port/hub";
import { StudioIcon } from "./StudioIcon";
import "../styles/session-rename.css";

interface Props {
  session: { path: string; title: string };
  hub: Pick<HubPort, "renameSession" | "autoNameSession">;
  onClose: () => void;
  onSaved: () => void;
}

export function RenameSession({ session, hub, onClose, onSaved }: Props) {
  const dialog = useRef<HTMLDialogElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const pending = useRef(false);
  const [title, setTitle] = useState(session.title);
  const [busy, setBusy] = useState<"save" | "auto" | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const el = dialog.current!;
    el.showModal();
    input.current?.select();
    return () => { el.close(); previous?.focus(); };
  }, []);

  const submit = async (mode: "save" | "auto") => {
    if (pending.current) return;
    pending.current = true;
    setBusy(mode);
    setError("");
    try {
      if (mode === "auto") {
        const generated = await hub.autoNameSession(session.path);
        setTitle(generated.title);
        pending.current = false;
        setBusy(null);
        return;
      }
      await hub.renameSession(session.path, title.trim());
    } catch (e) {
      setError(reason(e));
      pending.current = false;
      setBusy(null);
      return;
    }
    onSaved();
  };

  return createPortal(
    <dialog ref={dialog} className="session-rename-dialog" aria-labelledby="session-rename-title"
      aria-describedby="session-rename-hint" aria-busy={!!busy}
      data-action-cancel="layer.dismiss"
      onCancel={(ev) => { ev.preventDefault(); if (!pending.current) onClose(); }}
      data-action-keydown="layer.dismiss"
      onKeyDown={(ev) => { if (ev.key === "Escape") { ev.preventDefault(); ev.stopPropagation(); if (!pending.current) onClose(); } }}>
      <form data-action-submit="session.rename" data-target={session.path}
        onSubmit={(ev) => { ev.preventDefault(); void submit("save"); }}>
        <header>
          <h2 id="session-rename-title">{t("重命名聊天")}</h2>
          <button type="button" className="session-rename-close" data-action="layer.dismiss"
            aria-label={t("关闭")} disabled={!!busy} onClick={onClose}><StudioIcon name="close" /></button>
        </header>
        <p id="session-rename-hint">{t("保持简短且易于识别")}</p>
        <input ref={input} aria-label={t("聊天标题")} placeholder={t("留空以恢复自动标题")} value={title} disabled={!!busy}
          onChange={(ev) => setTitle(ev.target.value)} autoFocus />
        {error && <p className="session-rename-error" role="alert">{error}</p>}
        <footer>
          <button type="button" className="session-rename-auto" data-action="session.auto-name" data-target={session.path}
            disabled={!!busy} title={t("根据全部用户消息拼接后的最后 300 个字符生成标题")}
            onClick={() => void submit("auto")}>{busy === "auto" ? t("命名中…") : t("自动命名")}</button>
          <button type="button" data-action="layer.dismiss" disabled={!!busy} onClick={onClose}>{t("取消")}</button>
          <button type="submit" className="session-rename-save" data-action="session.rename" data-target={session.path}
            disabled={!!busy}>{busy === "save" ? t("保存中…") : t("保存")}</button>
        </footer>
      </form>
    </dialog>, document.body,
  );
}
