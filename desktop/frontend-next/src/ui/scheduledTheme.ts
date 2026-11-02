import type { ThemeSchedule } from "../state/themeSchedule";

function darkAt(start: number, end: number, minute: number): boolean {
  return start < end ? minute >= start && minute < end : start > end && (minute >= start || minute < end);
}

export function scheduledScheme({ start, end }: ThemeSchedule, now: Date): "light" | "dark" {
  const from = Number(start.slice(0, 2)) * 60 + Number(start.slice(3));
  const until = Number(end.slice(0, 2)) * 60 + Number(end.slice(3));
  return darkAt(from, until, now.getHours() * 60 + now.getMinutes()) ? "dark" : "light";
}

export function nextScheduledBoundary(range: ThemeSchedule, now: Date): number | null {
  if (range.start === range.end) return null;
  const from = Number(range.start.slice(0, 2)) * 60 + Number(range.start.slice(3));
  const until = Number(range.end.slice(0, 2)) * 60 + Number(range.end.slice(3));
  const current = darkAt(from, until, now.getHours() * 60 + now.getMinutes());
  const next = new Date(now);
  next.setSeconds(0, 0);
  // Walking actual minutes honours skipped and repeated local times at DST changes.
  // Only the resulting boundary gets a timer; there is no minute polling.
  for (let minute = 0; minute < 26 * 60; minute++) {
    next.setTime(next.getTime() + 60_000);
    if (darkAt(from, until, next.getHours() * 60 + next.getMinutes()) !== current) return next.getTime() - now.getTime();
  }
  return null;
}
