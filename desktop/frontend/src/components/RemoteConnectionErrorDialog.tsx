import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";

import { useT } from "../lib/i18n";
import { isRemoteHostKeyMismatch, remoteConnectionErrorSummaryKey } from "../lib/remoteErrors";
import type { RemoteConnectionStatus, RemoteHostView } from "../lib/types";

export function RemoteConnectionErrorDialog({
  host,
  status,
  onClose,
  onManage,
  onRetry,
}: {
  host: RemoteHostView;
  status: RemoteConnectionStatus;
  onClose: () => void;
  onManage?: () => void;
  onRetry?: () => void;
}) {
  const t = useT();
  const closeRef = useRef<HTMLButtonElement>(null);
  const mismatch = isRemoteHostKeyMismatch(status);
  const details = status.errorDetails;
  const target = `${host.user ? `${host.user}@` : ""}${host.host}${host.port && host.port !== 22 ? `:${host.port}` : ""}`;

  useEffect(() => {
    closeRef.current?.focus();
  }, []);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      onClose();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose]);

  return createPortal(
    <div className="remote-hostkey-overlay" role="dialog" aria-modal="true" aria-labelledby="remote-connection-error-title">
      <div className="remote-hostkey-dialog remote-connection-error-dialog">
        <h2 id="remote-connection-error-title" className="remote-hostkey-dialog__title">
          {t(mismatch ? "remote.error.hostKeyMismatch.title" : "remote.error.dialog.title")}
        </h2>
        <p className="remote-connection-error-dialog__summary">
          {t(remoteConnectionErrorSummaryKey(status), { host: host.label })}
        </p>
        <dl className="remote-hostkey-dialog__facts">
          <dt>{t("remote.error.host")}</dt>
          <dd>{target}</dd>
          {details?.presentedSha256 && (
            <>
              <dt>{t("remote.error.presentedFingerprint")}</dt>
              <dd className="remote-hostkey-dialog__fp">{details.presentedSha256}</dd>
            </>
          )}
        </dl>
        {details?.knownHostRecords && details.knownHostRecords.length > 0 && (
          <div className="remote-connection-error-dialog__records">
            <strong>{t("remote.error.knownHostRecords")}</strong>
            <ul>
              {details.knownHostRecords.map((record) => (
                <li key={`${record.path}:${record.line}`}>
                  <code>{record.path}:{record.line}</code>
                </li>
              ))}
            </ul>
          </div>
        )}
        {status.error && (
          <details className="remote-connection-error-dialog__technical">
            <summary>{t("remote.error.technicalDetails")}</summary>
            <code>{status.error}</code>
          </details>
        )}
        <div className="remote-hostkey-dialog__actions">
          <button ref={closeRef} className="btn" onClick={onClose}>
            {t("remote.error.close")}
          </button>
          {onManage && (
            <button className="btn" onClick={() => { onClose(); onManage(); }}>
              {t("remote.error.manage")}
            </button>
          )}
          {onRetry && (
            <button className="btn btn--primary" onClick={() => { onClose(); onRetry(); }}>
              {t(mismatch ? "remote.error.retryAfterFix" : "remote.error.retry")}
            </button>
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
}
