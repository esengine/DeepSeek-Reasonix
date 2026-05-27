// Centralised error sink so we have one place to redirect when embedding
// in a host that owns stderr (e.g. a TUI inside another TUI). Prefer the
// stack trace when available — for renderer bugs the call chain matters
// more than the message.
export function logError(err: unknown): void {
  const message =
    err instanceof Error ? err.stack ?? err.message : String(err);
  // stderr may be closed during shutdown; refusing to log is preferable to
  // crashing the host application.
  try {
    process.stderr.write(`[ink] ${message}\n`);
  } catch {
    /* stderr unavailable during shutdown */
  }
}
