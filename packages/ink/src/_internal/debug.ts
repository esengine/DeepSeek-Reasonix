import { isEnvTruthy } from './envUtils.js';

type DebugOptions = {
  reentrant?: boolean;
  level?: string | number;
};

// Off by default. The env var lets us trace renderer activity in production
// builds without recompiling — useful when a downstream consumer hits a
// terminal-specific glitch we cannot reproduce locally.
let debugEnabled = isEnvTruthy(process.env['REASONIX_DEBUG_INK']);

export function setDebugLogging(enabled: boolean): void {
  debugEnabled = enabled;
}

export function logForDebugging(message: string, _opts?: DebugOptions): void {
  if (!debugEnabled) return;
  // stderr can throw EPIPE when the receiving end has gone away (e.g. the
  // process is being torn down). Diagnostics must never crash the host app,
  // so we swallow — there is nowhere else to report this anyway.
  try {
    process.stderr.write(`[ink·debug] ${message}\n`);
  } catch {
    /* stderr unavailable during shutdown */
  }
}
