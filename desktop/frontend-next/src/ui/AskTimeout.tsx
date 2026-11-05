import { useEffect, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { AskTimeoutSettings } from "../port/boundary";
import { SettingRow } from "./SettingRow";
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
      return setError(t("common.enter_a_whole_number_from", { min: state.minSeconds, max: state.maxSeconds }));
    }
    if (n !== state.seconds) void save(n);
  };

  const on = !!state && state.seconds > 0;
  if (!state) return <div className="empty">{t("safety.the_question_wait_setting_could")}</div>;
  return (
    <>
      <SettingRow id="ask-timeout" title={t("safety.question_wait_time")} desc={t("settings.skip_after_no_answer")}>
        {on && (
          <span className="unit-field">
            <input type="text" inputMode="numeric" aria-label={t("safety.wait_time")} data-action-keydown="ask-timeout.seconds" data-action-change="ask-timeout.seconds"
              value={draft} disabled={busy}
              onChange={(e) => setDraft(e.target.value.replace(/\D/g, ""))}
              onBlur={commit}
              onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => { if (e.key === "Enter") e.currentTarget.blur(); }} />
            <i>{t("common.s")}</i>
          </span>
        )}
        <Switch data-action="ask-timeout.enabled" on={on} busy={busy} label={t("safety.skip_a_question_nobody_answers")}
          onClick={() => void save(on ? 0 : state.defaultSeconds)} />
      </SettingRow>
      {state.invalid && <div className="why">{t("safety.the_question_wait_in_the", { min: state.minSeconds, max: state.maxSeconds, path: state.path })}</div>}
      {error && <div className="why">{error}</div>}
    </>
  );
}
