import type { HeartbeatTask } from "./heartbeat.types";

const INTERVAL_MS: Record<"s" | "m" | "h", number> = {
  s: 1000,
  m: 60_000,
  h: 3_600_000,
};

const WEEKDAYS: Record<string, number> = {
  sun: 0,
  mon: 1,
  tue: 2,
  wed: 3,
  thu: 4,
  fri: 5,
  sat: 6,
};

interface CalendarSchedule {
  kind: "daily" | "weekly" | "biweekly" | "monthly" | "yearly";
  days: number[];
  month: number;
  day: number;
  hour: number;
  minute: number;
}

function heartbeatIntervalMs(interval?: string): number | null {
  const clean = (interval || "").replace(/\|.*$/, "");
  const match = clean.match(/^(\d+)([smh])$/);
  if (!match) return null;
  return parseInt(match[1], 10) * INTERVAL_MS[match[2] as "s" | "m" | "h"];
}

function heartbeatClockMinutes(value?: string): number | null {
  const match = (value || "").match(/^(\d{2}):(\d{2})$/);
  if (!match) return null;
  const hour = parseInt(match[1], 10);
  const minute = parseInt(match[2], 10);
  if (hour < 0 || hour > 23 || minute < 0 || minute > 59) return null;
  return hour * 60 + minute;
}

function dateAtMinutes(base: Date, minutes: number): Date {
  const date = new Date(base);
  date.setHours(Math.floor(minutes / 60), minutes % 60, 0, 0);
  return date;
}

function heartbeatWithinWindow(date: Date, start: number | null, end: number | null): boolean {
  if (start === null && end === null) return true;
  const minutes = date.getHours() * 60 + date.getMinutes();
  if (start !== null && end === null) return minutes >= start;
  if (start === null && end !== null) return minutes < end;
  if (start === end) return true;
  if (start! < end!) return minutes >= start! && minutes < end!;
  return minutes >= start! || minutes < end!;
}

function nextHeartbeatWindowTime(from: Date, start: number | null, end: number | null): Date {
  if (heartbeatWithinWindow(from, start, end)) return from;
  if (start !== null && end === null) return dateAtMinutes(from, start);
  if (start === null && end !== null) {
    const next = new Date(from);
    next.setDate(next.getDate() + 1);
    next.setHours(0, 0, 0, 0);
    return next;
  }
  const minutes = from.getHours() * 60 + from.getMinutes();
  if (start! < end! && minutes < start!) return dateAtMinutes(from, start!);
  if (start! > end! && minutes < start! && minutes >= end!) return dateAtMinutes(from, start!);
  const next = dateAtMinutes(from, start!);
  next.setDate(next.getDate() + 1);
  return next;
}

function positiveInt(value: string, fallback: number): number {
  return /^\d+$/.test(value.trim()) ? parseInt(value, 10) : fallback;
}

function parseCalendarScheduleImpl(interval?: string): CalendarSchedule | null {
  const separator = (interval || "").indexOf("|");
  if (separator < 0) return null;
  const suffix = interval!.slice(separator + 1).trim();
  const clockSeparator = suffix.indexOf("@");
  const rulePart = clockSeparator < 0 ? suffix : suffix.slice(0, clockSeparator);
  const clock = clockSeparator < 0 ? "09:00" : suffix.slice(clockSeparator + 1);
  const clockMinutes = heartbeatClockMinutes(clock);
  if (clockMinutes === null) return null;
  const ruleSeparator = rulePart.indexOf(":");
  const kind = ruleSeparator < 0 ? rulePart : rulePart.slice(0, ruleSeparator);
  const rule = ruleSeparator < 0 ? "" : rulePart.slice(ruleSeparator + 1);
  if (!(["daily", "weekly", "biweekly", "monthly", "yearly"] as const).includes(kind as CalendarSchedule["kind"])) {
    return null;
  }
  const schedule: CalendarSchedule = {
    kind: kind as CalendarSchedule["kind"],
    days: [],
    month: 1,
    day: 1,
    hour: Math.floor(clockMinutes / 60),
    minute: clockMinutes % 60,
  };
  if (schedule.kind === "weekly" || schedule.kind === "biweekly") {
    schedule.days = rule.split(",").map((day) => WEEKDAYS[day.trim().toLowerCase()]).filter((day) => day !== undefined);
    if (schedule.days.length === 0) return null;
  } else if (schedule.kind === "monthly") {
    schedule.day = positiveInt(rule, 1);
  } else if (schedule.kind === "yearly") {
    const [month, day] = rule.split("-", 2);
    schedule.month = Math.min(12, Math.max(1, positiveInt(month, 1)));
    schedule.day = positiveInt(day || "", 1);
  }
  return schedule;
}

