export interface ThemeSchedule { start: string; end: string }

const KEY = "rx-theme-schedule";
const DEFAULT: ThemeSchedule = { start: "20:00", end: "07:00" };
const listeners = new Set<() => void>();
let cached = DEFAULT;
let lastRaw: string | null | undefined;
let volatile: ThemeSchedule | null = null;

function valid(value: unknown): value is ThemeSchedule {
  if (!value || typeof value !== "object") return false;
  const { start, end } = value as ThemeSchedule;
  return typeof start === "string" && typeof end === "string" &&
    /^([01]\d|2[0-3]):[0-5]\d$/.test(start) && /^([01]\d|2[0-3]):[0-5]\d$/.test(end);
}

export function themeSchedule(): ThemeSchedule {
  if (volatile) return volatile;
  try {
    const raw = localStorage.getItem(KEY);
    if (raw !== lastRaw) {
      lastRaw = raw;
      cached = DEFAULT;
      try {
        const value: unknown = JSON.parse(raw ?? "null");
        if (valid(value)) cached = { start: value.start, end: value.end };
      } catch { /* unreadable preferences keep the default */ }
    }
    return cached;
  } catch {
    return cached;
  }
}

export function onThemeScheduleChange(fn: () => void): () => void {
  listeners.add(fn);
  return () => { listeners.delete(fn); };
}

export function setThemeSchedule(next: ThemeSchedule): void {
  if (!valid(next)) return;
  cached = { start: next.start, end: next.end };
  try {
    lastRaw = JSON.stringify(cached);
    localStorage.setItem(KEY, lastRaw);
    volatile = null;
  } catch {
    volatile = cached;
  }
  listeners.forEach((fn) => fn());
}
