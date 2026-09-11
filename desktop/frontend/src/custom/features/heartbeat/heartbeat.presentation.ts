import type { HeartbeatTask } from "./heartbeat.types";
import type { HeartbeatTranslator } from "./heartbeat.i18n";
import { heartbeatNextRunAt as calendarHeartbeatNextRunAt, parseCalendarSchedule, nextCalendarRunImpl } from "./heartbeat.schedule";

const WEEKDAYS = [
  { key: "mon", labelKey: "heartbeat.weekdayMon" },
  { key: "tue", labelKey: "heartbeat.weekdayTue" },
  { key: "wed", labelKey: "heartbeat.weekdayWed" },
  { key: "thu", labelKey: "heartbeat.weekdayThu" },
  { key: "fri", labelKey: "heartbeat.weekdayFri" },
  { key: "sat", labelKey: "heartbeat.weekdaySat" },
  { key: "sun", labelKey: "heartbeat.weekdaySun" },
] as const;

// 日历调度计算（interval 窗口 / daily / weekly / biweekly / monthly / yearly /
// 月末 clamp / 闰日 / 双周锚点 / DST）统一委托给 heartbeat.schedule.ts，面板只
// 负责 cron 分支与展示格式化，避免两套日历逻辑漂移。
export function heartbeatNextRunAt(task: Pick<HeartbeatTask, "interval" | "lastRunAt" | "createdAt" | "timeWindowStart" | "timeWindowEnd">, now = Date.now()): number | null {
  if (isCronExpr(task.interval || "")) {
    return nextCronRunAt(task.interval || "", now);
  }
  return calendarHeartbeatNextRunAt(task, now);
}

// 周期任务（"24h|daily@22:00" 等）从 `from` 起的下一次触发时刻。calendar
// schedule 的 nextCalendarRun 语义就是 "after 之后的下一次"，与后端
// previousHeartbeatScheduleAt 对齐（月末 clamp、闰日、周一制双周锚点）。
export function nextCycleRunAt(interval: string, from = Date.now(), createdAt?: number): number | null {
  const schedule = parseCalendarSchedule(interval);
  if (!schedule) return null;
  const after = new Date(from);
  const anchor = createdAt ? new Date(createdAt) : after;
  return nextCalendarRunImpl(schedule, after, anchor).getTime();
}

export function formatInterval(interval: string, t: HeartbeatTranslator): string {
  const cycleMatch = interval.match(/^(\d+)[smh]\|(daily|weekly|biweekly|monthly|yearly)(?::([^@]*))?(?:@(\d{2}:\d{2}))?$/);
  if (cycleMatch) {
    const [, , type, days, time] = cycleMatch;
    // 格式参考：周一（时间：9:00）；每天直接「每天 22:00」，不套（时间：）包装
    const timeStr = time ? t("heartbeat.cycleTimeAt", { time }) : "";
    const dayNames = (d: string) => {
      const wd = WEEKDAYS.find((w) => w.key === d);
      return wd ? t(wd.labelKey) : d;
    };
    if (type === "daily") return time ? `${t("heartbeat.cycleDaily")} ${time}` : t("heartbeat.cycleDaily");
    if (type === "weekly") {
      const list = (days || "").split(",").filter(Boolean).map(dayNames).join(t("heartbeat.joinComma"));
      return `${list || t("heartbeat.cycleWeekly")}${timeStr}`;
    }
    if (type === "biweekly") {
      const list = (days || "").split(",").filter(Boolean).map(dayNames).join(t("heartbeat.joinComma"));
      return `${t("heartbeat.cycleBiweekly")}${list ? ` ${list}` : ""}${timeStr}`;
    }
    if (type === "monthly") return `${t("heartbeat.cycleMonthly")}${days ? ` ${days}${t("heartbeat.monthDay")}` : ""}${timeStr}`;
    if (type === "yearly") {
      const parts = (days || "").split("-");
      return `${t("heartbeat.cycleYearly")} ${parts[0] || "1"}/${parts[1] || "1"}${timeStr}`;
    }
  }
  const simple = interval.match(/^(\d+)([smh])$/);
  if (simple) {
    const unitLabels: Record<string, string> = {
      s: t("heartbeat.unitSec"),
      m: t("heartbeat.unitMin"),
      h: t("heartbeat.unitHour"),
    };
    return `${t("heartbeat.freqEvery")} ${simple[1]}${unitLabels[simple[2]] || simple[2]}`;
  }
  return interval;
}

