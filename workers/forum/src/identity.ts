import type { Context, MiddlewareHandler } from "hono";
import type { AppEnv, Bindings, Member } from "./env";

// The id.reasonix.io session cookie is scoped to `.reasonix.io`, so the browser
// sends it here too; we hand it back as a Bearer token to resolve the identity.
const SHARED_COOKIE = "rxid";

function bearerFrom(c: Context<AppEnv>): string | undefined {
  const cookie = c.req.header("cookie") ?? "";
  for (const part of cookie.split(";")) {
    const [k, ...v] = part.trim().split("=");
    if (k === SHARED_COOKIE) return v.join("=");
  }
  return undefined;
}

function isAdminEmail(env: Bindings, email: string): boolean {
  return (env.ADMIN_EMAILS ?? "")
    .split(",")
    .map((s) => s.trim().toLowerCase())
    .filter(Boolean)
    .includes(email);
}

interface Identity {
  email: string;
  handle: string;
  emailVerified: boolean;
}

async function resolveIdentity(c: Context<AppEnv>): Promise<Identity | null> {
  const token = bearerFrom(c);
  if (!token) return null;
  const base = c.env.ID_ORIGIN.replace(/\/$/, "");
  const res = await fetch(`${base}/me`, { headers: { authorization: `Bearer ${token}` } });
  if (!res.ok) return null;
  const data = await res.json<{ user?: { email?: string; handle?: string; emailVerified?: boolean } }>().catch(() => null);
  const email = data?.user?.email?.toLowerCase();
  if (!email) return null;
  return { email, handle: data?.user?.handle ?? email.split("@")[0], emailVerified: data?.user?.emailVerified === true };
}

// Resolves the shared identity and upserts a local member row (trust 0 for a new
// identity; ADMIN_EMAILS are seeded as admin). Runs for every request; the member
// is null when there's no valid session.
export const loadMember: MiddlewareHandler<AppEnv> = async (c, next) => {
  const identity = await resolveIdentity(c);
  let member: Member | null = null;
  if (identity) {
    const now = new Date().toISOString();
    const seedRole = isAdminEmail(c.env, identity.email) ? "admin" : "member";
    await c.env.DB.prepare(
      `INSERT INTO members (email, handle, trust, role, created_at, last_seen_at)
       VALUES (?1, ?2, ?3, ?4, ?5, ?5)
       ON CONFLICT(email) DO UPDATE SET handle = ?2, last_seen_at = ?5,
         role = CASE WHEN ?4 = 'admin' THEN 'admin' ELSE members.role END`,
    )
      .bind(identity.email, identity.handle, seedRole === "admin" ? 2 : 0, seedRole, now)
      .run();
    const row = await c.env.DB.prepare(
      "SELECT email, handle, trust, role, silenced_until FROM members WHERE email = ?1",
    )
      .bind(identity.email)
      .first<{ email: string; handle: string; trust: number; role: Member["role"]; silenced_until: string | null }>();
    if (row) {
      member = {
        email: row.email,
        handle: row.handle,
        emailVerified: identity.emailVerified,
        trust: row.trust,
        role: row.role,
        silencedUntil: row.silenced_until,
      };
    }
  }
  c.set("member", member);
  await next();
};

export function currentMember(c: Context<AppEnv>): Member {
  const member = c.get("member");
  if (!member) throw new HttpError(401, "unauthorized", "Sign in to continue.");
  return member;
}

export class HttpError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
  ) {
    super(message);
  }
}
