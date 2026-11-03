import { plural, t } from "../i18n";

export function UnreadDot() {
  return <i className="unread-dot" role="img" aria-label={t("common.unread")} title={t("common.unread")} />;
}

export function UnreadCount({ n }: { n: number }) {
  if (n <= 0) return null;
  const label = plural(n, "unreadMark.1UnreadSession", "unreadMark.unreadSessions");
  return (
    <i className="unread-count" role="img" aria-label={label} title={label}>
      {n > 9 ? "9+" : n}
    </i>
  );
}
