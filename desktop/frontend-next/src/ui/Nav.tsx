import { FEEDBACK_RETIRED } from "../feedbackchannel";
import { t } from "../i18n";
import { FeedbackBadge } from "./feedbackentry";
import { useDestinations, type Mark } from "./navdest";
import { ICON } from "./prefsnav";
import { StudioIcon } from "./StudioIcon";
import { UpdateDot } from "./UpdateDot";

// Every entry goes somewhere that exists. A rail of icons that open nothing is
// the cheapest thing to draw and the most expensive thing to keep.
const HOME: [Mark, string, string | null] = ["chat", "common.session", null];

const FOOT: [Mark, string, string, string?][] = [
  ["account", "settings.account", "account"],
  ["advanced", "common.settings", "", "chrome.settings"],
];

interface Props {
  at: string | null;
  onGo: (sec?: string) => void;
  onFiles: () => void;
  onHome: () => void;
  /** The conversation item already shows the conversation, so pressing it again
   *  does what the rail beside it can: open or close the workspace column. */
  onRail: () => void;
  onFeedback: () => void;
  /** A new session in the window's folder, the same action as the sidebar's. */
  onNew: () => void;
  feedbackUnread: number;
  /** False keeps the column mounted but out of the way and out of reach. */
  shown: boolean;
}

export function Nav({ at, onGo, onFiles, onHome, onRail, onFeedback, onNew, feedbackUnread, shown }: Props) {
  const destinations = useDestinations();
  const btn = ([key, label, sec, action]: [Mark, string, string | null, string?]) => {
    const account = key === "account";
    return (
    <button
      key={key}
      className="navbtn"
      data-action={account ? "chrome.account" : action}
      aria-label={t(label)}
      aria-current={(sec ?? null) === at ? "page" : undefined}
      onClick={() => (key === "files" ? onFiles() : sec === null ? (at === null ? onRail() : onHome()) : onGo(key === "ext" ? "extensions" : key === "usage" ? "spend" : sec || undefined))}
    >
      {key === "files" ? <StudioIcon name="file" /> : key === "chat" ? <StudioIcon name="chat" /> : ICON[key === "advanced" ? "common" : key]}
      {key === "advanced" && <UpdateDot />}
      <span className="navhint" role="tooltip">{t(label)}</span>
    </button>
    );
  };
  return (
    <nav className="nav" aria-label={t("settings.main_navigation")} aria-hidden={shown ? undefined : true} inert={!shown}>
      <div className="navgrp">
        <button className="navbtn" data-action="session.new" aria-label={t("common.start_a_new_session")} onClick={onNew}>
          <StudioIcon name="plus" aria-hidden="true" />
          <span className="navhint" role="tooltip">{t("common.start_a_new_session")}</span>
        </button>
        {[HOME, ...destinations].map(btn)}
      </div>
      <div className="navgrp foot">
        {!FEEDBACK_RETIRED && (
          <button className="navbtn" data-action="feedback.open" onClick={onFeedback}>
            <StudioIcon name="feedback" aria-hidden="true" />
            <span className="navhint" role="tooltip">{t("common.send_feedback")}</span>
            <FeedbackBadge unread={feedbackUnread} />
          </button>
        )}
        {FOOT.map(btn)}
      </div>
    </nav>
  );
}
