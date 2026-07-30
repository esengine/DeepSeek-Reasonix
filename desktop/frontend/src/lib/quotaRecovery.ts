export interface QuotaRecoveryIntent {
  tabId: string;
  sessionPath: string;
}

type RefBox<T> = { current: T };

// Use the freshest non-empty session identity. Tab metadata can temporarily
// carry an empty path while controller metadata already knows the session.
export function quotaRecoverySessionPath(tabPath?: string, controllerPath?: string): string {
  return (tabPath?.trim() || controllerPath?.trim() || "");
}

// Consume only the exact attempt that started the successful model switch.
// Navigation clears the ref and a newer recovery action replaces the object, so
// stale async completions cannot resume work in an old tab/session or erase a
// newer intent.
export function consumeQuotaRecoveryIntent(
  ref: RefBox<QuotaRecoveryIntent | null>,
  expected: QuotaRecoveryIntent | null,
): boolean {
  if (!expected || ref.current !== expected) return false;
  ref.current = null;
  return true;
}
