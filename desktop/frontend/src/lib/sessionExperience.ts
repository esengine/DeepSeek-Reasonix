import { useMemo, useSyncExternalStore } from "react";

export type SessionExperience = "standard" | "deep";

export type WorkProcessPresentation = {
  experience: SessionExperience;
  showWhileRunning: true;
  keepExpandedAfterCompletion: boolean;
};

const SESSION_EXPERIENCE_KEY = "reasonix-session-experience";
const SESSION_EXPERIENCE_EVENT = "reasonix:session-experience";

let current: SessionExperience = "standard";
let hydrated = false;
const listeners = new Set<() => void>();
const beforeChangeListeners = new Set<(previous: SessionExperience, next: SessionExperience) => void>();

function normalize(value: unknown): SessionExperience {
  return value === "deep" ? "deep" : "standard";
}

function emit(): void {
  for (const listener of listeners) listener();
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent(SESSION_EXPERIENCE_EVENT, { detail: current }));
  }
}

function emitCompatibilitySignals(next: SessionExperience): void {
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent("reasonix:process-fold", {
      detail: next === "deep" ? "expanded" : "auto",
    }));
    window.dispatchEvent(new CustomEvent("reasonix:reasoning-display-mode", {
      detail: next === "deep" ? "expanded" : "auto",
    }));
  }
}

function writeCompatibilityMirrors(next: SessionExperience): void {
  if (typeof localStorage === "undefined") return;
  localStorage.setItem(SESSION_EXPERIENCE_KEY, next);
  localStorage.setItem("reasonix-display-mode", "standard");
  localStorage.setItem("reasonix-process-fold", next === "deep" ? "expanded" : "auto");
  // The oldest boolean summary key cannot represent Deep. Let the mirrored
  // backend reasoning field win instead of reviving a contradictory value.
  localStorage.removeItem("reasonix-reasoning-summary");
}

export function getSessionExperience(): SessionExperience {
  // The backend snapshot is authoritative. Before it arrives, use the safe
  // default instead of reviving a stale value written by an older frontend.
  if (!hydrated) return "standard";
  return current;
}

export function hydrateSessionExperience(value: unknown): void {
  const next = normalize(value);
  hydrated = true;
  current = next;
  writeCompatibilityMirrors(next);
  emit();
  emitCompatibilitySignals(next);
}

export function applySessionExperience(value: SessionExperience): void {
  const next = normalize(value);
  hydrated = true;
  writeCompatibilityMirrors(next);
  if (next === current) {
    emitCompatibilitySignals(next);
    return;
  }
  for (const listener of beforeChangeListeners) listener(current, next);
  current = next;
  emit();
  emitCompatibilitySignals(next);
}

/** Synchronously capture layout state before a mode change updates row
 * geometry. Layout owners use this to preserve the reader's logical anchor. */
export function onSessionExperienceWillChange(
  listener: (previous: SessionExperience, next: SessionExperience) => void,
): () => void {
  beforeChangeListeners.add(listener);
  return () => beforeChangeListeners.delete(listener);
}

export function resolveWorkProcessPresentation(value: SessionExperience): WorkProcessPresentation {
  return {
    experience: value,
    showWhileRunning: true,
    keepExpandedAfterCompletion: value === "deep",
  };
}

export function useSessionExperience(): SessionExperience {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    getSessionExperience,
    () => "standard",
  );
}

export function useWorkProcessPresentation(): WorkProcessPresentation {
  const experience = useSessionExperience();
  return useMemo(() => resolveWorkProcessPresentation(experience), [experience]);
}
