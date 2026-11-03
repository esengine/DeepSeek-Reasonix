import { t } from "../i18n";
import type { Stall } from "../state/session_types";

export type StallAction = "stop" | "mute" | "continue" | "dismiss";

// The kernel's reading that the run stopped producing observable effects, said
// beside the composer. It never takes focus and never opens anything: while the
// run goes on it offers to stop it or to stop saying so; once the user's pause
// setting has ended the run, it offers to go on — a new message, which is what
// zeroes the count — or to leave it stopped.
export function StallBar({ stall, onAct }: { stall: Stall; onAct: (a: StallAction) => void }) {
  const said = stall.cause === "tokens"
    ? t("stallBar.aboutTheContextWindowIn", { n: stall.tokenMultiple })
    : stall.cause === "perseveration"
      ? t("stallBar.theModelIsRepeatingThe")
      : t("stallBar.noObservableProgressForRounds", { n: stall.idleRounds });
  const why = stall.cause === "tokens"
    ? t("stallBar.tokensInAll", { n: stall.promptTokens.toLocaleString() })
    : stall.cause === "perseveration"
      ? t("stallBar.theSameBlockIsBeing")
      : t("stallBar.noFileChangedNoCheck");
  return (
    <div className="rtbar stallbar" data-lvl="warn" role="status" data-paused={stall.paused ? "" : undefined}>
      <span className="t">{stall.paused ? t("stallBar.taskPaused", { s: said }) : said}</span>
      <span className="why" title={why}>{why}</span>
      {stall.paused ? (
        <>
          <button data-action="stall.continue" onClick={() => onAct("continue")}>{t("stallBar.continueResetsTheCount")}</button>
          <button data-action="stall.dismiss" onClick={() => onAct("dismiss")}>{t("common.stop")}</button>
        </>
      ) : (
        <>
          <button data-action="stall.stop" onClick={() => onAct("stop")}>{t("common.stop")}</button>
          <button data-action="stall.mute" onClick={() => onAct("mute")}>{t("stallBar.donTShowAgainThis")}</button>
        </>
      )}
    </div>
  );
}
