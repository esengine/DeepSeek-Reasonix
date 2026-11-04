import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { RewindUndo } from "../port/port";
import { StudioIcon } from "./StudioIcon";
import { dismissRewindNotice, useRewindNoticeDismissed } from "../state/rewind_notice";

export function RewindNotice({ readUndo, onUndo, refreshKey, moved = 0, busy = false, running, shown }: {
  readUndo: () => Promise<RewindUndo | null>;
  onUndo: (transactionId: string) => Promise<void>;
  refreshKey: unknown;
  moved?: number;
  busy?: boolean;
  running: boolean;
  shown: boolean;
}) {
  const [offer, setOffer] = useState<RewindUndo | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const reads = useRef(0);
  const dismissed = useRewindNoticeDismissed(offer?.transactionId);

  useEffect(() => {
    if (!shown || running) {
      setOffer(null);
      setError("");
      return;
    }
    if (pending) return;
    let alive = true;
    const seq = ++reads.current;
    // The event signals a change; only the backend owns the current offer.
    readUndo().then((next) => {
      if (!alive || seq !== reads.current) return;
      setOffer((prev) => prev?.transactionId === next?.transactionId && prev?.turn === next?.turn && prev?.files === next?.files ? prev : next);
      if (!next) setError("");
    }).catch(() => {
      if (alive && seq === reads.current) setOffer(null);
    });
    return () => { alive = false; };
  }, [readUndo, refreshKey, moved, running, shown, pending]);

  const undo = async () => {
    if (!offer || pending || busy) return;
    setPending(true);
    setError("");
    try {
      await onUndo(offer.transactionId);
      setOffer(null);
    } catch (e) {
      setError(reason(e));
    } finally {
      setPending(false);
    }
  };

  const dismiss = () => {
    if (!offer || pending || busy) return;
    try { dismissRewindNotice(offer.transactionId); }
    catch (e) { setError(t("app.could_not_save_dismissal_error", { error: reason(e) })); }
  };

  if (!shown || running || !offer || dismissed) return null;
  return (
    <div className="rewind-notice" role="status" aria-live="polite" aria-busy={pending || busy}>
      <button type="button" className="rewind-notice-close" data-action="rewind.notice-dismiss" data-target={offer.transactionId} aria-label={t("app.dismiss_rewind_notice")} title={t("app.dismiss_rewind_notice")} disabled={pending || busy} onClick={dismiss}>
        <StudioIcon name="close" />
      </button>
      <StudioIcon name="rewind" />
      <div className="rewind-notice-copy">
        <span>{t("app.rewind_completed")}</span>
        <span className="rewind-notice-count">{t("app.n_files_restored", { n: offer.files })}</span>
        {error && <span className="rewind-notice-error" role="alert">{error}</span>}
      </div>
      <button type="button" className="btn" data-action="rewind.undo" data-target={offer.transactionId} disabled={pending || busy} onClick={() => void undo()}>
        {t(pending ? "app.undoing" : "app.undo_this_rewind")}
      </button>
    </div>
  );
}
