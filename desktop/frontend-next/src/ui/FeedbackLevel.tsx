import { current, t } from "../i18n";
import type { FeedbackProfile } from "../port/feedback";
import { LevelBadge } from "./LevelBadge";

const LEVEL_NAME = ["新种", "萌芽", "幼苗", "小树", "繁花", "成荫", "共林"] as const;

const levelName = (level: number): string | null => (Number.isInteger(level) && level >= 0 && level < LEVEL_NAME.length ? t(LEVEL_NAME[level]!) : null);

const levelLabel = (level: number): string => {
  const name = levelName(level);
  return name ? t("L{n} · {name}", { n: level, name }) : t("L{n}", { n: level });
};

const day = (iso: string): string => new Date(iso).toLocaleDateString(current() === "zh" ? "zh-CN" : "en", { year: "numeric", month: "short", day: "numeric" });

const validDate = (iso: string | null | undefined): iso is string => typeof iso === "string" && !Number.isNaN(Date.parse(iso));

function progressText(p: FeedbackProfile): string {
  if (p.nextLevel === null || p.remaining === null) return t("已落地 {n} 条，已是最高等级", { n: p.adoptedCount });
  const next = levelName(p.nextLevel);
  return next
    ? t("已落地 {n} 条，再 {left} 条升到「{name}」", { n: p.adoptedCount, left: p.remaining, name: next })
    : t("已落地 {n} 条，再 {left} 条升到 L{level}", { n: p.adoptedCount, left: p.remaining, level: p.nextLevel });
}

function trustNote(p: FeedbackProfile): string | null {
  if (!validDate(p.trustExpiresAt)) return p.trustState === "lapsed" ? t("较高的额度已经失效。等级和已落地的条数都保留，之后再有反馈落地就会续上。") : null;
  switch (p.trustState) {
    case "legacy_active":
      return t("你现在较高的额度保留到 {date}。", { date: day(p.trustExpiresAt) });
    case "active":
      return t("较高的额度有效期至 {date}，下一条落地的反馈会续上。", { date: day(p.trustExpiresAt) });
    case "lapsed":
      return t("较高的额度已经失效。等级和已落地的条数都保留，之后再有反馈落地就会续上。");
    default:
      return null;
  }
}

interface Props {
  profile: FeedbackProfile;
  offline: boolean;
}

export function FeedbackLevel({ profile: p, offline }: Props) {
  const limits = p.effectiveLimits;
  const span = p.nextThreshold === null ? 0 : p.nextThreshold - p.currentThreshold;
  const done = Math.min(Math.max(p.adoptedCount - p.currentThreshold, 0), span);
  const next = p.nextLevel;
  const note = trustNote(p);
  const said = progressText(p);
  return (
    <section className="fbk-level" aria-label={t("反馈等级")} data-level={p.level} data-trust={p.trustState} data-stale={offline ? "" : undefined}>
      <div className="fbk-level-hd">
        <LevelBadge level={p.level} decorative size={32} />
        <div className="fbk-level-id">
          <span className="fbk-level-name">{levelLabel(p.level)}</span>
          <span className="fbk-level-progress">{said}</span>
        </div>
      </div>
      {next !== null && span > 0 && (
        <div className="fbk-level-track">
          <div
            className="fbk-level-bar"
            role="progressbar"
            aria-label={t("升级进度")}
            aria-valuemin={0}
            aria-valuenow={done}
            aria-valuemax={span}
            aria-valuetext={said}
          >
            <i className="fbk-level-fill" style={{ width: `${+((done / span) * 100).toFixed(2)}%` }} />
          </div>
          <span className="fbk-level-next">
            <LevelBadge level={next} locked decorative size={24} />
            <span>{levelName(next) ?? `L${next}`}</span>
          </span>
        </div>
      )}
      <p className="fbk-level-limits">
        {t("每小时最多 {h} 条反馈 · 每天 {d} 条 · 每小时 {r} 条回复", { h: limits.reportsPerHour, d: limits.reportsPerDay, r: limits.repliesPerHour })}
      </p>
      {note && <p className="fbk-level-note">{note}</p>}
      {offline && validDate(p.observedAt) && <p className="fbk-level-stale">{t("这是上次确认的等级（{date}），连上反馈服务后会更新。", { date: day(p.observedAt) })}</p>}
    </section>
  );
}