// ── Cron expressions ─────────────────────────────────────────────────────────

// isCronExpr returns true when s looks like a 5-field cron expression
// (e.g. "0 * * * *", "*/15 * * * *", "0 9 * * 1-5").
// formatRelativeTime renders how long ago something happened: "just now",
// "N minutes ago", "N hours ago", "N days ago".
export function formatRelativeTime(at: number, now: number, t: HeartbeatTranslator): string {
  const diff = Math.max(0, now - at);
  const min = Math.floor(diff / 60000);
  if (min < 1) return t("heartbeat.justNow");
  if (min < 60) return t("heartbeat.minutesAgo", { n: min });
  const hr = Math.floor(min / 60);
  if (hr < 24) return t("heartbeat.hoursAgo", { n: hr });
  const d = Math.floor(hr / 24);
  return t("heartbeat.daysAgo", { n: d });
}

export function isCronExpr(s: string): boolean {
  const fields = s.trim().split(/\s+/);
  if (fields.length !== 5) return false;
  if (!fields.every((f) => f !== "" && /^[0-9*/\-,]+$/.test(f))) return false;
  // Reject out-of-range values (e.g. "99 * * * *"), zero/empty steps
  // ("*/0" never fires: value % 0 is NaN), and descending ranges ("5-1"
  // never matches). dom/month are 1-based (0 can never match getDate()/
  // getMonth()); dow is 0-7 with 7 accepted as the Sunday alias — mirror the
  // Go engine exactly.
  const limits = [59, 23, 31, 12, 7]; // min, hour, dom, month, dow
  const mins = [0, 0, 1, 1, 0];
  return fields.every((f, i) =>
    f.split(",").every((part) => {
      const slashIdx = part.indexOf("/");
      const base = slashIdx >= 0 ? part.slice(0, slashIdx) : part;
      if (slashIdx >= 0) {
        const step = Number(part.slice(slashIdx + 1));
        if (!Number.isInteger(step) || step < 1) return false;
      }
      if (base === "*") return true;
      if (base.includes("-")) {
        const [lo, hi] = base.split("-").map(Number);
        return Number.isInteger(lo) && Number.isInteger(hi)
          && lo >= mins[i] && hi <= limits[i] && lo <= hi;
      }
      const v = Number(base);
      return Number.isInteger(v) && v >= mins[i] && v <= limits[i];
    })
  );
}

function cronFieldMatch(pattern: string, value: number, minValue: number, maxValue: number): boolean {
  for (const part of pattern.split(",")) {
    const p = part.trim();
    let base = p;
    let step = 1;
    const slashIdx = p.indexOf("/");
    if (slashIdx >= 0) {
      base = p.slice(0, slashIdx);
      step = parseInt(p.slice(slashIdx + 1)) || 1;
    }
    if (step <= 0) continue;
    if (base === "*") {
      if (value >= minValue && value <= maxValue && (value - minValue) % step === 0) return true;
      continue;
    }
    const dashIdx = base.indexOf("-");
    let low = parseInt(base);
    let high = low;
    if (dashIdx >= 0) {
      low = parseInt(base.slice(0, dashIdx));
      high = parseInt(base.slice(dashIdx + 1));
    } else if (slashIdx >= 0) {
      high = maxValue;
    }
    if (isNaN(low) || isNaN(high)) continue;
    if (value < low || value > high) continue;
    if ((value - low) % step === 0) return true;
  }
  return false;
}

