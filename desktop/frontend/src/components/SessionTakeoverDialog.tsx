import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useT } from "../lib/i18n";
import { app } from "../lib/bridge";
import type { SessionTakeoverView } from "../lib/types";

/**
 * SessionTakeoverDialog confirms taking a lease-blocked session over from the
 * resident serve on this machine. The remote tab keeps watching through the
 * frame mirror and drops to read-only; when it reclaims, this window demotes
 * itself the same way.
 */
export function SessionTakeoverDialog({ tabId, onClose }: { tabId: string; onClose: () => void }) {
  const t = useT();
  const titleId = useId();
  const messageId = useId();
  const cancelRef = useRef<HTMLButtonElement>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  const [view, setView] = useState<SessionTakeoverView | null>(null);
  const [queryError, setQueryError] = useState("");
  const [actionError, setActionError] = useState("");
  const [busyMode, setBusyMode] = useState<"wait" | "interrupt" | null>(null);

  useLayoutEffect(() => {
    restoreFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    cancelRef.current?.focus();
    return () => {
      if (restoreFocusRef.current?.isConnected) restoreFocusRef.current.focus();
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    app.QuerySessionTakeover(tabId)
      .then((result) => {
        if (!cancelled) setView(result);
      })
      .catch((error) => {
        if (!cancelled) setQueryError(error instanceof Error ? error.message : String(error));
      });
    return () => {
      cancelled = true;
    };
  }, [tabId]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        if (!busyMode) onClose();
      }
    };
    document.addEventListener("keydown", onKeyDown, { capture: true });
    return () => document.removeEventListener("keydown", onKeyDown, { capture: true });
  }, [busyMode, onClose]);

  const take = (mode: "wait" | "interrupt") => {
    if (busyMode) return;
    setBusyMode(mode);
    setActionError("");
    app.TakeoverSession(tabId, mode)
      .then(onClose)
      .catch((error) => {
        setActionError(error instanceof Error ? error.message : String(error));
        setBusyMode(null);
      });
  };

  const busy = busyMode !== null;
  let body: React.ReactNode;
  if (queryError) {
    body = <span className="reasonix-confirm-dialog__message-error">{t("takeover.unavailable", { reason: queryError })}</span>;
  } else if (!view) {
    body = <span>{t("takeover.querying")}</span>;
  } else if (!view.available) {
    body = <span>{t("takeover.unavailable", { reason: view.reason || t("takeover.noHolder") })}</span>;
  } else {
    body = (
      <>
        <span>{t("takeover.descRemote")}</span>
        <span className="session-takeover-dialog__state">
          {view.running ? t("takeover.running") : t("takeover.idle")}
        </span>
      </>
    );
  }
  const canTake = !queryError && view?.available === true;

  return createPortal(
    <div
      className="modal-backdrop reasonix-confirm-backdrop"
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !busy) onClose();
      }}
    >
      <div className="modal reasonix-confirm-dialog session-takeover-dialog" role="dialog" aria-modal="true" aria-labelledby={titleId} aria-describedby={messageId}>
        <div className="modal__title reasonix-confirm-dialog__title" id={titleId}>{t("takeover.title")}</div>
        <div className="reasonix-confirm-dialog__message" id={messageId}>
          {body}
          {actionError ? <span className="reasonix-confirm-dialog__message-error">{actionError}</span> : null}
        </div>
        <div className="modal__actions reasonix-confirm-dialog__actions">
          <button ref={cancelRef} className="btn btn--small" type="button" disabled={busy} onClick={onClose}>
            {t("takeover.cancel")}
          </button>
          {canTake ? (
            <button className="btn btn--small btn--primary" type="button" disabled={busy} onClick={() => take("wait")}>
              {busyMode === "wait" ? t("takeover.busy") : view?.running ? t("takeover.takeWait") : t("takeover.takeIdle")}
            </button>
          ) : null}
          {canTake && view?.running ? (
            <button className="btn btn--small btn--danger" type="button" disabled={busy} onClick={() => take("interrupt")}>
              {busyMode === "interrupt" ? t("takeover.busy") : t("takeover.takeInterrupt")}
            </button>
          ) : null}
        </div>
      </div>
    </div>,
    document.body,
  );
}
