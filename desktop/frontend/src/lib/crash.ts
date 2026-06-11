// Last-resort crash surface: a React render error with no boundary unmounts the
// whole tree (blank window), and global errors/rejections leave no trace either.

import { t } from "./i18n";

function sendButton(text: string): HTMLButtonElement | null {
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
      await report("crash", text);
      send.textContent = t("crash.sent");
    } catch {
      send.textContent = t("crash.sendFailed");
    }
  };
  return send;
}

function paint(text: string) {
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
  body.textContent = text;
  const copy = document.createElement("button");
  copy.className = "crash-overlay__copy";
  copy.textContent = t("crash.copy");
  copy.onclick = () => void navigator.clipboard?.writeText(text);
  const actions = document.createElement("div");
  actions.className = "crash-overlay__actions";
  const send = sendButton(text);
  if (send) actions.append(send);
  actions.append(copy);
  const note = document.createElement("div");
  note.className = "crash-overlay__note";
  note.textContent = t("crash.privacyNote");
  host.replaceChildren(title, body, actions, ...(send ? [note] : []));
}

function format(label: string, err: unknown, extra?: string): string {
  const e = err as { message?: string; stack?: string } | null;
  const detail = e?.stack || e?.message || String(err);
  return [`[${label}]`, detail, extra?.trim()].filter(Boolean).join("\n\n");
}

export function reportCrash(label: string, err: unknown, extra?: string) {
  paint(format(label, err, extra));
}

export function installGlobalCrashHandlers() {
  window.addEventListener("error", (e) => reportCrash("window.error", e.error ?? e.message));
  window.addEventListener("unhandledrejection", (e) => reportCrash("unhandledrejection", e.reason));
}