// nextCronRunAt returns the timestamp of the next time the 5-field cron
// expression matches, starting from `from` (default now). Returns null when
// the expression is invalid or nothing matches within the search horizon.
export function nextCronRunAt(expr: string, from = Date.now()): number | null {
  if (!isCronExpr(expr)) return null;
  const fields = expr.trim().split(/\s+/);
  if (fields.length !== 5) return null;
  const [minP, hourP, domP, monP, dowP] = fields;
  if (![minP, hourP, domP, monP, dowP].every((f) => /^[0-9*/\-,]+$/.test(f))) return null;
  const base = new Date(from);
  base.setSeconds(0, 0);
  for (let day = 0; day <= 366 * 8; day++) {
    const d = new Date(base);
    d.setDate(d.getDate() + day);
    if (!cronFieldMatch(monP, d.getMonth() + 1, 1, 12)) continue;
    // Standard cron: dom & dow are OR-ed when both are restricted.
    const domRestricted = domP !== "*";
    const dowRestricted = dowP !== "*";
    const domMatch = cronFieldMatch(domP, d.getDate(), 1, 31);
    // 7 is the standard Sunday alias in the dow field (getDay() is 0-6).
    const dowMatch = cronFieldMatch(dowP, d.getDay(), 0, 7) || (d.getDay() === 0 && cronFieldMatch(dowP, 7, 0, 7));
    const dayMatch = domRestricted && dowRestricted ? domMatch || dowMatch
      : domRestricted ? domMatch
      : dowRestricted ? dowMatch
      : true;
    if (!dayMatch) continue;
    const hStart = day === 0 ? d.getHours() : 0;
    for (let h = hStart; h < 24; h++) {
      if (!cronFieldMatch(hourP, h, 0, 23)) continue;
      const mStart = day === 0 && h === hStart ? d.getMinutes() + 1 : 0;
      for (let m = mStart; m < 60; m++) {
        if (!cronFieldMatch(minP, m, 0, 59)) continue;
        return new Date(d.getFullYear(), d.getMonth(), d.getDate(), h, m, 0, 0).getTime();
      }
    }
  }
  return null;
}

export function formatCronNext(ts: number | null): string {
  if (ts === null) return "";
  const d = new Date(ts);
  return `${(d.getMonth() + 1).toString().padStart(2, "0")}/${d.getDate().toString().padStart(2, "0")} ${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`;
}

