import { useEffect, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { ProgressWatchSettings } from "../port/boundary";
import { Group } from "./Group";
import { Switch } from "./Switch";

type Draft = { rounds: string; tokenMultiple: string };
const LIMIT = 1000;

// When a run reads as no longer moving. The notice is always said; pausing is
// the one part that changes what a run does, so it is the only switch and it
// ships off. Both numbers feed both halves, which is why they sit under it
// rather than behind it.
export function ProgressWatch({ port }: { port: AgentPort }) {
  const [state, setState] = useState<ProgressWatchSettings | null>(null);
  const [draft, setDraft] = useState<Draft>({ rounds: "", tokenMultiple: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const adopt = (s: ProgressWatchSettings) => {
    setState(s);
    setDraft({ rounds: String(s.rounds), tokenMultiple: String(s.tokenMultiple) });
  };

  useEffect(() => {
    port.progressWatch().then(adopt).catch(() => setState(null));
  }, [port]);

  const commit = (text: string, apply: (n: number) => void) => {
    const n = Number(text);
    if (!Number.isInteger(n) || n < 1 || n > LIMIT) return setError(t("common.enterAWholeNumberFrom", { n: LIMIT }));
    apply(n);
  };

  const save = async (next: Pick<ProgressWatchSettings, "pause" | "rounds" | "tokenMultiple">) => {
    setBusy(true);
    setError("");
    try {
      adopt(await port.saveProgressWatch(next));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const body = !state ? (
    <div className="empty">{t("progressWatch.couldNotReadTheProgress")}</div>
  ) : (
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("progressWatch.pauseATaskThatStops")}</span>
          <span className="ds">{t("progressWatch.offByDefaultANotice")}</span>
        </span>
        <Switch
          data-action="progress-watch.pause"
          on={state.pause}
          busy={busy}
          label={t("progressWatch.pauseATaskThatStops")}
          onClick={() => void save({ ...pick(state), pause: !state.pause })}
        />
      </div>
      <div className="lrow threshold-row">
        <span className="tx">
          <span className="lb">{t("progressWatch.roundsWithoutProgress")}</span>
          <span className="ds">{t("progressWatch.toolRoundsInARow", { n: state.defaultRounds })}</span>
        </span>
        <div className="threshold-control">
          <span className="unit-field">
            <input type="text" inputMode="numeric" aria-label={t("progressWatch.roundsWithoutProgress")} data-action-keydown="progress-watch.rounds" data-action-change="progress-watch.rounds"
              value={draft.rounds} disabled={busy}
              onChange={(e) => setDraft({ ...draft, rounds: digits(e.target.value) })}
              onBlur={() => commit(draft.rounds, (n) => n !== state.rounds && void save({ ...pick(state), rounds: n }))}
              onKeyDown={blurOnEnter} />
            <i>{t("progressWatch.rounds")}</i>
          </span>
        </div>
      </div>
      <div className="lrow threshold-row">
        <span className="tx">
          <span className="lb">{t("progressWatch.inputTokenBackstop")}</span>
          <span className="ds">{t("progressWatch.aModelIndependentBackstopThe", { n: state.defaultTokenMultiple })}</span>
        </span>
        <div className="threshold-control">
          <span className="unit-field">
            <input type="text" inputMode="numeric" aria-label={t("progressWatch.inputTokenBackstop")} data-action-keydown="progress-watch.token-multiple" data-action-change="progress-watch.token-multiple"
              value={draft.tokenMultiple} disabled={busy}
              onChange={(e) => setDraft({ ...draft, tokenMultiple: digits(e.target.value) })}
              onBlur={() => commit(draft.tokenMultiple, (n) => n !== state.tokenMultiple && void save({ ...pick(state), tokenMultiple: n }))}
              onKeyDown={blurOnEnter} />
            <i>{t("progressWatch.window")}</i>
          </span>
        </div>
      </div>
      {error && <div className="why">{error}</div>}
    </div>
  );

  return (
    <Group id="progress-watch" title={t("common.longStretchesWithoutProgress")}
      hint={t("progressWatch.judgedOnlyFromEffectsThe")}>
      {body}
    </Group>
  );
}

const digits = (v: string) => v.replace(/\D/g, "");
const pick = (s: ProgressWatchSettings) => ({ pause: s.pause, rounds: s.rounds, tokenMultiple: s.tokenMultiple });

// Enter commits by leaving the field, so one keystroke reaches the kernel once.
const blurOnEnter = (e: KeyboardEvent<HTMLInputElement>) => {
  if (e.key === "Enter") e.currentTarget.blur();
};
