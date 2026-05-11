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

let installed = false;

/** Sets the undici global dispatcher to a ProxyAgent. Returns the proxy URL or null if no env var is set. Idempotent. */
export function installProxyIfConfigured(
  env: NodeJS.ProcessEnv = process.env,
): { url: string; reinstalled: boolean } | null {
  const url = detectProxyUrl(env);
  if (!url) return null;
  const reinstalled = installed;
  setGlobalDispatcher(new ProxyAgent(url));
  installed = true;
  return { url, reinstalled };
}

/** Test-only escape hatch so the installed flag doesn't leak between vitest cases. */
export function _resetForTests(): void {
  installed = false;
}
