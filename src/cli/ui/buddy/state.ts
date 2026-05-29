export type BuddyMood = "idle" | "thinking" | "working" | "warning" | "pet";

export type BuddyPulseKind = "pet" | "wake";

export interface BuddyPulse {
  kind: BuddyPulseKind;
  at: number;
}

export interface BuddySignals {
  pulse?: BuddyPulse | null;
  awaitingUser?: boolean;
  toolActive?: boolean;
  loopActive?: boolean;
  busy?: boolean;
  streaming?: boolean;
}

export const BUDDY_PULSE_TTL_MS = 2400;

export function createBuddyPulse(kind: BuddyPulseKind, now: number = Date.now()): BuddyPulse {
  return { kind, at: now };
}

export function isBuddyPulseExpired(
  pulse: BuddyPulse | null | undefined,
  now: number = Date.now(),
  ttlMs: number = BUDDY_PULSE_TTL_MS,
): boolean {
  return !pulse || now - pulse.at >= ttlMs;
}

export function applyBuddyPulseToMood(mood: BuddyMood, pulse?: BuddyPulse | null): BuddyMood {
  if (pulse?.kind === "pet") return "pet";
  if (pulse?.kind === "wake" && mood === "idle") return "thinking";
  return mood;
}

export function resolveBuddyMood(signals: BuddySignals): BuddyMood {
  const baseMood: BuddyMood = signals.awaitingUser
    ? "warning"
    : signals.toolActive || signals.loopActive
      ? "working"
      : signals.busy || signals.streaming
        ? "thinking"
        : "idle";

  return applyBuddyPulseToMood(baseMood, signals.pulse);
}

export function buddyPhrase(mood: BuddyMood): string {
  if (mood === "pet") return "bloop";
  if (mood === "thinking") return "diving through context";
  if (mood === "working") return "following tool wake";
  if (mood === "warning") return "waiting for your choice";
  return "ready at the surface";
}
