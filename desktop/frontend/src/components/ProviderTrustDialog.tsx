import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

import { app, onProviderTrust } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { ProviderTrustPrompt } from "../lib/workbenchTarget";

/** Authorizes a verified Remote Host to consume selected local providers.
 * Provider credentials and endpoint details never enter this component. */
export function ProviderTrustDialog() {
  const t = useT();
  const [prompt, setPrompt] = useState<ProviderTrustPrompt | null>(null);
  const acceptRef = useRef<HTMLButtonElement>(null);
  const resolvingRef = useRef(false);

  useEffect(() => {
    let active = true;
    const off = onProviderTrust((next) => {
      if (active && next?.hostId) setPrompt(next);
    });
    void app.WorkbenchPendingProviderTrust()
      .then((pending) => {
        if (active && pending?.hostId) setPrompt(pending);
      })
      .catch(() => undefined);
    return () => {
      active = false;
      off();
    };
  }, []);

  useEffect(() => {
    resolvingRef.current = false;
    if (prompt) queueMicrotask(() => acceptRef.current?.focus());
  }, [prompt]);

  if (!prompt) return null;

  const resolve = async (accept: boolean) => {
    if (resolvingRef.current) return;
    resolvingRef.current = true;
    try {
      await app.WorkbenchResolveProviderTrust(accept);
      setPrompt(null);
    } catch {
      resolvingRef.current = false;
    }
  };

  return createPortal(
    <div className="remote-hostkey-overlay" role="dialog" aria-modal="true" aria-labelledby="provider-trust-title">
      <div className="remote-hostkey-dialog">
        <h2 id="provider-trust-title" className="remote-hostkey-dialog__title">
          {t("remote.providerTrust.title")}
        </h2>
        <p>{t("remote.providerTrust.body", { host: prompt.host, workspace: prompt.workspace })}</p>
        <dl className="remote-hostkey-dialog__facts">
          <dt>{t("remote.providerTrust.hostKey")}</dt>
          <dd className="remote-hostkey-dialog__fp">{prompt.keyType} · {prompt.fingerprint}</dd>
          <dt>{t("remote.providerTrust.providers")}</dt>
          <dd>{prompt.providerRefs.join(", ")}</dd>
        </dl>
        <p>{prompt.warning}</p>
        <div className="remote-hostkey-dialog__actions">
          <button className="btn" onClick={() => void resolve(false)}>{t("remote.providerTrust.reject")}</button>
          <button ref={acceptRef} className="btn btn--primary" onClick={() => void resolve(true)}>
            {t("remote.providerTrust.accept")}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}
