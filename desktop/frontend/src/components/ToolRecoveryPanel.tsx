import { useEffect, useRef, useState } from "react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { RecoveryCall, ToolRecoveryBindings, ToolRecoveryRequest, ToolRecoverySnapshot } from "../lib/toolRecovery";
import "./ToolRecoveryPanel.css";

export function ToolRecoveryPanel({ tabId, sessionKey, running, refreshKey, onResume, bindings = app }: {
  tabId: string; sessionKey: string; running: boolean; refreshKey: number;
  onResume?: () => void; bindings?: ToolRecoveryBindings;
}) {
  const t = useT();
  const generation = useRef(0);
  const [snapshot, setSnapshot] = useState<ToolRecoverySnapshot | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [resolved, setResolved] = useState(false);
  useEffect(() => {
    const own = ++generation.current;
    setSnapshot(null); setError(""); setBusy(false); setResolved(false);
    if (!running && bindings.GetToolRecoveryForTab) {
      void bindings.GetToolRecoveryForTab(tabId).then(next => {
        if (generation.current === own) setSnapshot(next);
      }).catch(err => { if (generation.current === own) setError(String(err)); });
    }
    return () => { generation.current++; };
  }, [bindings, tabId, sessionKey, running, refreshKey]);

  const act = async (call: RecoveryCall, action: ToolRecoveryRequest["action"]) => {
    if (!snapshot || busy || running || !bindings.ResolveToolRecoveryForTab) return;
    const own = generation.current;
    setBusy(true); setError("");
    try {
      const next = await bindings.ResolveToolRecoveryForTab(tabId, {
        sessionPath: snapshot.sessionPath, runtimeEpoch: snapshot.runtimeEpoch, revision: snapshot.revision,
        attemptId: call.identity.attempt_id ?? "", inspectionId: call.inspection_id ?? "", action,
      });
      if (generation.current === own) { setSnapshot(next); setResolved(next.calls.length === 0); }
    } catch (err) {
      if (generation.current === own) {
        setError(String(err));
        // Read back after a lost response or stale revision; never replay a
        // potentially committed action automatically.
        try {
          const fresh = await bindings.GetToolRecoveryForTab?.(tabId);
          if (fresh && fresh.sessionPath === snapshot.sessionPath && generation.current === own) {
            setSnapshot(fresh); setResolved(fresh.calls.length === 0);
          }
        } catch { /* Keep the original error and its action identity visible. */ }
      }
    } finally { if (generation.current === own) setBusy(false); }
  };
  if (!snapshot?.calls.length && !snapshot?.silent && !error && !resolved) return null;
  return <section className="notice-line notice-line--warn tool-recovery-panel" aria-label={t("toolRecovery.title")} aria-busy={busy}>
    <details open>
    <summary className="notice-line__title">{t("toolRecovery.title")}</summary>
    <div className="notice-line__text">
      {error && <p role="alert">{error}</p>}
      {(snapshot?.calls ?? []).map(call => <div key={call.identity.attempt_id}>
        <p>{call.identity.canonical_tool} · {t("toolRecovery.unknown")}</p>
        <details><summary>{t("toolRecovery.details")}</summary>
          <p>{call.identity.resource_scope}</p>
          <p>{call.identity.argument_digest}</p>
          {call.arguments !== undefined && <pre>{JSON.stringify(call.arguments, null, 2)}</pre>}
        </details>
        {call.inspection_state && <p>{t(call.inspection_state === "present" || call.inspection_state === "postcondition_satisfied" ? "toolRecovery.present" : call.inspection_state === "absent_fenced" ? "toolRecovery.absent" : "toolRecovery.unproven")}</p>}
        {call.resolution === "reject" && <p>{t("toolRecovery.rejected")}</p>}
        <div className="notice-line__actions">
          <button type="button" className="btn btn--small" disabled={busy || running} onClick={() => void act(call, "inspect")}>{t("toolRecovery.inspect")}</button>
          <button type="button" className="btn btn--small" disabled={busy || running || !call.inspection_id} onClick={() => void act(call, "confirm")}>{t("toolRecovery.confirm")}</button>
          <button type="button" className="btn btn--small" disabled={busy || running || !call.inspection_id} onClick={() => void act(call, "reject")}>{t("toolRecovery.reject")}</button>
          {snapshot?.retryEnabled && <button type="button" className="btn btn--small" disabled={busy || running || !call.inspection_id || (!call.read_only && call.inspection_state !== "absent_fenced")} onClick={() => void act(call, "retry")}>{t("toolRecovery.retry")}</button>}
        </div>
      </div>)}
      {(resolved || snapshot?.silent) && onResume && <button type="button" className="btn btn--small" disabled={busy || running} onClick={onResume}>{t("toolRecovery.resume")}</button>}
    </div>
    </details>
  </section>;
}
