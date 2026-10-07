const PINNED_SESSIONS_KEY = "reasonix:pinned-sessions";

export function savedPins(): Set<string> {
  try {
    const raw = JSON.parse(localStorage.getItem(PINNED_SESSIONS_KEY) ?? "[]");
    return new Set(Array.isArray(raw) ? raw.filter((x): x is string => typeof x === "string") : []);
  } catch {
    return new Set();
  }
}

export function persistPins(pins: Set<string>): void {
  localStorage.setItem(PINNED_SESSIONS_KEY, JSON.stringify([...pins]));
}
