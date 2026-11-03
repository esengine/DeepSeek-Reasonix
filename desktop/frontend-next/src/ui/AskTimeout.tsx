import { useEffect, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { AskTimeoutSettings } from "../port/boundary";
import { Group } from "./Group";
import { Switch } from "./Switch";

// An unanswered ask question can be skipped after a wait the user chooses. It
// ships off: skipping is the one setting here that lets a run go on without a
// decision someone else was asked for, and the model is told it was a timeout.
export function AskTimeout({ port }: { port: AgentPort }) {
  const [state, setState] = useState<AskTimeoutSettings | null>(null);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const adopt = (s: AskTimeoutSettings) => {
    setState(s);
    setDraft(String(s.seconds || s.defaultSeconds));
  };

  useEffect(() => {
    port.askTimeout().then(adopt).catch(() => setState(null));
  }, [port]);

  const save = async (seconds: number) => {
    setBusy(true);
    setError("");
    try {
      adopt(await port.saveAskTimeout(seconds));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const commit = () => {
    if (!state || state.seconds === 0) return;
    const n = Number(draft);
    if (!Number.isInteger(n) || n < state.minSeconds || n > state.maxSeconds) {
      return setError(t("common.enterAWholeNumberFrom2", { min: state.minSeconds, max: state.maxSeconds }));
    }
    if (n !== state.seconds) void save(n);
  };

  const on = !!state && state.seconds > 0;
  const body = !state ? (
    <div className="empty">{t("askTimeout.theQuestionWaitSettingCould")}</div>
  ) : (
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("askTimeout.skipAQuestionNobodyAnswers")}</span>
          <span className="ds">{t("askTimeout.offByDefaultAQuestion")}</span>
        </span>
        <Switch
          data-action="ask-timeout.enabled"
          on={on}
          busy={busy}
          label={t("askTimeout.skipAQuestionNobodyAnswers")}
          onClick={() => void save(on ? 0 : state.defaultSeconds)}
        />
      </div>
      {on && (
        <div className="lrow threshold-row">
          <span className="tx">
            <span className="lb">{t("askTimeout.waitTime")}</span>
            <span className="ds">{t("askTimeout.countedFromWhenTheKernel", { n: state.defaultSeconds })}</span>
          </span>
          <div className="threshold-control">
            <span className="unit-field">
              <input type="text" inputMode="numeric" aria-label={t("askTimeout.waitTime")} data-action-keydown="ask-timeout.seconds" data-action-change="ask-timeout.seconds"
                value={draft} disabled={busy}
                onChange={(e) => setDraft(e.target.value.replace(/\D/g, ""))}
                onBlur={commit}
                onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => { if (e.key === "Enter") e.currentTarget.blur(); }} />
              <i>{t("common.s")}</i>
            </span>
          </div>
        </div>
      )}
      {state.invalid && <div className="why">{t("askTimeout.theQuestionWaitInThe", { min: state.minSeconds, max: state.maxSeconds, path: state.path })}</div>}
      {error && <div className="why">{error}</div>}
    </div>
  );

  return <Group id="ask-timeout" title={t("common.questionWaitTime")} hint={t("askTimeout.aLongTaskNeedNot")}>{body}</Group>;
}