// intervalToCron converts a cycle ("24h|daily@09:00") or simple ("30m", "1h")
// interval into a 5-field cron expression. Already-cron values pass through.
export function intervalToCron(interval: string, timeWindowStart?: string, timeWindowEnd?: string): string | null {
  // Guard: biweekly cannot be losslessly expressed in 5-field cron (DOM/DOW
  // are OR-ed, so a biweekly rule like "1-15 * 1" becomes "1st-15th OR Monday",
  // doubling the actual frequency). Seconds cannot be expressed either (cron
  // has no seconds field). Cross-midnight windows (22:00–06:00) produce a
  // descending range "22-6" that no matcher handles. Non-top-of-hour windows
  // (09:30–17:30) would be truncated to whole hours. Return null for these so
  // callers can refuse the conversion instead of silently corrupting semantics.
  const windowTopOfHour = !timeWindowStart && !timeWindowEnd
    || (!timeWindowStart || timeWindowStart.endsWith(":00"))
    && (!timeWindowEnd || timeWindowEnd.endsWith(":00"));
  if (!windowTopOfHour) return null;
  const cycleMatch = interval.match(/^\d+[smh]\|(daily|weekly|biweekly|monthly|yearly)(?::([^@]*))?(?:@(\d{2}:\d{2}))?$/);
  if (cycleMatch) {
    const kind = cycleMatch[1];
    if (kind === "biweekly") return null;
    const days = cycleMatch[2] || "";
    const time = cycleMatch[3] || "09:00";
    const [h, m] = time.split(":").map(Number);
    // Cycle tasks schedule on their own clock (@09:00 etc); the engine ignores
    // interval-style time windows for them (see heartbeatTaskDueAt), so any
    // stale window must not be folded into the cron hour field — that would
    // turn "daily@12:00" into "every hour 9-16".
    const dayMap: Record<string, number> = { mon: 1, tue: 2, wed: 3, thu: 4, fri: 5, sat: 6, sun: 0 };
    switch (kind) {
      case "daily": return `${m} ${h} * * *`;
      case "weekly": {
        const d = days.split(",").map((x) => dayMap[x.toLowerCase()] ?? "*").join(",");
        return `${m} ${h} * * ${d}`;
      }
      case "monthly": return `${m} ${h} ${days || "1"} * *`;
      case "yearly": {
        const [mo, dy] = days.split("-");
        return `${m} ${h} ${dy || "1"} ${mo || "1"} *`;
      }
    }
  }
  const simple = interval.match(/^(\d+)([smh])$/);
  if (simple) {
    const n = parseInt(simple[1]);
    const unit = simple[2];
    if (timeWindowStart && timeWindowEnd
      && parseInt(timeWindowStart.split(":")[0]) > parseInt(timeWindowEnd.split(":")[0])) {
      return null; // cross-midnight window: not expressible
    }
    const hExpr = timeWindowStart && timeWindowEnd
      // End is exclusive: "09:00–17:00" → hour range 9-16 (17:00 excluded).
      ? `${Math.max(0, parseInt(timeWindowStart.split(":")[0]))}-${Math.max(0, Math.min(23, parseInt(timeWindowEnd.split(":")[0]) - 1))}`
      : "*";
    if (unit === "m") return `*/${n} ${hExpr} * * *`;
    // 5-field cron: minute hour dom mon dow. Hourly tasks run at minute 0 of
    // every n-th hour (`0 */n * * *`). With a time window the hour field can
    // only carry "all hours in the range" (`0 9-16 * * *`), which is lossless
    // for 1h but would silently change 2h+ windows from "every N hours" to
    // "every hour" — refuse those.
    if (unit === "h") {
      if (timeWindowStart && timeWindowEnd && n > 1) return null;
      const hourField = timeWindowStart && timeWindowEnd ? hExpr : `*/${n}`;
      return `0 ${hourField} * * *`;
    }
    if (unit === "s") return null; // seconds cannot be expressed in cron
  }
  if (isCronExpr(interval)) return interval.trim();
  return null;
}

// cronToInterval reverse-converts a cron expression back to a simple interval.
// Returns null when the expression cannot be expressed as a plain "every N
// minutes/hours" interval without changing semantics (dom/dow/month-restricted
// or fixed-time schedules) — callers must keep the cron instead of silently
// rewriting e.g. a weekly "0 9 * * 1" into "1h".
export function cronToInterval(cron: string): string | null {
  const f = cron.trim().split(/\s+/);
  if (f.length !== 5) return null;
  // Only pure every-N minute/hour schedules round-trip losslessly.
  if (f[2] !== "*" || f[3] !== "*" || f[4] !== "*") return null;
  const min = f[0], hour = f[1];
  if (min.startsWith("*/") && hour === "*") return `${min.slice(2)}m`;
  if (min === "0" && hour.startsWith("*/")) return `${hour.slice(2)}h`;
  return null;
}

export type HeartbeatFrequencyType = "interval" | "daily" | "weekly" | "biweekly" | "monthly" | "yearly" | "cron";

export function changeHeartbeatFrequency(task: HeartbeatTask, frequency: HeartbeatFrequencyType): HeartbeatTask | null {
  const current = task.interval || "";
  if (frequency === "daily") return { ...task, interval: "24h|daily:mon,tue,wed,thu,fri,sat,sun@09:00" };
  if (frequency === "weekly") return { ...task, interval: "168h|weekly:mon@09:00" };
  if (frequency === "biweekly") return { ...task, interval: "336h|biweekly:mon@09:00" };
  if (frequency === "monthly") return { ...task, interval: "720h|monthly:1@09:00" };
  if (frequency === "yearly") return { ...task, interval: "8760h|yearly:1-1@09:00" };
  if (frequency === "cron") {
    if (isCronExpr(current)) return task;
    const converted = intervalToCron(current, task.timeWindowStart, task.timeWindowEnd);
    return converted === null ? null : { ...task, interval: converted };
  }
  if (isCronExpr(current)) {
    const converted = cronToInterval(current);
    return converted === null ? null : { ...task, interval: converted };
  }
  if (current.includes("|")) return { ...task, interval: current.replace(/\|.*$/, "") };
  return task;
}

