// Provider compatibility values are shared by add and edit. Keeping the wire
// vocabulary here prevents the two forms from drifting while letting the UI
// translate the labels independently.
export const THINKING: [string, string][] = [
  ["", "自动 · 按模型和地址推断"],
  ["openai", "OpenAI 风格"],
  ["anthropic", "Anthropic thinking"],
  ["deepseek", "DeepSeek"],
  ["glm", "GLM enable_thinking"],
  ["kimi-k3", "Kimi K3"],
  ["none", "不发思考参数"],
];

export function headerLines(headers?: Record<string, string>): string {
  return Object.entries(headers ?? {})
    .map(([k, v]) => `${k}: ${v}`)
    .join("\n");
}

export function parseHeaders(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const at = line.indexOf(":");
    if (at <= 0) continue;
    const name = line.slice(0, at).trim();
    const value = line.slice(at + 1).trim();
    if (name && value) out[name] = value;
  }
  return out;
}

// null means "typed but invalid", unlike an empty object which is a deliberate
// request to leave no custom request fields behind.
export function parseExtraBody(text: string): Record<string, unknown> | null {
  if (!text.trim()) return {};
  try {
    const parsed: unknown = JSON.parse(text);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
    return parsed as Record<string, unknown>;
  } catch {
    return null;
  }
}

// Mirrors what the kernel stores: lower-cased, deduplicated, and without auto,
// which every ladder already opens with.
export function parseEffortLevels(text: string): string[] {
  const out: string[] = [];
  for (const raw of text.split(/[\s,，、]+/)) {
    const level = raw.trim().toLowerCase();
    if (level && level !== "auto" && !out.includes(level)) out.push(level);
  }
  return out;
}

// The kernel's own bounds for a provider's idle_timeout_seconds.
export const IDLE_TIMEOUT_MIN = 1;
export const IDLE_TIMEOUT_MAX = 32767;

/** Empty is the default, which the kernel stores as 0; anything else has to be
 *  a plain integer inside the bounds, because a silently coerced "1.5" would
 *  save a different number than the one typed. */
export function parseIdleTimeout(text: string): { ok: true; secs: number } | { ok: false } {
  const v = text.trim();
  if (v === "") return { ok: true, secs: 0 };
  if (!/^[1-9]\d*$/.test(v)) return { ok: false };
  const secs = Number(v);
  return secs >= IDLE_TIMEOUT_MIN && secs <= IDLE_TIMEOUT_MAX ? { ok: true, secs } : { ok: false };
}

/** The body a request would carry for a level, built from the protocol's dotted
 *  field path, so the example is the declaration itself rather than a table. */
export function effortExample(field: string, level: string): string {
  const body = field.split(".").reduceRight<unknown>((inner, key) => ({ [key]: inner }), level);
  return JSON.stringify(body);
}
