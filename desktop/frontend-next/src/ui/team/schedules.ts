import { t } from "../../i18n";
import type { TeamSchedule } from "../../port/team";

const WEEKDAYS = ["日", "一", "二", "三", "四", "五", "六"];

const UNITS: Record<NonNullable<TeamSchedule["unit"]>, string> = { minutes: "分钟", hours: "小时", days: "天" };

/** When a task runs, in the reader's words. */
export function describeSchedule(s: TeamSchedule): string {
  switch (s.repeat) {
    case "once":
      return s.once ? t("一次 · {at}", { at: new Date(s.once).toLocaleString() }) : t("一次");
    case "daily":
      return t("每天 {clock}", { clock: s.clock ?? "" });
    case "weekly":
      return t("每周{days} {clock}", { days: (s.days ?? []).map((d) => t(WEEKDAYS[d])).join("、"), clock: s.clock ?? "" });
    case "every":
      return t("每隔 {n} {unit}", { n: s.every ?? 0, unit: t(UNITS[s.unit ?? "hours"]) });
  }
}
