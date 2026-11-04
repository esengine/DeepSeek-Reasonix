import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { HubPort } from "../port/hub";
import { StudioIcon } from "./StudioIcon";

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
    if (mode === "save") {
      const next = title.trim();
      if (next === "" || next === session.title.trim()) {
        onClose();
        return;
      }
    }
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
          <h2 id="session-rename-title">{t("session.rename_chat")}</h2>
          <button type="button" className="session-rename-close" data-action="layer.dismiss"
            aria-label={t("common.close")} disabled={!!busy} onClick={onClose}><StudioIcon name="close" /></button>
        </header>
        <p id="session-rename-hint">{t("session.keep_it_short_and_recognizable")}</p>
        <input ref={input} data-action="session.rename.title" aria-label={t("common.chat_title")} placeholder={t("common.chat_title")} value={title} disabled={!!busy}
          onChange={(ev) => setTitle(ev.target.value)} autoFocus />
        {error && <p className="session-rename-error" role="alert">{error}</p>}
        <footer>
          <button type="button" className="session-rename-auto" data-action="session.auto-name" data-target={session.path}
            disabled={!!busy} title={t("session.generate_a_title_from_the")}
            onClick={() => void submit("auto")}>{busy === "auto" ? t("session.naming") : t("session.auto_name")}</button>
          <button type="button" data-action="layer.dismiss" disabled={!!busy} onClick={onClose}>{t("common.cancel")}</button>
          <button type="submit" className="session-rename-save" data-action="session.rename" data-target={session.path}
            disabled={!!busy}>{busy === "save" ? t("common.saving") : t("common.save")}</button>
        </footer>
      </form>
    </dialog>, document.body,
  );
}
