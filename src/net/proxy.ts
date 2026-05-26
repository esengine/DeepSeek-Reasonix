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

// Loopback bypass protects the dashboard, MCP stdio sidecars' HTTP probes, and
// `reasonix doctor` reachability checks; non-negotiable.
const LOOPBACK_NO_PROXY = ["localhost", "127.0.0.1", "::1"] as const;

// DeepSeek's API origin is in CN; routing it through a user's clash/v2ray
// (typically a US-exit pool) lands on shared abuse IPs that DeepSeek 403s.
// Opt-out via `proxy.bypassDeepSeekDirect: false` (config) or
// `REASONIX_PROXY_DEEPSEEK_DIRECT=0` (env) for corporate firewalls that block
// direct egress and require the proxy to reach api.deepseek.com (issue #1497).
const DEEPSEEK_NO_PROXY = ["api.deepseek.com", "*.deepseek.com"] as const;

export const DEFAULT_NO_PROXY = [...DEEPSEEK_NO_PROXY, ...LOOPBACK_NO_PROXY] as const;

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

export interface InstallProxyOptions {
  /** Skip proxy install entirely — for `--no-proxy` / `cfg.proxy.disabled` / env-driven kill-switch. */
  disabled?: boolean;
  /** Config-supplied proxy URL. Wins over env detection so desktop-GUI users who can't reliably set HTTPS_PROXY can still route via `cfg.proxy.url` (issue #1868). */
  url?: string;
  /** Additional NO_PROXY patterns layered on top of defaults + env. Sourced from `cfg.proxy.noProxy` / `REASONIX_NO_PROXY`. */
  extraNoProxy?: readonly string[];
  /** When false, route api.deepseek.com / *.deepseek.com through the proxy too (issue #1497). Default true preserves the clash/v2ray US-exit-IP 403 fix. */
  bypassDeepSeekDirect?: boolean;
}

export type ProxyUrlSource = "config" | "env";

export interface ProxyInstallResult {
  url: string;
  source: ProxyUrlSource;
  reinstalled: boolean;
  noProxy: readonly NoProxyPattern[];
}

export interface ResolvedNoProxy {
  defaults: NoProxyPattern[];
  envSystem: NoProxyPattern[];
  envReasonix: NoProxyPattern[];
  extra: NoProxyPattern[];
  /** Defaults + env + REASONIX + extra concatenated. The same list `installProxyIfConfigured` uses. */
  all: NoProxyPattern[];
}

/** Env `REASONIX_PROXY_DEEPSEEK_DIRECT` (1/0/true/false/yes/no/on/off) overrides config when set; defaults true. Per issue #1497, corporate firewalls need the env knob since they often can't edit `~/.reasonix/config.json` ergonomically. */
export function resolveBypassDeepSeekDirect(
  env: NodeJS.ProcessEnv,
  configValue: boolean | undefined,
): boolean {
  const raw = env.REASONIX_PROXY_DEEPSEEK_DIRECT;
  if (typeof raw === "string") {
    const trimmed = raw.trim().toLowerCase();
    if (trimmed === "0" || trimmed === "false" || trimmed === "no" || trimmed === "off") {
      return false;
    }
    if (trimmed === "1" || trimmed === "true" || trimmed === "yes" || trimmed === "on") {
      return true;
    }
  }
  if (configValue === false) return false;
  return true;
}

/** Merge default + env + REASONIX_NO_PROXY + opts.extraNoProxy into one resolved view. Same composition as installProxyIfConfigured so /doctor can show what's actually applied. */
export function resolveNoProxy(
  env: NodeJS.ProcessEnv = process.env,
  opts: { extraNoProxy?: readonly string[]; bypassDeepSeekDirect?: boolean } = {},
): ResolvedNoProxy {
  const wantsDeepSeekDirect = resolveBypassDeepSeekDirect(env, opts.bypassDeepSeekDirect);
  const defaultHosts = wantsDeepSeekDirect
    ? [...DEEPSEEK_NO_PROXY, ...LOOPBACK_NO_PROXY]
    : LOOPBACK_NO_PROXY;
  const defaults = parseNoProxy(defaultHosts.join(","));
  const envSystem = parseNoProxy(detectNoProxyRaw(env));
  const envReasonix = parseNoProxy(
    typeof env.REASONIX_NO_PROXY === "string" ? env.REASONIX_NO_PROXY : null,
  );
  const extra = parseNoProxy((opts.extraNoProxy ?? []).join(","));
  return {
    defaults,
    envSystem,
    envReasonix,
    extra,
    all: [...defaults, ...envSystem, ...envReasonix, ...extra],
  };
}

/** Sets the undici global dispatcher to a SelectiveProxyDispatcher (proxy for non-NO_PROXY hosts, direct for matches). `opts.url` (from `cfg.proxy.url`) wins over env detection; falls back to HTTPS_PROXY / HTTP_PROXY / ALL_PROXY. Returns the proxy URL + source + parsed NO_PROXY patterns, or null when nothing is configured, the value is unparseable, the ProxyAgent ctor throws, or opts.disabled is true. Idempotent. */
export function installProxyIfConfigured(
  env: NodeJS.ProcessEnv = process.env,
  opts: InstallProxyOptions = {},
): ProxyInstallResult | null {
  if (opts.disabled) return null;
  const configRaw = typeof opts.url === "string" && opts.url.trim() !== "" ? opts.url.trim() : null;
  const raw = configRaw ?? detectProxyUrl(env);
  if (!raw) return null;
  const url = normalizeProxyUrl(raw);
  if (!url) {
    const origin = configRaw ? "config value" : "env value";
    process.stderr.write(
      `▲ ignoring proxy ${origin} ${JSON.stringify(raw)} — not a valid URL. Expected something like \`http://host:port\` or \`socks5://host:port\`.\n`,
    );
    return null;
  }
  const source: ProxyUrlSource = configRaw ? "config" : "env";

  // Default whitelist always applies; env NO_PROXY, REASONIX_NO_PROXY, and
  // opts.extraNoProxy (config) all layer on top additively. Composition lives
  // in resolveNoProxy() so /doctor and install can't drift.
  const { all: patterns } = resolveNoProxy(env, {
    extraNoProxy: opts.extraNoProxy,
    bypassDeepSeekDirect: opts.bypassDeepSeekDirect,
  });

  try {
    const reinstalled = installed;
    setGlobalDispatcher(new SelectiveProxyDispatcher(url, patterns) as unknown as Dispatcher);
    installed = true;
    const bypassList = patterns.map((p) => p.raw).join(",");
    process.stderr.write(`[proxy] using ${url} (source: ${source}, NO_PROXY: ${bypassList})\n`);
    return { url, source, reinstalled, noProxy: patterns };
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
