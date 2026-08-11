export function formatGoalWorkTime(durationMs: number | undefined): string {
  if (!Number.isFinite(durationMs) || (durationMs ?? 0) <= 0) return "0s";
  const totalSeconds = Math.max(1, Math.round((durationMs ?? 0) / 1000));
  if (totalSeconds < 60) return `${totalSeconds}s`;
  const totalMinutes = Math.round(totalSeconds / 60);
  if (totalMinutes < 60) return `${totalMinutes}m`;
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`;
}
