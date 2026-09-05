import { useCallback } from "react";
import type { HeartbeatTask } from "./heartbeat.types";
import { useHeartbeatT } from "./heartbeat.i18n";

const WEEKDAYS = [
  { key: "mon", labelKey: "heartbeat.weekdayMon" },
  { key: "tue", labelKey: "heartbeat.weekdayTue" },
  { key: "wed", labelKey: "heartbeat.weekdayWed" },
  { key: "thu", labelKey: "heartbeat.weekdayThu" },
  { key: "fri", labelKey: "heartbeat.weekdayFri" },
  { key: "sat", labelKey: "heartbeat.weekdaySat" },
  { key: "sun", labelKey: "heartbeat.weekdaySun" },
] as const;

const ALL_WEEKDAYS = WEEKDAYS.map(w => w.key);
const DEFAULT_WEEKLY_DAY = "mon";

function defaultHeartbeatCycleDays(cycleType: string): string[] {
  if (cycleType === "daily") return [...ALL_WEEKDAYS];
  if (cycleType === "weekly" || cycleType === "biweekly") return [DEFAULT_WEEKLY_DAY];
  return [];
}

export function heartbeatBuildCycleInterval(cycleType: string, days: string[], time: string): string {
  const base: Record<string, string> = {
    daily: "24h",
    weekly: "168h",
    biweekly: "336h",
    monthly: "720h",
    yearly: "8760h",
  };
  const selectedDays = days.filter(Boolean);
  const isDailyWithSelection = cycleType === "daily" && selectedDays.length > 0 && selectedDays.length < 7;
  const isDailyWithoutSelection = cycleType === "daily" && selectedDays.length === 0;
  const effectiveType = isDailyWithoutSelection || isDailyWithSelection ? "weekly" : cycleType;
  const scheduleDays =
    (effectiveType === "weekly" || effectiveType === "biweekly") && selectedDays.length === 0
      ? defaultHeartbeatCycleDays(effectiveType)
      : selectedDays;

  let suffix = `|${effectiveType}`;
  if (effectiveType === "weekly" || effectiveType === "biweekly") {
    suffix += `:${scheduleDays.join(",")}`;
  } else if (effectiveType === "monthly") {
    suffix += `:${scheduleDays[0] || "1"}`;
  } else if (effectiveType === "yearly") {
    suffix += `:${scheduleDays[0] || "1"}-${scheduleDays[1] || "1"}`;
  }
  suffix += `@${time}`;
  return (base[cycleType] || "24h") + suffix;
}

