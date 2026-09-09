import { CloudOff, Loader2, TriangleAlert } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";
import { useT } from "../lib/i18n";
import type { SessionAvailability } from "../lib/sessionAvailability";

/** A persistent status region, outside the collapsible transcript. Key by session identity. */
export function SessionRecoveryBanner({ availability, onRetry }: {
  availability: SessionAvailability;
  onRetry?: () => Promise<unknown>;
}) {
  const t = useT();
  const detailId = useId();
  const mounted = useRef(false);
  const inFlight = useRef(false);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState("");
  const [expanded, setExpanded] = useState(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => { setActionError(""); setExpanded(false); }, [availability.kind, availability.detail]);
  const retry = async () => {
    if (!onRetry || inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setActionError("");
    try {
      await onRetry();
    } catch (error) {
      if (mounted.current) setActionError(error instanceof Error ? error.message : String(error));
    } finally {
      inFlight.current = false;
      if (mounted.current) setBusy(false);
    }
  };
  // Runtime setup errors already have their own startup/lease recovery controls.
  if (availability.kind === "ready" || availability.source === "runtime") return null;
  const loading = busy || availability.kind === "loading";
  const connection = availability.source === "connection";
  const detail = actionError || availability.detail;
  const Icon = loading ? Loader2 : connection ? CloudOff : TriangleAlert;
  return (
    <section className={`session-recovery${loading ? " session-recovery--loading" : ""}`} role={loading ? "status" : "alert"} aria-busy={loading}>
      <Icon size={28} aria-hidden="true" className={loading ? "session-recovery__spinner" : undefined} />
      <div className="session-recovery__copy">
        <strong>{t(loading ? connection ? "remoteSurface.connecting" : "sessionRecovery.loadingHistory"
          : connection ? "sessionRecovery.connectionLost" : "sessionRecovery.historyFailed")}</strong>
        <span>{t(actionError ? "sessionRecovery.retryFailed" : connection ? "sessionRecovery.connectionHint" : "sessionRecovery.historyHint")}</span>
      </div>
      {onRetry && <button type="button" className="btn btn--primary btn--small" disabled={loading} onClick={() => void retry()}>
        {t(loading ? "common.loading" : connection ? "remoteSurface.reconnect" : "sessionRecovery.retryHistory")}
      </button>}
      {detail && <button type="button" className="btn btn--ghost btn--small" aria-expanded={expanded} aria-controls={detailId} onClick={() => setExpanded(value => !value)}>
        {t("sessionRecovery.details")}
      </button>}
      {detail && expanded && <pre className="session-recovery__detail" id={detailId}>{detail}</pre>}
    </section>
  );
}

export function SessionRecoveryPlaceholder({ availability }: { availability: SessionAvailability }) {
  const t = useT();
  const loading = availability.kind === "loading";
  const Icon = loading ? Loader2 : availability.source === "connection" ? CloudOff : TriangleAlert;
  return <div className="session-recovery-placeholder">
    <Icon size={30} aria-hidden="true" className={loading ? "session-recovery__spinner" : undefined} />
    <span>{t(loading ? "common.loading" : "sessionRecovery.contentAfterRecovery")}</span>
  </div>;
}