// describeCron renders a human-readable description of a 5-field cron
// expression, localized via t().
export function describeCron(expr: string, t: HeartbeatTranslator): string {
  const f = expr.trim().split(/\s+/);
  if (f.length !== 5) return "";
  const min = f[0], hour = f[1], dom = f[2], mon = f[3], dow = f[4];

  const hourRange = (h: string): string => {
    if (!h || h === "*") return "";
    if (h.includes("/")) {
      const base = h.split("/")[0];
      if (base.includes("-")) {
        const parts = base.split("-");
        return `${parts[0].padStart(2, "0")}:00-${parts[1].padStart(2, "0")}:00`;
      }
      return "";
    }
    if (h.includes("-")) {
      const parts = h.split("-");
      return `${parts[0].padStart(2, "0")}:00-${parts[1].padStart(2, "0")}:00`;
    }
    return "";
  };
  const wd = hourRange(hour);

  if (min.startsWith("*/") && hour !== "*" && hour.includes("-")) {
    return `${t("heartbeat.cronEveryMin", { n: min.slice(2) })} (${wd})`;
  }
  if (min.startsWith("*/") && hour === "*") return t("heartbeat.cronEveryMin", { n: min.slice(2) });
  if (min.startsWith("*/") && hour !== "*") return `${t("heartbeat.cronEveryMin", { n: min.slice(2) })} ${wd}`;
  if (min === "0" && hour !== "*" && dom === "*" && mon === "*" && dow === "*") {
    if (hour.includes("/")) return t("heartbeat.cronEveryHour", { n: hour.replace("*/", "") });
    if (hour.includes("-")) return `${t("heartbeat.cronHourly")} (${wd})`;
    return t("heartbeat.cronAt", { time: `${hour.padStart(2, "0")}:00` });
  }
  if (min === "0" && hour === "*" && dom === "*" && mon === "*" && dow === "*") return t("heartbeat.cronHourly");
  if (min !== "*" && !min.includes("/") && hour === "*" && dom === "*" && mon === "*" && dow === "*") {
    return t("heartbeat.cronOnHour", { n: min });
  }
  if (dow !== "*" && dow !== "") {
    const weekdays: Record<string, string> = {
      "0": t("heartbeat.cronWeekdaySun"), "1": t("heartbeat.cronWeekdayMon"),
      "2": t("heartbeat.cronWeekdayTue"), "3": t("heartbeat.cronWeekdayWed"),
      "4": t("heartbeat.cronWeekdayThu"), "5": t("heartbeat.cronWeekdayFri"),
      "6": t("heartbeat.cronWeekdaySat"), "7": t("heartbeat.cronWeekdaySun"),
    };
    const days = dow.split(",").map((d) => weekdays[d] || d).join(t("heartbeat.joinComma"));
    const suffix = wd ? ` (${wd})` : "";
    return `${days} ${hour.padStart(2, "0")}:${min.padStart(2, "0")}${suffix}`;
  }
  const suffix = wd ? ` (${wd})` : "";
  return `${hour.padStart(2, "0")}:${min.padStart(2, "0")}${suffix}`;
}

// 周期 next-run 计算统一在文件头部通过 heartbeat.schedule.ts 的
// parseCalendarSchedule + nextCalendarRunImpl 实现（见 nextCycleRunAt），
// 与新实现的意图一致：镜像后端 previousHeartbeatScheduleAt 语义、月末
// clamp、周一制双周锚点。此处不再保留旧的行内实现。

