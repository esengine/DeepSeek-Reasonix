// Last-resort crash surface: a React render error with no boundary unmounts the
// whole tree (blank window), and global errors/rejections leave no trace either.

import { dumpBreadcrumbs, snapshotBreadcrumbs, type Breadcrumb } from "./breadcrumbs";
import { t } from "./i18n";

declare const __BUILD_COMMIT__: string;
declare const __BUILD_CHANNEL__: string;

export type CrashKind = "crash" | "exception" | "feedback";

export type CrashPayload = {
  schemaVersion: 2;
  source: "frontend" | "frontend.react" | "frontend.global";
  kind: CrashKind;
  label: string;
  message: string;
  errorType: string;
  errorMessage: string;
  stack?: string;
  componentStack?: string;
  topFrame?: string;
  buildCommit: string;
  channel: string;
  language: string;
  view: string;
  breadcrumbs: Breadcrumb[];
  occurredAt: string;
};

type NormalizedError = {
  errorType: string;
  errorMessage: string;
  stack?: string;
};

function clip(s: string, n: number): string {
  return s.length > n ? s.slice(0, n) : s;
}

function safeStringify(value: unknown): string {
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

export function normalizeCrashError(err: unknown): NormalizedError {
  if (err instanceof Error) {
    return {
      errorType: err.name || "Error",
      errorMessage: err.message || String(err),
      stack: err.stack,
    };
  }
  if (typeof err === "string") {
    return { errorType: "string", errorMessage: err };
  }
  if (err && typeof err === "object") {
    const obj = err as { name?: unknown; message?: unknown; stack?: unknown; constructor?: { name?: string } };
    const errorType = typeof obj.name === "string" && obj.name ? obj.name : obj.constructor?.name || "object";
    const errorMessage =
      typeof obj.message === "string" && obj.message ? obj.message : clip(safeStringify(err), 1000);
    return {
      errorType,
      errorMessage,
      stack: typeof obj.stack === "string" ? obj.stack : undefined,
    };
  }
  return { errorType: typeof err, errorMessage: String(err) };
}

export function topFrameFromStack(stack?: string): string {
  if (!stack) return "";
  const lines = stack
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean);
  return lines.find((l) => /\b(src|assets|wails|frontend)\b|\.tsx?:|\.jsx?:/.test(l)) ?? lines[1] ?? lines[0] ?? "";
}

function currentView(): string {
  if (typeof window === "undefined") return "";
  const { protocol, host, pathname, hash } = window.location;
  const safeHash = hash && hash.length < 80 ? hash : "";
  return clip(`${protocol}//${host}${pathname}${safeHash}`, 180);
}

function kindForLabel(label: string): CrashKind {
  return label === "unhandledrejection" ? "exception" : "crash";
}

function sourceForLabel(label: string): CrashPayload["source"] {
  if (label === "react") return "frontend.react";
  if (label === "window.error" || label === "unhandledrejection") return "frontend.global";
  return "frontend";
}

function formatText(label: string, normalized: NormalizedError, extra?: string): string {
  const detail = normalized.stack || normalized.errorMessage;
  const crumbs = dumpBreadcrumbs();
  const buildCommit = typeof __BUILD_COMMIT__ === "string" ? __BUILD_COMMIT__ : "dev";
  return [`[${label}]`, detail, extra?.trim(), crumbs && `--- breadcrumbs ---\n${crumbs}`, `build ${buildCommit}`]
    .filter(Boolean)
    .join("\n\n");
}

export function buildCrashPayload(label: string, err: unknown, extra?: string): CrashPayload {
  const normalized = normalizeCrashError(err);
  const buildCommit = typeof __BUILD_COMMIT__ === "string" ? __BUILD_COMMIT__ : "dev";
  return {
    schemaVersion: 2,
    source: sourceForLabel(label),
    kind: kindForLabel(label),
    label,
    message: formatText(label, normalized, extra),
    errorType: normalized.errorType,
    errorMessage: normalized.errorMessage,
    stack: normalized.stack,
    componentStack: extra?.trim() || undefined,
    topFrame: topFrameFromStack(normalized.stack || extra),
    buildCommit,
    channel: typeof __BUILD_CHANNEL__ === "string" ? __BUILD_CHANNEL__ : "",
    language: typeof navigator !== "undefined" ? navigator.language || "" : "",
    view: currentView(),
    breadcrumbs: snapshotBreadcrumbs(),
    occurredAt: new Date().toISOString(),
  };
}

function sendButton(payload: CrashPayload): HTMLButtonElement | null {
  // Resolved at click time via window.go, not the bridge module: this overlay must
  // stay usable even when the rest of the app (and its imports) is broken.
  const report = window.go?.main?.App?.ReportCrash;
  if (!report) return null;
  const send = document.createElement("button");
  send.className = "crash-overlay__send";
  send.textContent = t("crash.send");
  send.onclick = async () => {
    send.disabled = true;
    send.textContent = t("crash.sending");
    try {
      await report(payload.kind, JSON.stringify(payload));
      send.textContent = t("crash.sent");
    } catch {
      send.textContent = t("crash.sendFailed");
    }
  };
  return send;
}

function paint(payload: CrashPayload) {
  let host = document.getElementById("crash-overlay");
  if (!host) {
    host = document.createElement("div");
    host.id = "crash-overlay";
    document.body.appendChild(host);
  }
  const title = document.createElement("div");
  title.className = "crash-overlay__title";
  title.textContent = t("crash.title");
  const body = document.createElement("pre");
  body.className = "crash-overlay__body";
  body.textContent = payload.message;
  const copy = document.createElement("button");
  copy.className = "crash-overlay__copy";
  copy.textContent = t("crash.copy");
  copy.onclick = () => void navigator.clipboard?.writeText(payload.message);
  const actions = document.createElement("div");
  actions.className = "crash-overlay__actions";
  const send = sendButton(payload);
  if (send) actions.append(send);
  actions.append(copy);
  const note = document.createElement("div");
  note.className = "crash-overlay__note";
  note.textContent = t("crash.privacyNote");
  host.replaceChildren(title, body, actions, ...(send ? [note] : []));
}

export function reportCrash(label: string, err: unknown, extra?: string) {
  paint(buildCrashPayload(label, err, extra));
}

export function installGlobalCrashHandlers() {
  window.addEventListener("error", (e) => reportCrash("window.error", e.error ?? e.message));
  window.addEventListener("unhandledrejection", (e) => reportCrash("unhandledrejection", e.reason));
}
