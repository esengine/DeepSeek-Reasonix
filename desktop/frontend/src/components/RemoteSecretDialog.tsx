import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { useRemoteStore } from "../store/remote";

/** Global, one-shot SSH password/private-key passphrase prompt. The secret is
 * sent directly to Go and never enters the shared store or a status event. */
export function RemoteSecretDialog() {
  const t = useT();
  const prompt = useRemoteStore((s) => s.pendingSecretPrompt);
  const clear = useRemoteStore((s) => s.clearPendingSecretPrompt);
  const [secret, setSecret] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const resolvingRef = useRef(false);

  useEffect(() => {
    setSecret("");
    resolvingRef.current = false;
    if (prompt) queueMicrotask(() => inputRef.current?.focus());
  }, [prompt]);

  if (!prompt) return null;

  const resolve = async (accept: boolean) => {
    if (resolvingRef.current) return;
    resolvingRef.current = true;
    try {
      await app.ConfirmRemoteSecret(prompt.hostId, prompt.promptId, accept ? secret : "", accept);
    } finally {
      clear(prompt);
      setSecret("");
      resolvingRef.current = false;
    }
  };

  return createPortal(
    <div className="remote-hostkey-overlay" role="dialog" aria-modal="true" aria-labelledby="remote-secret-title">
      <form
        className="remote-hostkey-dialog"
        onSubmit={(event) => {
          event.preventDefault();
          void resolve(true);
        }}
      >
        <h2 id="remote-secret-title" className="remote-hostkey-dialog__title">
          {t(`remote.secret.${prompt.kind}.title`)}
        </h2>
        <p>{t(`remote.secret.${prompt.kind}.body`, { host: prompt.host })}</p>
        {prompt.kind === "passphrase" && prompt.identity ? (
          <p>{t("remote.secret.passphrase.identity", { identity: prompt.identity })}</p>
        ) : null}
        <input
          ref={inputRef}
          className="remote-secret-dialog__input"
          type="password"
          value={secret}
          autoComplete="off"
          aria-label={t(`remote.secret.${prompt.kind}.label`)}
          onChange={(event) => setSecret(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.preventDefault();
              void resolve(false);
            }
          }}
        />
        <div className="remote-hostkey-dialog__actions">
          <button type="button" className="btn" onClick={() => void resolve(false)}>
            {t("remote.secret.cancel")}
          </button>
          <button type="submit" className="btn btn--primary">
            {t("remote.secret.continue")}
          </button>
        </div>
      </form>
    </div>,
    document.body,
  );
}
