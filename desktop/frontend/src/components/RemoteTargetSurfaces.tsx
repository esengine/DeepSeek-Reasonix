import { useCallback, useEffect, useState, type FormEvent } from "react";
import { AlertTriangle, Loader2, RotateCw, Server } from "lucide-react";

import { app, onRemoteAskPass, onRemoteTargetState } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { RemoteAskPassKind, RemoteAskPassView, RemoteTargetStatusView } from "../lib/types";
import { RemoteWorkspaceSetup } from "./RemoteWorkspaceSetup";

function errorText(error: unknown): string {
  if (error && typeof error === "object" && "message" in error && typeof error.message === "string") {
    return error.message;
  }
  return String(error);
}

function askPassKindKey(kind: RemoteAskPassKind) {
  return `remote.askpass.${kind}` as const;
}

function targetStateKey(state: RemoteTargetStatusView["state"]) {
  return `remote.state.${state}` as const;
}

function statusNeedsSurface(status: RemoteTargetStatusView | null): status is RemoteTargetStatusView {
  return Boolean(status && (status.state !== "LocalConnected" || status.failure || status.canReconnect));
}

export interface RemoteTargetSurfacesProps {
  workspaceSetupRequest?: number;
}

export function RemoteTargetSurfaces({ workspaceSetupRequest = 0 }: RemoteTargetSurfacesProps) {
  const t = useT();
  const [status, setStatus] = useState<RemoteTargetStatusView | null>(null);
  const [prompt, setPrompt] = useState<RemoteAskPassView | null>(null);
  const [answer, setAnswer] = useState("");
  const [targetBusy, setTargetBusy] = useState(false);
  const [targetError, setTargetError] = useState("");
  const [responseError, setResponseError] = useState("");
  const [confirmLocal, setConfirmLocal] = useState(false);

  useEffect(() => {
    let active = true;
    void (async () => {
      try {
        const next = await app.RemoteTargetStatus();
        if (active) setStatus(next);
      } catch {
        // During a rolling Desktop upgrade an older backend can briefly lack the
        // new binding. Do not replace the real workbench with mock connection UI.
      }
    })();
    const offStatus = onRemoteTargetState((next) => {
      setStatus(next);
      setConfirmLocal(false);
      setTargetError("");
    });
    const offAskPass = onRemoteAskPass((next) => {
      if (!next.requestId || !next.prompt) return;
      setAnswer("");
      setResponseError("");
      setPrompt(next);
    });
    return () => {
      active = false;
      offStatus();
      offAskPass();
    };
  }, []);

  useEffect(() => {
    if (!prompt) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      void respondToPrompt(true);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [prompt]);

  const refreshStatus = useCallback(async () => {
    setStatus(await app.RemoteTargetStatus());
  }, []);

  const reconnect = useCallback(async () => {
    setTargetBusy(true);
    setTargetError("");
    try {
      await app.ReconnectRemoteTarget();
      await refreshStatus();
    } catch (cause) {
      setTargetError(errorText(cause));
    } finally {
      setTargetBusy(false);
    }
  }, [refreshStatus]);

  const switchLocal = useCallback(async () => {
    setTargetBusy(true);
    setTargetError("");
    try {
      await app.SwitchToLocalTarget(true);
      await refreshStatus();
      setConfirmLocal(false);
    } catch (cause) {
      setTargetError(errorText(cause));
    } finally {
      setTargetBusy(false);
    }
  }, [refreshStatus]);

  const respondToPrompt = useCallback(async (cancelled: boolean) => {
    const current = prompt;
    if (!current) return;
    const value = cancelled ? "" : answer;
    // Remove the credential-bearing controlled input before crossing the IPC
    // boundary. Failures show only the error; the value is never restored.
    setAnswer("");
    setPrompt(null);
    setResponseError("");
    try {
      await app.RespondRemoteAskPass(current.requestId, value, cancelled);
    } catch (cause) {
      setResponseError(errorText(cause));
    }
  }, [answer, prompt]);

  const submitPrompt = useCallback((event: FormEvent) => {
    event.preventDefault();
    void respondToPrompt(false);
  }, [respondToPrompt]);

  const hostKeyConfirm = prompt?.kind === "host_key_confirm";
  const hostKeyChanged = prompt?.kind === "host_key_changed";
  const promptNeedsValue = Boolean(prompt && !hostKeyConfirm && !hostKeyChanged);
  const remoteActive = status?.state === "RemoteConnected" || status?.state === "RemoteReconnecting";

  return (
    <>
      {statusNeedsSurface(status) && (
        <aside className={`remote-target-surface remote-target-surface--${status.state.toLowerCase()}`} aria-live="polite">
          <div className="remote-target-surface__identity">
            {status.failure ? <AlertTriangle size={16} aria-hidden="true" /> : <Server size={16} aria-hidden="true" />}
            <span>
              <strong>{t(targetStateKey(status.state))}</strong>
              {status.hostLabel && <small>{status.hostLabel}</small>}
            </span>
          </div>
          {(status.failure || targetError) && <div className="remote-target-surface__failure" role="alert">{status.failure || targetError}</div>}
          <div className="remote-target-surface__actions">
            {status.canReconnect && (
              <button className="btn btn--small" type="button" disabled={targetBusy} onClick={() => void reconnect()}>
                {targetBusy ? <Loader2 className="spin" size={13} aria-hidden="true" /> : <RotateCw size={13} aria-hidden="true" />}
                {t("remote.action.reconnect")}
              </button>
            )}
            {remoteActive && !confirmLocal && (
              <button className="btn btn--small" type="button" disabled={targetBusy || status.state === "RemoteReconnecting"} onClick={() => setConfirmLocal(true)}>
                {t("remote.action.switchLocal")}
              </button>
            )}
            {confirmLocal && (
              <div className="remote-target-surface__confirm" role="group" aria-label={t("remote.switch.confirmTitle")}>
                <span>{t("remote.switch.confirm")}</span>
                <button className="btn btn--small btn--danger" type="button" disabled={targetBusy} onClick={() => void switchLocal()}>{t("common.confirm")}</button>
                <button className="btn btn--small" type="button" disabled={targetBusy} onClick={() => setConfirmLocal(false)}>{t("common.cancel")}</button>
              </div>
            )}
          </div>
        </aside>
      )}

      <RemoteWorkspaceSetup target={status} requestSignal={workspaceSetupRequest} />

      {responseError && <div className="remote-askpass-error banner banner--error" role="alert">{responseError}</div>}

      {prompt && (
        <div className="management-modal-backdrop remote-askpass-backdrop">
          <form className="management-modal remote-askpass-modal" role="dialog" aria-modal="true" aria-labelledby="remote-askpass-title" onSubmit={submitPrompt}>
            <header className="management-modal__head">
              <div>
                <div className="management-modal__title" id="remote-askpass-title">{t("remote.askpass.title")}</div>
                {prompt.hostLabel && <div className="remote-askpass-modal__host">{t("remote.askpass.host", { host: prompt.hostLabel })}</div>}
              </div>
            </header>
            <div className="remote-askpass-modal__body">
              <div className="remote-askpass-modal__kind">{t(askPassKindKey(prompt.kind))}</div>
              <pre className="remote-askpass-modal__prompt">{prompt.prompt}</pre>
              {hostKeyConfirm && <div className="banner banner--warning">{t("remote.askpass.hostKeyHint")}</div>}
              {hostKeyChanged && <div className="banner banner--error" role="alert">{t("remote.askpass.changedBlocked")}</div>}
              {promptNeedsValue && (
                <label className="remote-askpass-modal__field">
                  <span>{t("remote.askpass.value")}</span>
                  <input
                    autoFocus
                    autoComplete="off"
                    name="remote-askpass-response"
                    type={prompt.secret ? "password" : "text"}
                    value={answer}
                    onInput={(event) => setAnswer(event.currentTarget.value)}
                    placeholder={t("remote.askpass.valuePlaceholder")}
                  />
                </label>
              )}
            </div>
            <footer className="remote-askpass-modal__actions">
              <button className="btn" type="button" onClick={() => void respondToPrompt(true)}>{t("common.cancel")}</button>
              {!hostKeyChanged && (
                <button className="btn btn--primary" type="submit" disabled={promptNeedsValue && answer.length === 0}>{t("remote.askpass.submit")}</button>
              )}
            </footer>
          </form>
        </div>
      )}
    </>
  );
}

export default RemoteTargetSurfaces;
