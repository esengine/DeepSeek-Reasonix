import type { Context, MiddlewareHandler } from "hono";
import type { AppEnv } from "../env";
import type { AccountUser } from "../types";
import { toAccountUser } from "../types";
import { repos } from "../db";
import type { SessionRef } from "../db/sessions";
import { readSessionToken } from "../auth/cookies";
import { ApiError } from "./errors";
import { REMOTE_REAUTH_MS } from "../config";

// Non-browser clients (CLI/desktop) and cross-service callers carry the session
// in an Authorization header instead of the cookie.
export function readBearerToken(c: Context<AppEnv>): string | undefined {
  const header = c.req.header("authorization");
  if (!header) return undefined;
  const token = /^Bearer\s+(.+)$/i.exec(header.trim())?.[1]?.trim();
  return token || undefined;
}

// Resolves the session (cookie or Bearer token, if any) and stashes the user on
// the context. Runs for every request; never rejects.
export const loadUser: MiddlewareHandler<AppEnv> = async (c, next) => {
  const token = readSessionToken(c) ?? readBearerToken(c);
  let user: AccountUser | null = null;
  let session: SessionRef | null = null;
  if (token) {
    const resolved = await repos(c.env).sessions.resolveSession(token);
    if (resolved) {
      user = toAccountUser(resolved.user);
      session = resolved.session;
    }
  }
  c.set("user", user);
  c.set("session", session);
  await next();
};

// Gate for protected routes. Pairs with currentUser() in handlers.
export const requireAuth: MiddlewareHandler<AppEnv> = async (c, next) => {
  if (!c.get("user")) throw new ApiError(401, "unauthorized", "Sign in to continue.");
  await next();
};

export function currentUser(c: Context<AppEnv>): AccountUser {
  const user = c.get("user");
  if (!user) throw new ApiError(401, "unauthorized", "Sign in to continue.");
  return user;
}

// Remote-control identity is issued only to a browser sign-in made within the
// reauth window: a device-flow session says nothing about when a password was
// last entered. Returns the signed-in user and the session that proved it.
export function requireRemoteWebSession(c: Context<AppEnv>): { user: AccountUser; session: SessionRef; reauthAt: number } {
  const user = currentUser(c);
  const session = c.get("session");
  if (!session) throw new ApiError(401, "unauthorized", "Sign in to continue.");
  if (session.kind !== "web") {
    throw new ApiError(403, "remote_reauth_required", "Sign in on this browser to control a computer remotely.");
  }
  const reauthAt = Date.parse(session.createdAt) + REMOTE_REAUTH_MS;
  if (!(reauthAt > Date.now())) {
    throw new ApiError(403, "remote_reauth_required", "Sign in again to control this computer remotely.");
  }
  return { user, session, reauthAt };
}
