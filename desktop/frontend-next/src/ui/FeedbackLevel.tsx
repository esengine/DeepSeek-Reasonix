import { useId } from "react";
import { current, t } from "../i18n";
import type { FeedbackProfile } from "../port/feedback";
import { LevelBadge } from "./LevelBadge";

const LEVEL_NAME = ["feedback.new_seed", "feedback.sprout", "feedback.seedling", "feedback.sapling", "feedback.bloom", "feedback.canopy", "feedback.grove"] as const;

const levelName = (level: number): string | null => (Number.isInteger(level) && level >= 0 && level < LEVEL_NAME.length ? t(LEVEL_NAME[level]!) : null);

const levelLabel = (level: number): string => {
  const name = levelName(level);
  return name ? t("L{n} · {name}", { n: level, name }) : t("L{n}", { n: level });
};

const day = (iso: string): string => new Date(iso).toLocaleDateString(current() === "zh" ? "zh-CN" : "en", { year: "numeric", month: "short", day: "numeric" });

const validDate = (iso: string | null | undefined): iso is string => typeof iso === "string" && !Number.isNaN(Date.parse(iso));

function progressText(p: FeedbackProfile): string {
  if (p.nextLevel === null || p.remaining === null) return t("feedback.n_shipped_the_highest_level", { n: p.adoptedCount });
  const next = levelName(p.nextLevel);
  return next
    ? t("feedback.n_shipped_left_more_to_2", { n: p.adoptedCount, left: p.remaining, name: next })
    : t("feedback.n_shipped_left_more_to", { n: p.adoptedCount, left: p.remaining, level: p.nextLevel });
}

function trustNote(p: FeedbackProfile): string | null {
  if (!validDate(p.trustExpiresAt)) return p.trustState === "lapsed" ? t("feedback.your_higher_limits_have_lapsed") : null;
  switch (p.trustState) {
    case "legacy_active":
      return t("feedback.your_current_higher_limits_stay", { date: day(p.trustExpiresAt) });
    case "active":
      return t("feedback.your_higher_limits_run_until", { date: day(p.trustExpiresAt) });
    case "lapsed":
      return t("feedback.your_higher_limits_have_lapsed");
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
  const meaning = useId();
  return (
    <section className="fbk-level" aria-label={t("feedback.feedback_level")} data-level={p.level} data-trust={p.trustState} data-stale={offline ? "" : undefined}>
      <div className="fbk-level-hd">
        <LevelBadge level={p.level} decorative size={32} />
        <div className="fbk-level-id">
          <span className="fbk-level-name">{levelLabel(p.level)}</span>
          <span className="fbk-level-progress" aria-describedby={meaning}>{said}</span>
        </div>
      </div>
      {next !== null && span > 0 && (
        <div className="fbk-level-track">
          <div
            className="fbk-level-bar"
            role="progressbar"
            aria-label={t("feedback.progress_to_the_next_level")}
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
      <p className="fbk-level-meaning" id={meaning}>{t("feedback.shipped_means_your_report_was")}</p>
      <p className="fbk-level-limits">
        {t("feedback.up_to_h_reports_an", { h: limits.reportsPerHour, d: limits.reportsPerDay, r: limits.repliesPerHour })}
      </p>
      {note && <p className="fbk-level-note">{note}</p>}
      {offline && validDate(p.observedAt) && <p className="fbk-level-stale">{t("feedback.this_is_the_last_confirmed", { date: day(p.observedAt) })}</p>}
    </section>
  );
}
