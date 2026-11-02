import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onThemeScheduleChange, setThemeSchedule, themeSchedule } from "../state/themeSchedule";

const FIELDS = [["start", "深色开始时间"], ["end", "恢复浅色时间"]] as const;

export function ThemeScheduleFields() {
  const schedule = useSyncExternalStore(onThemeScheduleChange, themeSchedule);
  return <>
    <p className="hint" id="theme-schedule-hint">{t("每天按本机时间切换，可跨午夜；开始时进入深色，结束时恢复浅色。")}</p>
    {FIELDS.map(([field, label]) => (
      <label className="prow" key={field}>
        <span className="tx">{t(label)}</span>
        <input type="time" className="fontown" value={schedule[field]} required step={60}
          aria-describedby="theme-schedule-hint" data-action="appearance.schedule" data-target={field}
          onChange={(e) => setThemeSchedule({ ...schedule, [field]: e.target.value })} />
      </label>
    ))}
    {schedule.start === schedule.end && <p className="hint">{t("开始和结束时间相同时全天使用浅色。")}</p>}
  </>;
}
