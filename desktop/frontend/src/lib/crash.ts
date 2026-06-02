// Last-resort crash surface: a React render error with no boundary unmounts the
// whole tree (blank window), and global errors/rejections leave no trace either.

function paint(text: string) {
  let host = document.getElementById("crash-overlay");
  if (!host) {
    host = document.createElement("div");
    host.id = "crash-overlay";
    document.body.appendChild(host);
  }
  const title = document.createElement("div");
  title.className = "crash-overlay__title";
  title.textContent = "Reasonix hit an error — screenshot this and send it over";
  const body = document.createElement("pre");
  body.className = "crash-overlay__body";
  body.textContent = text;
  const copy = document.createElement("button");
  copy.className = "crash-overlay__copy";
  copy.textContent = "Copy";
  copy.onclick = () => void navigator.clipboard?.writeText(text);
  host.replaceChildren(title, body, copy);
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
