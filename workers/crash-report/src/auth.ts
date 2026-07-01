// Dashboard authorization. Identity comes from id.reasonix.io (the shared
// account service); this worker only maps a signed-in identity to a per-dashboard
// role via the `access` table.
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

// The id.reasonix.io session cookie is scoped to `.reasonix.io`, so the browser
// sends it here too; we hand it back as a Bearer token to resolve the identity.
const SHARED_COOKIE = "rxid";

function idOrigin(env: Env): string {
  return (env.ID_ORIGIN ?? "https://id.reasonix.io").replace(/\/$/, "");
}

function appOrigin(env: Env): string {
  return (env.APP_ORIGIN ?? "https://reasonix.io").replace(/\/$/, "");
}

export function getCookie(request: Request, name: string): string | null {
  const header = request.headers.get("cookie") ?? "";
  for (const part of header.split(";")) {
    const [k, ...v] = part.trim().split("=");
    if (k === name) return v.join("=");
  }
  return null;
}

// Where an unauthenticated visitor is sent to sign in: the shared login page,
// with a same-site `next` back to the page they wanted.
export function loginUrl(env: Env, request: Request): string {
  const here = new URL(request.url);
  const next = `${here.origin}${here.pathname}`;
  return `${appOrigin(env)}/login/?next=${encodeURIComponent(next)}`;
}

interface Identity {
  email: string;
  emailVerified: boolean;
}

async function resolveIdentity(request: Request, env: Env): Promise<Identity | null> {
  const token = getCookie(request, SHARED_COOKIE);
  if (!token) return null;
  const res = await fetch(`${idOrigin(env)}/me`, { headers: { authorization: `Bearer ${token}` } });
  if (!res.ok) return null;
  const data = await res.json<{ user?: { email?: string; emailVerified?: boolean } }>().catch(() => null);
  const email = data?.user?.email?.toLowerCase();
  if (!email) return null;
  return { email, emailVerified: data?.user?.emailVerified === true };
}

// Resolves the signed-in dashboard user, or null. A first-seen identity is
// recorded as `pending`; an ADMIN_EMAILS identity is force-promoted to admin so
// the owner can never be locked out of their own dashboard.
export async function currentUser(request: Request, env: Env): Promise<User | null> {
  const identity = await resolveIdentity(request, env);
  if (!identity) return null;

  const now = new Date().toISOString();
  const bootstrapAdmin = isAdminEmail(env, identity.email);
  let row = await selectAccess(env, identity.email);
  if (!row) {
    await env.DB.prepare(
      "INSERT INTO access (email, role, created_at, approved_at) VALUES (?1, ?2, ?3, ?4) ON CONFLICT(email) DO NOTHING",
    )
      .bind(identity.email, bootstrapAdmin ? "admin" : "pending", now, bootstrapAdmin ? now : null)
      .run();
    row = await selectAccess(env, identity.email);
    if (!row) return null;
  } else if (bootstrapAdmin && row.role !== "admin") {
    await env.DB.prepare("UPDATE access SET role = 'admin', approved_at = COALESCE(approved_at, ?2) WHERE id = ?1")
      .bind(row.id, now)
      .run();
    row.role = "admin";
  }
  return row;
}

function selectAccess(env: Env, email: string): Promise<User | null> {
  return env.DB.prepare("SELECT id, email, role, created_at, approved_at FROM access WHERE email = ?1")
    .bind(email)
    .first<User>();
}

// Ends the shared id.reasonix.io session (best-effort) and returns a Set-Cookie
// that clears the shared cookie browser-side.
export async function sharedLogout(request: Request, env: Env): Promise<string> {
  const token = getCookie(request, SHARED_COOKIE);
  if (token) {
    await fetch(`${idOrigin(env)}/auth/logout`, {
      method: "POST",
      headers: { authorization: `Bearer ${token}` },
    }).catch(() => {});
  }
  return `${SHARED_COOKIE}=; HttpOnly; Secure; SameSite=Lax; Path=/; Domain=.reasonix.io; Max-Age=0`;
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
