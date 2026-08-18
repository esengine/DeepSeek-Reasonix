// Work-status helpers shared by the transcript process-fold header and the
// live turn region's status row.

import { useEffect, useState } from "react";
import type { useT } from "./i18n";

export function useTick(on: boolean): number {
  const [, setN] = useState(0);
  useEffect(() => {
    if (!on) return;
    const id = window.setInterval(() => setN((n) => n + 1), 1000);
    return () => window.clearInterval(id);
  }, [on]);
  return Date.now();
}

export function formatWorkDuration(durationMs: number, t: ReturnType<typeof useT>): string {
  if (!Number.isFinite(durationMs) || durationMs <= 0) return "";
  const totalSeconds = Math.max(1, Math.round(durationMs / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes <= 0) return t("transcript.durationSeconds", { s: totalSeconds });
  if (seconds <= 0) return t("transcript.durationMinutes", { m: minutes });
  return t("transcript.durationMinutesSeconds", { m: minutes, s: seconds });
}

export function workStatusLabel(durationMs: number, running: boolean, t: ReturnType<typeof useT>): string {
  const duration = formatWorkDuration(durationMs, t);
  if (running) {
    return duration ? t("transcript.workingDuration", { duration }) : t("transcript.working");
  }
  return duration ? t("transcript.workedDuration", { duration }) : t("transcript.worked");
}
