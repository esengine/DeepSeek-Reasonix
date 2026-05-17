/** Node's built-in fetch ignores HTTPS_PROXY env vars — undici's ProxyAgent has to be wired in explicitly. */

import { ProxyAgent, setGlobalDispatcher } from "undici";

/** Env-var precedence matches curl: HTTPS_PROXY → HTTP_PROXY → ALL_PROXY, upper-case first then lower. */
const PROXY_ENV_KEYS = [
  "HTTPS_PROXY",
  "https_proxy",
  "HTTP_PROXY",
  "http_proxy",
  "ALL_PROXY",
  "all_proxy",
] as const;

export function detectProxyUrl(env: NodeJS.ProcessEnv = process.env): string | null {
  for (const key of PROXY_ENV_KEYS) {
    const raw = env[key];
    if (typeof raw !== "string") continue;
    const trimmed = raw.trim();
    if (trimmed) return trimmed;
  }
  return null;
}

/** Auto-prefix `http://` when the env value is bare `host:port` (issue #1034 — Windows users routinely set `HTTPS_PROXY=127.0.0.1:10888` without a scheme, and undici's ProxyAgent ctor calls `new URL(...)` which throws and kills startup). */
export function normalizeProxyUrl(raw: string): string | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const candidate = /^[a-z][a-z0-9+.-]*:\/\//i.test(trimmed) ? trimmed : `http://${trimmed}`;
  try {
    return new URL(candidate).toString();
  } catch {
    return null;
  }
}

let installed = false;

/** Sets the undici global dispatcher to a ProxyAgent. Returns the proxy URL or null if no env var is set, the value is unparseable, or the ProxyAgent ctor throws. Idempotent. */
export function installProxyIfConfigured(
  env: NodeJS.ProcessEnv = process.env,
): { url: string; reinstalled: boolean } | null {
  const raw = detectProxyUrl(env);
  if (!raw) return null;
  const url = normalizeProxyUrl(raw);
  if (!url) {
    process.stderr.write(
      `▲ ignoring proxy env value ${JSON.stringify(raw)} — not a valid URL. Expected something like \`http://host:port\` or \`socks5://host:port\`.\n`,
    );
    return null;
  }
  try {
    const reinstalled = installed;
    setGlobalDispatcher(new ProxyAgent(url));
    installed = true;
    return { url, reinstalled };
  } catch (err) {
    process.stderr.write(
      `▲ proxy install failed (${(err as Error).message}); continuing without proxy.\n`,
    );
    return null;
  }
}

/** Test-only escape hatch so the installed flag doesn't leak between vitest cases. */
export function _resetForTests(): void {
  installed = false;
}
