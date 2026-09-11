import type { HeartbeatTask } from "./heartbeat.types";

export function mergeEngineRunState(prev: HeartbeatTask, fresh: HeartbeatTask): HeartbeatTask {
  return {
    ...prev,
    ...(fresh.runHistory ? { runHistory: fresh.runHistory } : {}),
    ...(fresh.topicId ? { topicId: fresh.topicId } : {}),
    ...(fresh.lastRunAt ? { lastRunAt: fresh.lastRunAt } : {}),
  };
}

export function CirclePlaySolid({ size = 15 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2.4}
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="10" />
      <path d="M10 8l6 4-6 4z" fill="currentColor" stroke="none" />
    </svg>
  );
}
