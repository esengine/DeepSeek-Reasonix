export const USER_TURNS_DROPPED = "user_turns_dropped";
export const KEPT_TURNS_FOLDED = "kept_turns_folded";

// The typed payload these notices carry as their detail, read as the variables
// their sentences are worded from. A detail that is not one reads as none, so the
// notice keeps the kernel's English.
export function turnsDroppedVars(detail: string | undefined): Record<string, string> | undefined {
  if (!detail) return undefined;
  let p: { count?: unknown };
  try {
    p = JSON.parse(detail) as { count?: unknown };
  } catch {
    return undefined;
  }
  return typeof p.count === "number" && p.count > 0 ? { n: String(p.count) } : undefined;
}
