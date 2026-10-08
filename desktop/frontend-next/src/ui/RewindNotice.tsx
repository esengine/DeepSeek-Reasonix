import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { RewindUndo } from "../port/port";
import { StudioIcon } from "./StudioIcon";
import "../styles/rewind-notice.css";

const POLL_MS = 2000;

export function RewindNotice({ readUndo, onUndo, refreshKey, running, shown }: {
  readUndo: () => Promise<RewindUndo | null>;
  onUndo: (transactionId: string) => Promise<void>;
  refreshKey: unknown;
  running: boolean;
  shown: boolean;
}) {
  const [offer, setOffer] = useState<RewindUndo | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const reads = useRef(0);

  useEffect(() => {
    if (!shown || running) {
      setOffer(null);
      setError("");
      return;
    }
    if (pending) return;
    let alive = true;
    let reading = false;
    // Invalidations can arrive without a local turn, so idle panes reread the backend.
    const read = () => {
      if (reading) return;
      reading = true;
      const seq = ++reads.current;
      readUndo().then((next) => {
        if (!alive || seq !== reads.current) return;
        setOffer((prev) => prev?.transactionId === next?.transactionId && prev?.turn === next?.turn && prev?.files === next?.files ? prev : next);
        if (!next) setError("");
      }).catch(() => {
        if (alive && seq === reads.current) setOffer(null);
      }).finally(() => { reading = false; });
    };
    read();
    const timer = window.setInterval(read, POLL_MS);
    return () => { alive = false; window.clearInterval(timer); };
  }, [readUndo, refreshKey, running, shown, pending]);

  const undo = async () => {
    if (!offer || pending) return;
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

  if (!shown || running || !offer) return null;
  return (
    <div className="rewind-notice" role="status" aria-live="polite" aria-busy={pending}>
      <StudioIcon name="rewind" />
      <div className="rewind-notice-copy">
        <span>{t("回退已完成")}</span>
        {error && <span className="rewind-notice-error" role="alert">{error}</span>}
      </div>
      <button type="button" className="btn" data-action="rewind.undo" data-target={offer.transactionId} disabled={pending} onClick={() => void undo()}>
        {t(pending ? "正在撤销…" : "撤销这次回退")}
      </button>
    </div>
  );
}