function calendarDate(year: number, month: number, day: number, schedule: CalendarSchedule): Date {
  const maxDay = new Date(year, month + 1, 0).getDate();
  return new Date(year, month, Math.min(Math.max(day, 1), maxDay), schedule.hour, schedule.minute, 0, 0);
}

function weekStart(date: Date): Date {
  const start = new Date(date.getFullYear(), date.getMonth(), date.getDate());
  start.setDate(start.getDate() - ((start.getDay() + 6) % 7));
  return start;
}

function civilDayNumber(date: Date): number {
  return Math.floor(Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()) / 86_400_000);
}

// Exported for the panel's cycle interval math (nextCycleRunAt) so both frontend
// paths share one calendar implementation and cannot drift from each other.
export function parseCalendarSchedule(interval?: string): CalendarSchedule | null {
  return parseCalendarScheduleImpl(interval);
}

export function calendarDateFor(year: number, month: number, day: number, schedule: CalendarSchedule): Date {
  return calendarDate(year, month, day, schedule);
}

export function nextCalendarRunImpl(schedule: CalendarSchedule, after: Date, anchor: Date): Date {
  return nextCalendarRun(schedule, after, anchor);
}

function nextCalendarRun(schedule: CalendarSchedule, after: Date, anchor: Date): Date {
  if (schedule.kind === "daily") {
    const candidate = calendarDate(after.getFullYear(), after.getMonth(), after.getDate(), schedule);
    if (candidate.getTime() <= after.getTime()) candidate.setDate(candidate.getDate() + 1);
    return candidate;
  }
  if (schedule.kind === "weekly" || schedule.kind === "biweekly") {
    const searchDays = schedule.kind === "biweekly" ? 21 : 7;
    for (let offset = 0; offset <= searchDays; offset += 1) {
      const day = new Date(after.getFullYear(), after.getMonth(), after.getDate());
      day.setDate(day.getDate() + offset);
      if (!schedule.days.includes(day.getDay())) continue;
      const candidate = calendarDate(day.getFullYear(), day.getMonth(), day.getDate(), schedule);
      if (candidate.getTime() <= after.getTime()) continue;
      if (schedule.kind === "biweekly") {
        const weeks = Math.floor(Math.abs(civilDayNumber(weekStart(candidate)) - civilDayNumber(weekStart(anchor))) / 7);
        if (weeks % 2 !== 0) continue;
      }
      return candidate;
    }
  }
  if (schedule.kind === "monthly") {
    let candidate = calendarDate(after.getFullYear(), after.getMonth(), schedule.day, schedule);
    if (candidate.getTime() <= after.getTime()) {
      const nextMonth = new Date(after.getFullYear(), after.getMonth() + 1, 1);
      candidate = calendarDate(nextMonth.getFullYear(), nextMonth.getMonth(), schedule.day, schedule);
    }
    return candidate;
  }
  if (schedule.kind === "yearly") {
    let candidate = calendarDate(after.getFullYear(), schedule.month - 1, schedule.day, schedule);
    if (candidate.getTime() <= after.getTime()) candidate = calendarDate(after.getFullYear() + 1, schedule.month - 1, schedule.day, schedule);
    return candidate;
  }
  return after;
}

export function heartbeatNextRunAt(
  task: Pick<HeartbeatTask, "interval" | "lastRunAt" | "createdAt" | "timeWindowStart" | "timeWindowEnd">,
  now = Date.now(),
): number | null {
  if (!task.lastRunAt) return null;
  const intervalMs = heartbeatIntervalMs(task.interval);
  if (intervalMs === null) return null;
  const schedule = parseCalendarSchedule(task.interval);
  if (schedule) {
    const after = new Date(task.lastRunAt);
    const anchor = new Date(task.createdAt || task.lastRunAt);
    return nextCalendarRun(schedule, after, anchor).getTime();
  }
  const rawNext = task.lastRunAt + intervalMs;
  const start = heartbeatClockMinutes(task.timeWindowStart);
  const end = heartbeatClockMinutes(task.timeWindowEnd);
  if (start === null && end === null) return rawNext;
  const candidate = new Date(Math.max(rawNext, now));
  return nextHeartbeatWindowTime(candidate, start, end).getTime();
}
