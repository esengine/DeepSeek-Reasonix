// Node's built-in fetch ignores HTTPS_PROXY env vars — undici's ProxyAgent has to
// be wired in explicitly. A custom dispatcher routes around the proxy for hosts
// matched by NO_PROXY (curl-style) so DeepSeek API stays direct while user-set
// HTTPS_PROXY still routes everything else through the user's proxy.

import { Agent, type Dispatcher, ProxyAgent, setGlobalDispatcher } from "undici";

/** Env-var precedence matches curl: HTTPS_PROXY → HTTP_PROXY → ALL_PROXY, upper-case first then lower. */
const PROXY_ENV_KEYS = [
  "HTTPS_PROXY",
  "https_proxy",
  "HTTP_PROXY",
  "http_proxy",
  "ALL_PROXY",
  "all_proxy",
] as const;

const NO_PROXY_ENV_KEYS = ["NO_PROXY", "no_proxy"] as const;

// DeepSeek's API origin is in CN; routing it through a user's clash/v2ray
// (typically a US-exit pool) lands on shared abuse IPs that DeepSeek 403s.
// Localhost entries protect the dashboard, MCP stdio sidecars' HTTP probes,
// and the `reasonix doctor` reachability checks.
export const DEFAULT_NO_PROXY = [
  "api.deepseek.com",
  "*.deepseek.com",
  "localhost",
  "127.0.0.1",
  "::1",
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

export function detectNoProxyRaw(env: NodeJS.ProcessEnv = process.env): string | null {
  for (const key of NO_PROXY_ENV_KEYS) {
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

export interface NoProxyPattern {
  /** Raw pattern text, kept for /doctor display. */
  raw: string;
  matches: (host: string) => boolean;
}

/** Curl-style NO_PROXY parsing: comma-separated, supports `*` (all), bare host (exact OR `.host` suffix), `.suffix`, `*.suffix`, IP literals. Strips optional `:port` since we only match by host. */
export function parseNoProxy(raw: string | null | undefined): NoProxyPattern[] {
  if (!raw) return [];
  const out: NoProxyPattern[] = [];
  for (const segment of raw.split(",")) {
    const trimmed = segment.trim();
    if (!trimmed) continue;
    out.push(buildPattern(trimmed));
  }
  return out;
}

function buildPattern(raw: string): NoProxyPattern {
  // Strip optional :port — we route by host only.
  const colon = raw.lastIndexOf(":");
  const hostPart = colon !== -1 && /^\d+$/.test(raw.slice(colon + 1)) ? raw.slice(0, colon) : raw;
  const normalized = hostPart.toLowerCase();
  if (normalized === "*") {
    return { raw, matches: () => true };
  }
  if (normalized.startsWith("*.")) {
    const suffix = normalized.slice(1); // ".foo.com"
    const bare = normalized.slice(2); // "foo.com"
    return {
      raw,
      matches: (host) => {
        const h = host.toLowerCase();
        return h === bare || h.endsWith(suffix);
      },
    };
  }
  if (normalized.startsWith(".")) {
    return {
      raw,
      matches: (host) => host.toLowerCase().endsWith(normalized),
    };
  }
  return {
    raw,
    matches: (host) => {
      const h = host.toLowerCase();
      return h === normalized || h.endsWith(`.${normalized}`);
    },
  };
}

export function matchesNoProxy(host: string, patterns: readonly NoProxyPattern[]): boolean {
  for (const p of patterns) {
    if (p.matches(host)) return true;
  }
  return false;
}

class SelectiveProxyDispatcher {
  private readonly direct: Agent;
  private readonly proxied: ProxyAgent;
  private readonly patterns: readonly NoProxyPattern[];

  constructor(proxyUrl: string, patterns: readonly NoProxyPattern[]) {
    this.direct = new Agent();
    this.proxied = new ProxyAgent(proxyUrl);
    this.patterns = patterns;
  }

  dispatch(
    opts: Dispatcher.DispatchOptions,
    handler: Dispatcher.DispatchHandler,
  ): ReturnType<Dispatcher["dispatch"]> {
    const origin = opts.origin;
    let host = "";
    try {
      if (typeof origin === "string") {
        host = new URL(origin).hostname;
      } else if (origin instanceof URL) {
        host = origin.hostname;
      }
    } catch {
      // Fall through with empty host — won't match patterns, will route via proxy.
    }
    const target = host && matchesNoProxy(host, this.patterns) ? this.direct : this.proxied;
    return (target as unknown as Dispatcher).dispatch(opts, handler);
  }

  async close(): Promise<void> {
    await Promise.allSettled([this.direct.close(), this.proxied.close()]);
  }

  async destroy(): Promise<void> {
    await Promise.allSettled([this.direct.destroy(), this.proxied.destroy()]);
  }
}

let installed = false;

export interface ProxyInstallResult {
  url: string;
  reinstalled: boolean;
  noProxy: readonly NoProxyPattern[];
}

export interface InstallProxyOptions {
  /** Skip proxy install entirely — for `--no-proxy` / `cfg.proxy.disabled` / env-driven kill-switch. */
  disabled?: boolean;
  /** Additional NO_PROXY patterns layered on top of defaults + env. Sourced from `cfg.proxy.noProxy` / `REASONIX_NO_PROXY`. */
  extraNoProxy?: readonly string[];
}

/** Sets the undici global dispatcher to a SelectiveProxyDispatcher (proxy for non-NO_PROXY hosts, direct for matches). Returns the proxy URL + parsed NO_PROXY patterns, or null when no env var is set, the value is unparseable, the ProxyAgent ctor throws, or opts.disabled is true. Idempotent. */
export function installProxyIfConfigured(
  env: NodeJS.ProcessEnv = process.env,
  opts: InstallProxyOptions = {},
): ProxyInstallResult | null {
  if (opts.disabled) return null;
  const raw = detectProxyUrl(env);
  if (!raw) return null;
  const url = normalizeProxyUrl(raw);
  if (!url) {
    process.stderr.write(
      `▲ ignoring proxy env value ${JSON.stringify(raw)} — not a valid URL. Expected something like \`http://host:port\` or \`socks5://host:port\`.\n`,
    );
    return null;
  }

  // Default whitelist always applies; env NO_PROXY, REASONIX_NO_PROXY, and
  // opts.extraNoProxy (config) all layer on top additively.
  const defaultNoProxy = parseNoProxy(DEFAULT_NO_PROXY.join(","));
  const envNoProxy = parseNoProxy(detectNoProxyRaw(env));
  const reasonixNoProxy = parseNoProxy(
    typeof env.REASONIX_NO_PROXY === "string" ? env.REASONIX_NO_PROXY : null,
  );
  const extraNoProxy = parseNoProxy((opts.extraNoProxy ?? []).join(","));
  const patterns = [...defaultNoProxy, ...envNoProxy, ...reasonixNoProxy, ...extraNoProxy];

  try {
    const reinstalled = installed;
    setGlobalDispatcher(new SelectiveProxyDispatcher(url, patterns) as unknown as Dispatcher);
    installed = true;
    const bypassList = patterns.map((p) => p.raw).join(",");
    process.stderr.write(`[proxy] using ${url} (NO_PROXY: ${bypassList})\n`);
    return { url, reinstalled, noProxy: patterns };
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
