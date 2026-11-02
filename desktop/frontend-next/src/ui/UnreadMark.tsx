import { plural, t } from "../i18n";

export function UnreadDot() {
  return <i className="unread-dot" role="img" aria-label={t("未读")} title={t("未读")} />;
}

export function UnreadCount({ n }: { n: number }) {
  if (n <= 0) return null;
  const label = plural(n, "1 个会话未读", "{n} 个会话未读");
  return (
    <i className="unread-count" role="img" aria-label={label} title={label}>
      {n > 9 ? "9+" : n}
    </i>
  );
}
