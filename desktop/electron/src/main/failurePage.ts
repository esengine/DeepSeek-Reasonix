import type { HandshakeFailure } from "./handshake.js";

export const SHELL_ACTION_PREFIX = "reasonix://app/__shell/";

export type ShellAction = "open-logs" | "restart" | "quit";

export function shellActionFromURL(url: string): ShellAction | null {
  if (!url.startsWith(SHELL_ACTION_PREFIX)) return null;
  const action = url.slice(SHELL_ACTION_PREFIX.length).replace(/[?#].*$/, "");
  return action === "open-logs" || action === "restart" || action === "quit" ? action : null;
}

function escapeHTML(value: string): string {
  return value.replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char] as string);
}

export function renderFailurePage(failure: HandshakeFailure, logsPath: string): string {
  const code = failure.code === null ? failure.name : `${failure.name} (${failure.code})`;
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">
<title>Reasonix cannot start</title>
<style>
  html, body { margin: 0; height: 100%; background: #1a1a2e; color: #f4f4f3; font: 14px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
  main { max-width: 640px; margin: 0 auto; padding: 64px 32px; }
  h1 { font-size: 20px; margin: 0 0 12px; }
  .code { color: #b8b8c8; font-size: 12px; margin-bottom: 20px; }
  pre { white-space: pre-wrap; word-break: break-word; background: #11111f; border: 1px solid #2c2c48; border-radius: 8px; padding: 12px 14px; margin: 0 0 20px; }
  .actions { display: flex; gap: 10px; flex-wrap: wrap; }
  a { display: inline-block; padding: 8px 14px; border-radius: 8px; background: #2c2c48; color: inherit; text-decoration: none; }
  a.primary { background: #e58a3a; color: #111214; }
  .logs { color: #8f8fa8; font-size: 12px; margin-top: 20px; }
</style>
</head>
<body>
<main>
  <h1>${escapeHTML(failure.title)}</h1>
  <div class="code">${escapeHTML(code)}</div>
  <pre>${escapeHTML(failure.detail)}</pre>
  <div class="actions">
    <a class="primary" href="${SHELL_ACTION_PREFIX}restart">Restart service</a>
    <a href="${SHELL_ACTION_PREFIX}open-logs">Open logs folder</a>
    <a href="${SHELL_ACTION_PREFIX}quit">Quit</a>
  </div>
  <div class="logs">Logs: ${escapeHTML(logsPath)}</div>
</main>
</body>
</html>`;
}
