import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import type { Stall } from "../state/session_types";

export type StallAction = "stop" | "mute" | "continue" | "dismiss";

// The kernel's reading that the run stopped producing observable effects, said
// beside the composer. It never takes focus and never opens anything: while the
// run goes on it offers to stop it or to stop saying so; once the user's pause
// setting has ended the run, it offers to go on — a new message, which is what
// zeroes the count — or to leave it stopped.
export function StallBar({ stall, onAct }: { stall: Stall; onAct: (a: StallAction) => void }) {
  const said = stall.cause === "tokens"
    ? t("composer.about_n_the_context_window", { n: stall.tokenMultiple })
    : stall.cause === "perseveration"
      ? t("composer.the_model_is_repeating_the")
      : t("composer.no_observable_progress_for_n", { n: stall.idleRounds });
  const why = stall.cause === "tokens"
    ? t("composer.n_tokens_in_all", { n: stall.promptTokens.toLocaleString() })
    : stall.cause === "perseveration"
      ? t("composer.the_same_block_is_being")
      : t("composer.no_file_changed_no_check");
  return (
    <div className="rtbar stallbar" data-lvl="warn" role="status" data-paused={stall.paused ? "" : undefined}>
      <StudioIcon name="warning" />
      <span className="t">{stall.paused ? t("composer.task_paused_s", { s: said }) : said}</span>
      <span className="why" title={why}>{why}</span>
      {stall.paused ? (
        <>
          <button data-action="stall.continue" onClick={() => onAct("continue")}>{t("composer.continue_resets_the_count")}</button>
          <button data-action="stall.dismiss" onClick={() => onAct("dismiss")}>{t("common.stop")}</button>
        </>
      ) : (
        <>
          <button data-action="stall.stop" onClick={() => onAct("stop")}>{t("common.stop")}</button>
          <button data-action="stall.mute" onClick={() => onAct("mute")}>{t("composer.dont_show_again_this_session")}</button>
        </>
      )}
    </div>
  );
}
