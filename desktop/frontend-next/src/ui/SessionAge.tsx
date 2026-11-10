import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import { UnreadDot } from "./UnreadMark";

const DAY = 86_400_000;

export function ageWord(at: number, now: number): string {
  const gap = now - at;
  if (gap < 60_000) return t("common.just_now");
  if (gap < 3_600_000) return t("common.n_min", { n: Math.floor(gap / 60_000) });
  const day = (ms: number) => new Date(ms).setHours(0, 0, 0, 0);
  if (day(now) === day(at)) return t("common.n_h", { n: Math.floor(gap / 3_600_000) });
  if (day(now) - day(at) === DAY) return t("common.yesterday");
  const d = new Date(at);
  return `${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

export function SessionAge({ at, unread, dot = true }: { at?: number; unread?: boolean; dot?: boolean }) {
  const date = at ? new Date(at) : null;
  const time = date && Number.isFinite(date.valueOf())
    ? <time className="sessage ds-meta" dateTime={date.toISOString()} title={date.toLocaleString()}>{ageWord(at!, Date.now())}</time>
    : null;
  return <>{unread && dot && <UnreadDot />}{time}</>;
}

export function SessionTail({ run, at, unread, dot }: { run?: string; at?: number; unread?: boolean; dot?: boolean }) {
  if (run === "running") return <span className="sessspin ds-meta" data-run={run} role="img" aria-label={t("common.running")} title={t("common.running")}><StudioIcon name="loading" /></span>;
  if (run === "halt") return <span className="sessword ds-meta" data-run={run}>{t("session.needs_you")}</span>;
  return <SessionAge at={at} unread={unread} dot={dot} />;
}
