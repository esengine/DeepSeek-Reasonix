import type { EventKind } from "./types";

type EventPressureSnapshot = {
  second: number;
  events: number;
  textDeltas: number;
  reasoningDeltas: number;
  toolProgressRaw: number;
  toolProgressMerged: number;
};

declare global {
  interface Window {
    __reasonixEventPressure?: EventPressureSnapshot;
  }
}

function devEnabled(): boolean {
  const meta = import.meta as ImportMeta & { env?: { DEV?: boolean } };
  return Boolean(meta.env?.DEV);
}

function emptySnapshot(second: number): EventPressureSnapshot {
  return { second, events: 0, textDeltas: 0, reasoningDeltas: 0, toolProgressRaw: 0, toolProgressMerged: 0 };
}

let current: EventPressureSnapshot | null = null;

function bucket(): EventPressureSnapshot | null {
  if (!devEnabled() || typeof window === "undefined") return null;
  const second = Math.floor(Date.now() / 1000);
  if (!current || current.second !== second) {
    if (current && (current.events > 0 || current.toolProgressRaw > 0)) {
      window.__reasonixEventPressure = current;
      console.debug(
        "[reasonix:event-pressure]",
        `events/s=${current.events}`,
        `text=${current.textDeltas}`,
        `reasoning=${current.reasoningDeltas}`,
        `tool_progress=${current.toolProgressRaw}->${current.toolProgressMerged}`,
      );
    }
    current = emptySnapshot(second);
    window.__reasonixEventPressure = current;
  }
  return current;
}

export function recordControllerEvent(kind: EventKind): void {
  const stats = bucket();
  if (!stats) return;
  stats.events += 1;
  if (kind === "text") stats.textDeltas += 1;
  if (kind === "reasoning") stats.reasoningDeltas += 1;
  if (kind === "tool_progress") stats.toolProgressRaw += 1;
}

export function recordToolProgressMerge(rawCount: number, mergedCount: number): void {
  const stats = bucket();
  if (!stats) return;
  stats.toolProgressMerged += Math.max(0, mergedCount);
  if (rawCount > 0 && stats.toolProgressRaw < rawCount) {
    stats.toolProgressRaw += rawCount;
  }
}
