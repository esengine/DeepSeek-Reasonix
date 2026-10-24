import { t } from "../i18n";
import { FeedbackBadge } from "./feedbackentry";
import { useDestinations, type Mark } from "./navdest";
import { ICON } from "./prefsnav";
import { StudioIcon } from "./StudioIcon";
import { UpdateDot } from "./UpdateDot";

// Settings entries share their destination's mark with prefsnav.
const CHAT = "M3.2 4.4a1.2 1.2 0 0 1 1.2-1.2h7.2a1.2 1.2 0 0 1 1.2 1.2v5a1.2 1.2 0 0 1-1.2 1.2H7.2L4.4 13v-2.4a1.2 1.2 0 0 1-1.2-1.2Z";

// Every entry goes somewhere that exists. A rail of icons that open nothing is
// the cheapest thing to draw and the most expensive thing to keep.
const HOME: [Mark, string, string | null] = ["chat", "会话", null];

const FOOT: [Mark, string, string, string][] = [
  ["account", "账号", "account", "chrome.account"],
  ["advanced", "设置", "", "chrome.settings"],
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
  const btn = ([key, label, sec, action]: [Mark, string, string | null, string?]) => (
    <button
      key={key}
      className="navbtn"
      data-action={action}
      aria-label={t(label)}
      aria-current={(sec ?? null) === at ? "page" : undefined}
      onClick={() => (key === "files" ? onFiles() : sec === null ? (at === null ? onRail() : onHome()) : onGo(sec || undefined))}
    >
      {key === "files" ? <StudioIcon name="file" aria-hidden="true" /> : <svg viewBox="0 0 16 16" aria-hidden="true">
        {key === "chat" ? <path d={CHAT} /> : ICON[key]}
      </svg>}
      {key === "advanced" && <UpdateDot />}
      <span className="navhint" role="tooltip">{t(label)}</span>
    </button>
  );
  return (
    <nav className="nav" aria-label={t("主导航")} aria-hidden={shown ? undefined : true} inert={!shown}>
      <div className="navgrp">
        <button className="navbtn" data-action="session.new" aria-label={t("新建会话")} onClick={onNew}>
          <StudioIcon name="plus" aria-hidden="true" />
          <span className="navhint" role="tooltip">{t("新建会话")}</span>
        </button>
        {[HOME, ...destinations].map(btn)}
      </div>
      <div className="navgrp foot">
        <button className="navbtn" data-action="feedback.open" onClick={onFeedback}>
          <StudioIcon name="feedback" aria-hidden="true" />
          <span className="navhint" role="tooltip">{t("发送反馈")}</span>
          <FeedbackBadge unread={feedbackUnread} />
        </button>
        {FOOT.map(btn)}
      </div>
    </nav>
  );
}
