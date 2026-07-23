import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";

import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { useRemoteStore } from "../store/remote";

/** RemoteHostKeyDialog is the global TOFU confirmation modal. It renders off
 *  the remote store's pendingFingerprint so it appears regardless of which
 *  surface initiated the connection, and resolves via ConfirmRemoteHostKey. */
export function RemoteHostKeyDialog() {
  const t = useT();
  const fp = useRemoteStore((s) => s.pendingFingerprint);
  const clear = useRemoteStore((s) => s.clearPendingFingerprint);
  const acceptRef = useRef<HTMLButtonElement>(null);
  const resolvingRef = useRef(false);

  useEffect(() => {
    if (fp) acceptRef.current?.focus();
  }, [fp]);

  useEffect(() => {
    if (!fp) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        void resolve(false);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fp]);

  if (!fp) return null;

  const resolve = async (accept: boolean) => {
    if (resolvingRef.current) return;
    resolvingRef.current = true;
    try {
      await app.ConfirmRemoteHostKey(fp.hostId, accept);
    } finally {
      clear(fp);
      resolvingRef.current = false;
    }
  };

  return createPortal(
    <div className="remote-hostkey-overlay" role="dialog" aria-modal="true" aria-labelledby="remote-hostkey-title">
      <div className="remote-hostkey-dialog">
        <h2 id="remote-hostkey-title" className="remote-hostkey-dialog__title">
          {t("remote.fingerprint.title")}
        </h2>
        <p>{t("remote.fingerprint.body", { host: fp.hostId })}</p>
        <dl className="remote-hostkey-dialog__facts">
          <dt>{t("remote.fingerprint.type")}</dt>
          <dd>{fp.keyType}</dd>
          <dt>{t("remote.fingerprint.sha256")}</dt>
          <dd className="remote-hostkey-dialog__fp">{fp.sha256}</dd>
        </dl>
        <div className="remote-hostkey-dialog__actions">
          <button className="btn" onClick={() => void resolve(false)}>
            {t("remote.fingerprint.reject")}
          </button>
          <button ref={acceptRef} className="btn btn--danger" onClick={() => void resolve(true)}>
            {t("remote.fingerprint.accept")}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}
