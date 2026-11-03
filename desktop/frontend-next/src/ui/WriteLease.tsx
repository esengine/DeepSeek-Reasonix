import { useEffect, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, WriteLeaseSettings } from "../port/port";
import { Switch } from "./Switch";

// One switch over how sessions in one project wait on each other. On is the
// default and trades the whole-workspace hold for concurrency; off is strict.
// It says what that costs beside the switch, and says that only sessions
// opened afterwards pick it up: a running one keeps the lease it started with.
export function WriteLease({ port }: { port: AgentPort }) {
  const [state, setState] = useState<WriteLeaseSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [wait, setWait] = useState("");

  useEffect(() => {
    port
      .writeLease()
      .then((s) => {
        setState(s);
        setWait(String(s.maxWaitMinutes));
      })
      .catch(() => setState(null));
  }, [port]);

  if (!state) return <div className="empty">{t("writeLease.couldNotReadTheMulti")}</div>;

  const flip = async () => {
    setBusy(true);
    setError("");
    try {
      setState(await port.saveWriteLease({ optimistic: !state.optimistic }));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const commitWait = async () => {
    const minutes = Number(wait);
    if (minutes === state.maxWaitMinutes) return;
    setBusy(true);
    setError("");
    try {
      const next = await port.saveWriteLease({ maxWaitMinutes: minutes });
      setState(next);
      setWait(String(next.maxWaitMinutes));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };
  const enter = (e: KeyboardEvent<HTMLInputElement>) => { if (e.key === "Enter") e.currentTarget.blur(); };
  const waitLabel = t("writeLease.longestWaitForAnotherSession");

  return (
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("writeLease.optimisticModeSessionsInParallel")}</span>
          <span className="ds">{t("writeLease.anOperationThatDeclaresNo")}</span>
        </span>
        <Switch
          data-action="write-lease.optimistic"
          on={state.optimistic}
          busy={busy}
          label={t("writeLease.optimisticModeSessionsInParallel")}
          onClick={() => void flip()}
        />
      </div>
      <div className="lrow threshold-row">
        <span className="tx">
          <span className="lb">{waitLabel}</span>
          <span className="ds">{t("writeLease.whileAnotherSessionHoldsA")}</span>
        </span>
        <div className="threshold-control">
          <span className="unit-field">
            <input type="text" inputMode="numeric" aria-label={waitLabel} data-action-keydown="write-lease.max-wait" data-action-change="write-lease.max-wait" value={wait} disabled={busy} onChange={(e) => setWait(e.target.value.replace(/\D/g, ""))} onBlur={() => void commitWait()} onKeyDown={enter} />
            <i>{t("writeLease.minutes")}</i>
          </span>
        </div>
      </div>
      {error && <div className="why" role="alert">{error}</div>}
    </div>
  );
}
