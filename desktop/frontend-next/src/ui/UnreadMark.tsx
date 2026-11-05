import { plural, t } from "../i18n";

export function UnreadDot() {
  return <i className="unread-dot" role="img" aria-label={t("common.unread")} title={t("common.unread")} />;
}

/** A row's lead dot. Unread is drawn by the dot alone, so the dot carries the word. */
export function LeadDot({ unread }: { unread?: boolean }) {
  const label = unread ? t("common.unread") : undefined;
  return <span className="ds-lead"><i className="pip ds-dot" role={label && "img"} aria-label={label} title={label} /></span>;
}

export function UnreadCount({ n }: { n: number }) {
  if (n <= 0) return null;
  const label = plural(n, "session.n_1_unread_session", "session.n_unread_sessions");
  return (
    <i className="unread-count" role="img" aria-label={label} title={label}>
      {n > 9 ? "9+" : n}
    </i>
  );
}
