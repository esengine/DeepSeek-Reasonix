// Password hashing (PBKDF2-HMAC-SHA256) and token helpers, built on the Web
// Crypto API available in the Workers runtime. Ported from the crash-report
// worker and hardened: tokens are stored hashed, peppered with a server secret.
import { PBKDF2_ITERATIONS } from "../config";

const encoder = new TextEncoder();

function toBase64(bytes: Uint8Array): string {
  let s = "";
  for (const byte of bytes) s += String.fromCharCode(byte);
  return btoa(s);
}

function fromBase64(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function toHex(bytes: Uint8Array): string {
  let s = "";
  for (const byte of bytes) s += byte.toString(16).padStart(2, "0");
  return s;
}

async function deriveBits(password: string, salt: Uint8Array, iterations: number): Promise<Uint8Array> {
  const key = await crypto.subtle.importKey("raw", encoder.encode(password), "PBKDF2", false, ["deriveBits"]);
  const bits = await crypto.subtle.deriveBits({ name: "PBKDF2", salt, iterations, hash: "SHA-256" }, key, 256);
  return new Uint8Array(bits);
}

// Stored as `pbkdf2$<iters>$<salt-b64>$<hash-b64>` so the work factor travels
// with the hash and can be raised without invalidating older passwords.
export async function hashPassword(password: string): Promise<string> {
  const salt = crypto.getRandomValues(new Uint8Array(16));
  const hash = await deriveBits(password, salt, PBKDF2_ITERATIONS);
  return `pbkdf2$${PBKDF2_ITERATIONS}$${toBase64(salt)}$${toBase64(hash)}`;
}

export async function verifyPassword(password: string, stored: string | null): Promise<boolean> {
  if (!stored) return false;
  const [scheme, iters, saltB64, hashB64] = stored.split("$");
  if (scheme !== "pbkdf2" || !iters || !saltB64 || !hashB64) return false;
  const got = await deriveBits(password, fromBase64(saltB64), Number(iters));
  const want = fromBase64(hashB64);
  return got.byteLength === want.byteLength && crypto.subtle.timingSafeEqual(got, want);
}

export async function sha256Hex(input: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", encoder.encode(input));
  return toHex(new Uint8Array(digest));
}

// 256 bits of entropy as hex — used for session cookies and email links.
export function generateToken(): string {
  return toHex(crypto.getRandomValues(new Uint8Array(32)));
}

// Look-up key for a token: only the peppered hash is ever stored, so a DB read
// can neither resurrect a session nor redeem an email link.
export function hashToken(pepper: string, token: string): Promise<string> {
  return sha256Hex(`${pepper}:${token}`);
}

// A short random suffix for handle generation. Hex-encodes crypto bytes (a
// bijection — no modulo, so no bias) and trims to length. Hex digits are all
// valid handle characters.
export function randomSuffix(len: number): string {
  const bytes = crypto.getRandomValues(new Uint8Array(Math.ceil(len / 2)));
  let s = "";
  for (const byte of bytes) s += byte.toString(16).padStart(2, "0");
  return s.slice(0, len);
}
