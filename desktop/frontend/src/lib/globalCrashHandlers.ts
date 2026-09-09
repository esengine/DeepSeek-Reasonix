// Window-level failure routing. Wails rejects a bound Go call with the bare
// error string, so a stackless rejection is an ordinary backend error that is
// contained and toasted; only faults carrying a stack reach the crash overlay.

import {
  buildCrashPayload,
  globalCrashReportReason,
  isOpaqueScriptErrorEvent,
  normalizeCrashError,
  opaqueScriptFingerprintHint,
  paintCrashOverlay,
  reportCrash,
  shouldReportGlobalCrashEvent,
} from "./crash";
import { recordFrontendDiagnostic } from "./frontendDiagnosticBridge";

export const RECOVERABLE_ERROR_EVENT = "reasonix:recoverable-error";

export type RecoverableErrorDetail = { message: string };

export function isRecoverableRejectionReason(reason: unknown): boolean {
  if (typeof reason !== "object" || reason === null) return true;
  const stack = (reason as { stack?: unknown }).stack;
  return typeof stack !== "string" || stack.trim() === "";
}

export function onRecoverableError(cb: (detail: RecoverableErrorDetail) => void): () => void {
  const handler = (e: Event) => cb((e as CustomEvent<RecoverableErrorDetail>).detail);
  window.addEventListener(RECOVERABLE_ERROR_EVENT, handler);
  return () => window.removeEventListener(RECOVERABLE_ERROR_EVENT, handler);
}

function containRecoverableRejection(e: PromiseRejectionEvent): boolean {
  if (!isRecoverableRejectionReason(e.reason)) return false;
  e.preventDefault();
  recordFrontendDiagnostic("runtime", "unhandled-backend-rejection", { status: "error" });
  const detail: RecoverableErrorDetail = { message: normalizeCrashError(e.reason).errorMessage };
  window.dispatchEvent(new CustomEvent(RECOVERABLE_ERROR_EVENT, { detail }));
  return true;
}

export function installGlobalCrashHandlers() {
  window.addEventListener("error", (e) => {
    if (!shouldReportGlobalCrashEvent(e)) return;
    const payload = buildCrashPayload("window.error", globalCrashReportReason(e));
    if (isOpaqueScriptErrorEvent(e)) payload.fingerprintHint = opaqueScriptFingerprintHint();
    paintCrashOverlay(payload);
  });
  window.addEventListener("unhandledrejection", (e) => {
    if (!shouldReportGlobalCrashEvent(e) || containRecoverableRejection(e)) return;
    reportCrash("unhandledrejection", e.reason);
  });
}
