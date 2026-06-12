import type { Env } from "./env";
import { esc } from "./shell";

export type Role = "pending" | "viewer" | "admin";

export interface User {
  id: number;
  email: string;
  role: Role;
  created_at: string;
  approved_at: string | null;
}

const RANK: Record<Role, number> = { pending: 0, viewer: 1, admin: 2 };

export function atLeast(role: Role, min: Role): boolean {
  return RANK[role] >= RANK[min];
}

const COOKIE = "rxsess";
const SESSION_TTL_MS = 30 * 86_400_000;
const PBKDF2_ITERS = 100_000;

function b64(bytes: Uint8Array): string {
  let s = "";
  for (const byte of bytes) s += String.fromCharCode(byte);
  return btoa(s);
}

function unb64(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

async function deriveBits(password: string, salt: Uint8Array, iterations: number): Promise<Uint8Array> {
  const key = await crypto.subtle.importKey("raw", new TextEncoder().encode(password), "PBKDF2", false, ["deriveBits"]);
  const bits = await crypto.subtle.deriveBits({ name: "PBKDF2", salt, iterations, hash: "SHA-256" }, key, 256);
  return new Uint8Array(bits);
}

export async function hashPassword(password: string): Promise<string> {
  const salt = crypto.getRandomValues(new Uint8Array(16));
  const hash = await deriveBits(password, salt, PBKDF2_ITERS);
  return `pbkdf2$${PBKDF2_ITERS}$${b64(salt)}$${b64(hash)}`;
}

export async function verifyPassword(password: string, stored: string): Promise<boolean> {
  const [scheme, iters, saltB64, hashB64] = stored.split("$");
  if (scheme !== "pbkdf2") return false;
  const got = await deriveBits(password, unb64(saltB64), Number(iters));
  const want = unb64(hashB64);
  return got.byteLength === want.byteLength && crypto.subtle.timingSafeEqual(got, want);
}

function randomToken(): string {
  const b = crypto.getRandomValues(new Uint8Array(32));
  return [...b].map((x) => x.toString(16).padStart(2, "0")).join("");
}

export async function createSession(env: Env, userId: number): Promise<string> {
  const token = randomToken();
  const now = Date.now();
  await env.DB.prepare("INSERT INTO sessions (token, user_id, created_at, expires_at) VALUES (?1, ?2, ?3, ?4)")
    .bind(token, userId, new Date(now).toISOString(), new Date(now + SESSION_TTL_MS).toISOString())
    .run();
  return token;
}

export async function destroySession(env: Env, token: string): Promise<void> {
  await env.DB.prepare("DELETE FROM sessions WHERE token = ?1").bind(token).run();
}

export async function endSession(request: Request, env: Env): Promise<void> {
  const token = getCookie(request, COOKIE);
  if (token) await destroySession(env, token);
}

export function sessionCookie(token: string): string {
  return `${COOKIE}=${token}; HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=${Math.floor(SESSION_TTL_MS / 1000)}`;
}

export function clearCookie(): string {
  return `${COOKIE}=; HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=0`;
}

export function getCookie(request: Request, name: string): string | null {
  const header = request.headers.get("cookie") ?? "";
  for (const part of header.split(";")) {
    const [k, ...v] = part.trim().split("=");
    if (k === name) return v.join("=");
  }
  return null;
}

export async function currentUser(request: Request, env: Env): Promise<User | null> {
  const token = getCookie(request, COOKIE);
  if (!token) return null;
  const row = await env.DB.prepare(
    `SELECT u.id, u.email, u.role, u.created_at, u.approved_at, s.expires_at
     FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token = ?1`,
  )
    .bind(token)
    .first<{ id: number; email: string; role: Role; created_at: string; approved_at: string | null; expires_at: string }>();
  if (!row) return null;
  if (row.expires_at < new Date().toISOString()) {
    await destroySession(env, token);
    return null;
  }
  return { id: row.id, email: row.email, role: row.role, created_at: row.created_at, approved_at: row.approved_at };
}

export function isAdminEmail(env: Env, email: string): boolean {
  const list = (env.ADMIN_EMAILS ?? "")
    .split(",")
    .map((s) => s.trim().toLowerCase())
    .filter(Boolean);
  return list.includes(email.toLowerCase());
}

// CSRF guard for cookie-authed POSTs: a missing Origin is rejected, not waved
// through as same-site.
export function sameOrigin(request: Request): boolean {
  const origin = request.headers.get("origin");
  if (!origin) return false;
  try {
    return new URL(origin).host === new URL(request.url).host;
  } catch {
    return false;
  }
}

export async function logAction(env: Env, actor: User, action: string, target = "", detail = ""): Promise<void> {
  await env.DB.prepare(
    "INSERT INTO audit_log (at, actor_id, actor_email, action, target, detail) VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
  )
    .bind(new Date().toISOString(), actor.id, actor.email, action, target, detail)
    .run();
}

export function userNav(user: User): string {
  const admin = user.role === "admin" ? `<a class="navlink" href="/admin">Admin</a>` : "";
  return `<span class="chip"><span class="badge ${user.role}">${user.role}</span>${esc(user.email)}</span><a class="navlink" href="/account">Account</a>${admin}<form method="post" action="/logout" class="inline"><button class="btn ghost sm">Sign out</button></form>`;
}
