import { type ReactNode, useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { t } from "../../i18n";
import { StudioIcon } from "../StudioIcon";
import "../../styles/team.css";

// The frame every team dialog shares: modal, closes on Escape unless a write
// is in flight, and returns focus to whatever opened it.
export function TeamDialog({ title, busy, onClose, onSubmit, target, children, footer }: {
  title: string;
  busy: boolean;
  onClose: () => void;
  onSubmit: () => void;
  target?: string;
  children: ReactNode;
  footer: ReactNode;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const el = dialog.current!;
    el.showModal?.();
    return () => {
      el.close?.();
      previous?.focus();
    };
  }, []);
  return createPortal(
    <dialog ref={dialog} className="team-dialog" aria-label={title} aria-busy={busy}
      data-action-cancel="layer.dismiss"
      onCancel={(ev) => { ev.preventDefault(); if (!busy) onClose(); }}>
      <form data-action-submit="team.dialog-submit" data-target={target} onSubmit={(ev) => { ev.preventDefault(); if (!busy) onSubmit(); }}>
        <header>
          <h2>{title}</h2>
          <button type="button" className="team-close" data-action="layer.dismiss" aria-label={t("关闭")} disabled={busy} onClick={onClose}>
            <StudioIcon name="close" />
          </button>
        </header>
        {children}
        <footer>{footer}</footer>
      </form>
    </dialog>,
    document.body,
  );
}