export function CycleEditor({
  draft,
  setDraft,
  cycleType,
}: {
  draft: HeartbeatTask;
  setDraft: (field: keyof HeartbeatTask, value: string | boolean) => void;
  cycleType: "daily" | "weekly" | "biweekly" | "monthly" | "yearly";
}) {
  const t = useHeartbeatT();
  const cycleMatch = (draft.interval || "").match(/^(\d+)[smh]\|(daily|weekly|biweekly|monthly|yearly)(?::([^@]*))?(?:@(.*))?$/);
  const cycleDays = cycleMatch?.[3] || "";
  // The interval is the controlled draft, including an unfinished time value.
  const selectedDays = cycleDays ? cycleDays.split(",") : defaultHeartbeatCycleDays(cycleType);
  const monthDay = cycleDays || "1";
  const yearMonth = cycleDays.split("-")[0] || "1";
  const yearDay = cycleDays.split("-")[1] || "1";
  const timeVal = cycleMatch?.[4] ?? "09:00";

  // Build interval string when config changes
  const buildInterval = useCallback((ct: string, days: string[], tm: string) => {
    const base: Record<string, string> = {
      daily: "24h",
      weekly: "168h",
      biweekly: "336h",
      monthly: "720h",
      yearly: "8760h",
    };
    let suffix = `|${ct}`;
    if (ct === "daily" || ct === "weekly" || ct === "biweekly") {
      suffix += `:${days.join(",")}`;
    } else if (ct === "monthly") {
      suffix += `:${days[0] || "1"}`;
    } else if (ct === "yearly") {
      // days[0] = month, days[1] = day — each is a plain number, no dash
      suffix += `:${days[0] || "1"}-${days[1] || "1"}`;
    }
    suffix += `@${tm}`;
    return (base[ct] || "24h") + suffix;
  }, []);

  const onDayToggle = (day: string) => {
    const isWeeklyLike = cycleType === "weekly" || cycleType === "biweekly";
    if (selectedDays.includes(day) && selectedDays.length === 1 && isWeeklyLike) return;
    const next = selectedDays.includes(day) ? selectedDays.filter((value) => value !== day) : [...selectedDays, day];
    setDraft("interval", buildInterval(cycleType, next, timeVal));
  };

  const onMonthDayChange = useCallback((d: string) => {
    setDraft("interval", buildInterval(cycleType, [d], timeVal));
  }, [buildInterval, cycleType, setDraft, timeVal]);

  const onYearMonthChange = useCallback((m: string) => {
    setDraft("interval", buildInterval(cycleType, [m, yearDay], timeVal));
  }, [buildInterval, cycleType, setDraft, timeVal, yearDay]);

  const onYearDayChange = useCallback((d: string) => {
    setDraft("interval", buildInterval(cycleType, [yearMonth, d], timeVal));
  }, [buildInterval, cycleType, setDraft, timeVal, yearMonth]);

  const onTimeChange = useCallback((tm: string) => {
    const days = cycleType === "daily" || cycleType === "weekly" || cycleType === "biweekly" ? selectedDays
      : cycleType === "monthly" ? [monthDay]
      : cycleType === "yearly" ? [yearMonth, yearDay]
      : [];
    setDraft("interval", buildInterval(cycleType, days, tm));
  }, [buildInterval, cycleType, selectedDays, monthDay, yearMonth, yearDay, setDraft]);

  const MONTHS = Array.from({ length: 12 }, (_, i) => ({
    value: String(i + 1),
    label: t("heartbeat.monthOption", { n: i + 1 }),
  }));
  const DAYS = Array.from({ length: 31 }, (_, i) => ({
    value: String(i + 1),
    label: t("heartbeat.dayOption", { n: i + 1 }),
  }));

  return (
    <div className="heartbeat-editor__cycle-wrap">
      <div className="heartbeat-editor__cycle-row">
        {cycleType === "monthly" && (
          <select
            className="heartbeat-editor__freq-select"
            value={monthDay}
            onChange={(e) => onMonthDayChange(e.target.value)}
          >
            {DAYS.map((d) => (
              <option key={d.value} value={d.value}>{d.label}</option>
            ))}
          </select>
        )}

        {cycleType === "yearly" && (
          <>
            <select
              className="heartbeat-editor__freq-select"
              value={yearMonth}
              onChange={(e) => onYearMonthChange(e.target.value)}
            >
              {MONTHS.map((m) => (
                <option key={m.value} value={m.value}>{m.label}</option>
              ))}
            </select>
            <select
              className="heartbeat-editor__freq-select"
              value={yearDay}
              onChange={(e) => onYearDayChange(e.target.value)}
            >
              {DAYS.map((d) => (
                <option key={d.value} value={d.value}>{d.label}</option>
              ))}
            </select>
          </>
        )}

        <input
          className="heartbeat-editor__freq-input heartbeat-editor__freq-input--time"
          type="time"
          value={timeVal}
          onChange={(e) => onTimeChange(e.target.value)}
        />

        {(cycleType === "weekly" || cycleType === "biweekly") && (
          <div className="set-seg">
            {WEEKDAYS.map((wd) => (
              <button
                key={wd.key}
                type="button"
                className={`set-seg__btn${selectedDays.includes(wd.key) ? " set-seg__btn--on" : ""}`}
                onClick={() => onDayToggle(wd.key)}
                aria-pressed={selectedDays.includes(wd.key)}
              >
                {t(wd.labelKey)}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
