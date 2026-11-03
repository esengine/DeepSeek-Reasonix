import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onThemeScheduleChange, setThemeSchedule, themeSchedule } from "../state/themeSchedule";

const FIELDS = [["start", "themeScheduleFields.darkModeStarts"], ["end", "themeScheduleFields.lightModeResumes"]] as const;

export function ThemeScheduleFields() {
  const schedule = useSyncExternalStore(onThemeScheduleChange, themeSchedule);
  return <>
    <p className="hint" id="theme-schedule-hint">{t("themeScheduleFields.switchesDailyInLocalTime")}</p>
    {FIELDS.map(([field, label]) => (
      <label className="prow" key={field}>
        <span className="tx">{t(label)}</span>
        <input type="time" className="fontown" value={schedule[field]} required step={60}
          aria-describedby="theme-schedule-hint" data-action="appearance.schedule" data-target={field}
          onChange={(e) => setThemeSchedule({ ...schedule, [field]: e.target.value })} />
      </label>
    ))}
    {schedule.start === schedule.end && <p className="hint">{t("themeScheduleFields.equalStartAndEndTimes")}</p>}
  </>;
}
