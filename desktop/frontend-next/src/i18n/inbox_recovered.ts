export const INBOX_RECOVERED = "inbox_recovered";

// The typed payload an inbox-recovered notice carries as its detail, read as the
// variables its sentence is worded from. A detail that is not one reads as none,
// so the notice keeps the kernel's English.
export function inboxRecoveredVars(detail: string | undefined): { n: number } | undefined {
  if (!detail) return undefined;
  try {
    const p = JSON.parse(detail) as { count?: unknown };
    if (typeof p.count === "number" && Number.isInteger(p.count) && p.count > 0) return { n: p.count };
  } catch {
    return undefined;
  }
  return undefined;
}
