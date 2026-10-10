import { plural, t } from "../i18n";

export function UnreadDot({ kind }: { kind: "row" | "tab" }) {
  return <i className={kind === "row" ? "unread-dot" : "ptab-unread"} role="img" aria-label={t("未读")} />;
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
