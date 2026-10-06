import { post, type OpsRelay, type OpsWaiter } from "./ops_emit";

export interface RegistryPending {
  slug: string;
  version: string;
  kind: string;
  source: string;
  summary: string;
}

const RELAY_VALUE_UNITS = 80;
const FRAME_BYTES = 600;
const DEDUPE_MS = 60 * 60 * 1000;
const PER_MINUTE = 10;
const SEEN_MAX = 500;
const encoder = new TextEncoder();

const seen = new Map<string, number>();
let minute = { at: 0, n: 0 };
let dropped = 0;

export const registryDropped = () => dropped;

export function resetRegistryGate(): void {
  seen.clear();
  minute = { at: 0, n: 0 };
  dropped = 0;
}

function plain(value: string): string {
  let out = "";
  for (const ch of value) {
    const code = ch.codePointAt(0) ?? 0;
    const control = code < 0x20 || (code >= 0x7f && code <= 0x9f) || code === 0x2028 || code === 0x2029;
    out += control ? " " : ch;
  }
  return out.replace(/\s+/g, " ").trim();
}

function clip(value: string, units: number): string {
  let out = "";
  for (const ch of value) {
    if (out.length + ch.length > units) break;
    out += ch;
  }
  return out;
}

// The relay keeps 80 UTF-16 units per extra value, so the version is kept whole
// and the slug gives up the room.
function keyOf(slug: string, version: string): string {
  const v = clip(plain(version), 40);
  return `${clip(plain(slug), RELAY_VALUE_UNITS - 1 - v.length)}@${v}`;
}

const wireBytes = (body: object) =>
  encoder.encode(JSON.stringify({ ...body, id: 999999999999, ts: "2026-10-06T00:00:00.000Z" })).length;

// The relay drops a frame's whole extra above 600 bytes counted in UTF-8, so
// the free text is cut until the frame the relay will build fits.
function build(item: RegistryPending): object {
  const extra = {
    key: keyOf(item.slug, item.version),
    kind: clip(plain(item.kind), RELAY_VALUE_UNITS),
    source: clip(plain(item.source), RELAY_VALUE_UNITS),
    summary: clip(plain(item.summary), RELAY_VALUE_UNITS),
  };
  const body = { src: "registry", t: "pending", title: "registry submission pending", extra };
  while (wireBytes(body) > FRAME_BYTES && (extra.summary || extra.source)) {
    const field = extra.summary.length >= extra.source.length ? "summary" : "source";
    extra[field] = Array.from(extra[field]).slice(0, -1).join("");
  }
  return body;
}

function admit(key: string, now: number): boolean {
  const last = seen.get(key);
  if (last !== undefined && now - last < DEDUPE_MS) return false;
  const at = Math.floor(now / 60000);
  const n = minute.at === at ? minute.n : 0;
  if (n >= PER_MINUTE) {
    dropped++;
    return false;
  }
  minute = { at, n: n + 1 };
  seen.set(key, now);
  if (seen.size > SEEN_MAX) {
    for (const [k, t] of seen) if (now - t >= DEDUPE_MS || seen.size > SEEN_MAX) seen.delete(k);
  }
  return true;
}

// Registry events have their own gate so a burst of submissions cannot use up
// the relay's shared emit bucket that feedback events also draw from.
export function announceRegistryPending(ctx: OpsWaiter | undefined, relay: OpsRelay, item: RegistryPending): void {
  if (!admit(keyOf(item.slug, item.version), Date.now())) return;
  ctx?.waitUntil(post(relay, build(item)));
}
