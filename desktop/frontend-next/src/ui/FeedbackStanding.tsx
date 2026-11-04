import { useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import type { FeedbackMine } from "../port/feedback";
import { FeedbackBoard } from "./FeedbackBoard";
import { FeedbackLevel } from "./FeedbackLevel";
import { FeedbackLink } from "./FeedbackLink";

/** Level, account and leaderboard as one line in the dialog's title bar, opened on
 *  request; nothing behind it is asked for until it is opened. */
export function FeedbackStanding({ port, mine, slot }: { port: AgentPort; mine: FeedbackMine; slot: HTMLElement | null }) {
  const [open, setOpen] = useState(false);
  const [boardTick, setBoardTick] = useState(0);
  const p = mine.profile;
  const body = (
    <div className="fbk-standing">
      <button type="button" className="fbk-standing-btn" data-action="feedback.account-section" aria-expanded={open}
        title={t("feedback.level_account_and_leaderboard")} onClick={() => setOpen((v) => !v)}>
        {p ? t("feedback.standing", { level: p.level, n: p.adoptedCount }) : t("feedback.level_account_and_leaderboard")}
      </button>
      {open && (
        <div className="fbk-standing-pop">
          {p ? <FeedbackLevel profile={p} offline={mine.offline} />
            : mine.items.length > 0 && <p className="fbk-hint fbk-level-gone">{t("feedback.your_level_cannot_be_shown")}</p>}
          <FeedbackLink port={port} hasReport={mine.items.length > 0} onBoardChanged={() => setBoardTick((n) => n + 1)} />
          <FeedbackBoard port={port} reload={boardTick} />
        </div>
      )}
    </div>
  );
  return slot ? createPortal(body, slot) : body;
}
