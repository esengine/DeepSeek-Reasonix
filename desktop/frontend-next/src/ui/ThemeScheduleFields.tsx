import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onThemeScheduleChange, setThemeSchedule, themeSchedule } from "../state/themeSchedule";

const FIELDS = [["start", "look.dark_mode_starts"], ["end", "look.light_mode_resumes"]] as const;

export function ThemeScheduleFields() {
  const schedule = useSyncExternalStore(onThemeScheduleChange, themeSchedule);
  return <>
    <p className="hint" id="theme-schedule-hint">{t("look.switches_daily_in_local_time")}</p>
    {FIELDS.map(([field, label]) => (
      <label className="prow" key={field}>
        <span className="tx">{t(label)}</span>
        <input type="time" className="fontown" value={schedule[field]} required step={60}
          aria-describedby="theme-schedule-hint" data-action="appearance.schedule" data-target={field}
          onChange={(e) => setThemeSchedule({ ...schedule, [field]: e.target.value })} />
      </label>
    ))}
    {schedule.start === schedule.end && <p className="hint">{t("look.equal_start_and_end_times")}</p>}
  </>;
}
