const enc = new TextEncoder();
const CROCKFORD = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

export function base64url(bytes: Uint8Array): string {
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function hex(bytes: Uint8Array): string {
  return [...bytes].map((b) => b.toString(16).padStart(2, "0")).join("");
}

async function hmac(secret: string, label: string, value: string): Promise<Uint8Array> {
  const key = await crypto.subtle.importKey("raw", enc.encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  return new Uint8Array(await crypto.subtle.sign("HMAC", key, enc.encode(`${label}:${value}`)));
}

// Labels keep the uses of one secret apart, so an install hash can never be
// replayed as a token and none of them correlates with telemetry's plain hashes.
export async function installHash(secret: string, installId: string): Promise<string> {
  return hex(await hmac(secret, "feedback-install", installId));
}

export async function accountHash(secret: string, userId: string): Promise<string> {
  return hex(await hmac(secret, "feedback-account", userId));
}

export async function sign(secret: string, label: string, value: string): Promise<string> {
  return base64url(await hmac(secret, label, value));
}

export async function installToken(secret: string, installId: string): Promise<string> {
  return base64url(await hmac(secret, "feedback-token", installId));
}

// IPv6 clients are counted by /64, the smallest block a subscriber controls.
export function ipPrefix(ip: string): string {
  if (!ip.includes(":")) return ip;
  const [head, tail = ""] = ip.split("::");
  const groups = head.split(":").filter(Boolean);
  const rest = tail.split(":").filter(Boolean);
  const full = ip.includes("::") ? [...groups, ...new Array(Math.max(0, 8 - groups.length - rest.length)).fill("0"), ...rest] : groups;
  return full.slice(0, 4).map((g) => g.toLowerCase().replace(/^0+(?=.)/, "")).join(":");
}

export async function ipHash(secret: string, ip: string): Promise<string> {
  return hex((await hmac(secret, "feedback-ip", ipPrefix(ip))).slice(0, 12));
}

// Compares fixed-length digests so timing does not depend on where inputs differ.
export async function constantTimeEqual(a: string, b: string): Promise<boolean> {
  const [da, db] = await Promise.all([crypto.subtle.digest("SHA-256", enc.encode(a)), crypto.subtle.digest("SHA-256", enc.encode(b))]);
  const x = new Uint8Array(da);
  const y = new Uint8Array(db);
  let diff = 0;
  for (let i = 0; i < x.length; i++) diff |= x[i] ^ y[i];
  return diff === 0;
}

export function newReceipt(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(8));
  const chars = [...bytes].map((b) => CROCKFORD[b & 31]).join("");
  return `FB-${chars.slice(0, 4)}-${chars.slice(4)}`;
}

export function newAttachmentKey(): string {
  return base64url(crypto.getRandomValues(new Uint8Array(24)));
}