export function taskNextRunAt(task: HeartbeatTask, now = Date.now()): number | null {
  if (!task.enabled) return null;
  const interval = task.interval || "";
  let next: number | null = null;
  // 周期任务（"24h|daily@22:00" / "168h|weekly:fri@16:00"）：按调度语义计算
  // 下一个匹配时刻。已运行过（有 lastRunAt）的任务基于 lastRunAt 求下一时刻
  // （heartbeatNextRunAt → heartbeat.schedule 的 nextCalendarRun，含月末
  // clamp/闰日/双周锚点/DST，与后端 previousHeartbeatScheduleAt 对齐）；离线
  // 期间早该运行的任务，next 会落在过去 → 显示 dueSoon（当前应执行），而不是
  // 跳到下一周期。从未运行的任务从创建时刻起算首次运行。
  const cycleMatch = interval.match(/^\d+[smh]\|(daily|weekly|biweekly|monthly|yearly)(?::([^@]*))?(?:@(\d{2}:\d{2}))?$/);
  if (cycleMatch) {
    next = task.lastRunAt
      ? heartbeatNextRunAt(task, now)
      : nextCycleRunAt(interval, task.createdAt || now, task.createdAt);
  } else {
    const cleaned = interval.replace(/\|.*$/, "");
    const m = cleaned.match(/^(\d+)([smh])$/);
    if (m) {
      // Plain interval with a time window: use the window-aware helper so the
      // displayed next run matches the backend (defers to the next opening
      // instead of naively showing lastRunAt + interval outside the window).
      if (task.timeWindowStart || task.timeWindowEnd) {
        next = heartbeatNextRunAt(task, now);
      } else {
        if (!task.lastRunAt) return null;
        const ms = parseInt(m[1]) * { s: 1000, m: 60000, h: 3600000 }[m[2] as "s" | "m" | "h"];
        next = task.lastRunAt + ms;
      }
    } else if (isCronExpr(cleaned)) {
      next = nextCronRunAt(cleaned, now);
    } else {
      return null;
    }
  }
  return next;
}

export interface HeartbeatTaskNextRun {
  task: HeartbeatTask;
  nextRunAt: number | null;
}

type TaskNextRunResolver = (task: HeartbeatTask, now: number) => number | null;

// Compute each task's next run once so an eight-year cron search never runs
// repeatedly inside Array.sort or again while rendering the same row.
export function prepareTasksByNextRun(
  tasks: HeartbeatTask[],
  now = Date.now(),
  resolveNextRun: TaskNextRunResolver = taskNextRunAt,
): HeartbeatTaskNextRun[] {
  return tasks
    .map((task) => ({ task, nextRunAt: resolveNextRun(task, now) }))
    .sort((a, b) => {
      if (a.task.enabled !== b.task.enabled) return a.task.enabled ? -1 : 1;
      const sortAt = (entry: HeartbeatTaskNextRun) => (
        entry.task.enabled && !entry.task.lastRunAt ? now : entry.nextRunAt
      ) ?? Number.POSITIVE_INFINITY;
      return sortAt(a) - sortAt(b);
    });
}

export function formatTaskNextRun(next: number | null, now: number, t: HeartbeatTranslator): string | null {
  if (next === null) return null;
  if (next <= now) return t("heartbeat.dueSoon");
  const diff = next - now;
  // 剩余时间：如「下次运行 26 分钟后」/「下次运行 2 小时后」/「下次运行 2天3小时后」/「即将触发」
  const days = Math.floor(diff / 86400000);
  const hours = Math.floor((diff % 86400000) / 3600000);
  const minutes = Math.floor((diff % 3600000) / 60000);
  const prefix = t("heartbeat.nextRun");
  if (days > 0) return `${prefix} ${days}${t("heartbeat.unitDay")}${hours}${t("heartbeat.unitHour")}${t("heartbeat.later")}`;
  if (hours > 0) return `${prefix} ${hours}${t("heartbeat.unitHour")}${minutes}${t("heartbeat.unitMin")}${t("heartbeat.later")}`;
  if (minutes > 0) return `${prefix} ${minutes}${t("heartbeat.unitMin")}${t("heartbeat.later")}`;
  return t("heartbeat.dueSoon");
}

export function taskNextRun(task: HeartbeatTask, t: HeartbeatTranslator): string | null {
  const now = Date.now();
  return formatTaskNextRun(taskNextRunAt(task, now), now, t);
}
