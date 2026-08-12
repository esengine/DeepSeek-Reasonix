import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Clock3, MessageSquareText } from "lucide-react";
import { useT } from "../lib/i18n";
import type { RecoverySessionCandidate } from "../lib/types";

export function RecoverySessionDialog({
  candidates,
  busy,
  onConfirm,
  onCancel,
}: {
  candidates: RecoverySessionCandidate[];
  busy: boolean;
  onConfirm: (path: string) => void;
  onCancel: () => void;
}) {
  const t = useT();
  const titleId = useId();
  const [selected, setSelected] = useState("");
  const cancelRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    cancelRef.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !busy) onCancel();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [busy, onCancel]);

  return createPortal(
    <div className="modal-backdrop recovery-session-backdrop" role="presentation">
      <section className="modal recovery-session-dialog" role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <h2 id={titleId} className="recovery-session-dialog__title">{t("history.recoveryChooseTitle")}</h2>
        <p className="recovery-session-dialog__body">{t("history.recoveryChooseBody")}</p>
        <div className="recovery-session-dialog__list" role="radiogroup">
          {candidates.map((candidate) => (
            <label className="recovery-session-dialog__option" key={candidate.path}>
              <input
                type="radio"
                name="recovery-session"
                value={candidate.path}
                checked={selected === candidate.path}
                disabled={busy}
                onChange={() => setSelected(candidate.path)}
              />
              <span className="recovery-session-dialog__content">
                <strong>{candidate.summary}</strong>
                <span className="recovery-session-dialog__meta">
                  <span><Clock3 size={14} aria-hidden="true" />{new Date(candidate.lastActivityAt).toLocaleString()}</span>
                  <span><MessageSquareText size={14} aria-hidden="true" />{t("history.recoveryRounds", { count: candidate.turns })}</span>
                </span>
              </span>
            </label>
          ))}
        </div>
        <div className="modal__actions">
          <button ref={cancelRef} className="btn btn--small" type="button" disabled={busy} onClick={onCancel}>
            {t("history.recoveryChooseCancel")}
          </button>
          <button className="btn btn--small btn--primary" type="button" disabled={!selected || busy} onClick={() => onConfirm(selected)}>
            {t("history.recoveryChooseConfirm")}
          </button>
        </div>
      </section>
    </div>,
    document.body,
  );
}
