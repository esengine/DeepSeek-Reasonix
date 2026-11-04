import { t } from "../i18n";
import { UnreadDot } from "./UnreadMark";

const DAY = 86_400_000;

export function ageWord(at: number, now: number): string {
  const gap = now - at;
  if (gap < 60_000) return t("刚刚");
  if (gap < 3_600_000) return t("{n} 分钟", { n: Math.floor(gap / 60_000) });
  const day = (ms: number) => new Date(ms).setHours(0, 0, 0, 0);
  if (day(now) === day(at)) return t("{n} 小时", { n: Math.floor(gap / 3_600_000) });
  if (day(now) - day(at) === DAY) return t("昨天");
  const d = new Date(at);
  return `${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

export function SessionAge({ at, unread }: { at?: number; unread?: boolean }) {
  const date = at ? new Date(at) : null;
  const time = date && Number.isFinite(date.valueOf())
    ? <time className="sessmeta sessage" dateTime={date.toISOString()} title={date.toLocaleString()}>{ageWord(at!, Date.now())}</time>
    : null;
  return <>{unread && <UnreadDot />}{time}</>;
}

export function SessionTail({ run, at, unread }: { run?: string; at?: number; unread?: boolean }) {
  if (run === "running" || run === "halt") return <span className="sessword" data-run={run}>{t(run === "running" ? "运行中" : "等你确认")}</span>;
  return <SessionAge at={at} unread={unread} />;
}
