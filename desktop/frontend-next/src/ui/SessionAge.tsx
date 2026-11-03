import { t } from "../i18n";
import { since } from "./wallet";
import { UnreadDot } from "./UnreadMark";

export function SessionAge({ at, unread }: { at?: number; unread?: boolean }) {
  if (unread) return <UnreadDot />;
  if (!at) return null;
  const date = new Date(at);
  if (!Number.isFinite(date.valueOf())) return null;
  const iso = date.toISOString();
  return <time className="sessmeta sessage" dateTime={iso} title={date.toLocaleString()}>{since(iso, Date.now())}</time>;
}

export function SessionTail({ run, at, unread }: { run?: string; at?: number; unread?: boolean }) {
  if (run === "running" || run === "halt") return <span className="sessword" data-run={run}>{t(run === "running" ? "运行中" : "等你确认")}</span>;
  return <SessionAge at={at} unread={unread} />;
}
