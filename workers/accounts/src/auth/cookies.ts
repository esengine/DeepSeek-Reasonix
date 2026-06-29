import type { Context } from "hono";
import { getCookie, setCookie, deleteCookie } from "hono/cookie";
import type { AppEnv } from "../env";
import { SESSION_COOKIE, SESSION_TTL_MS } from "../config";

// Secure cookies are dropped over plain http, so honour the request scheme:
// https in production, http under `wrangler dev` on localhost.
function isSecure(c: Context<AppEnv>): boolean {
  return new URL(c.req.url).protocol === "https:";
}

export function readSessionToken(c: Context<AppEnv>): string | undefined {
  return getCookie(c, SESSION_COOKIE);
}

export function setSessionCookie(c: Context<AppEnv>, token: string): void {
  const domain = c.env.COOKIE_DOMAIN?.trim();
  setCookie(c, SESSION_COOKIE, token, {
    httpOnly: true,
    secure: isSecure(c),
    sameSite: "Lax",
    path: "/",
    maxAge: Math.floor(SESSION_TTL_MS / 1000),
    ...(domain ? { domain } : {}),
  });
}

export function clearSessionCookie(c: Context<AppEnv>): void {
  const domain = c.env.COOKIE_DOMAIN?.trim();
  deleteCookie(c, SESSION_COOKIE, {
    path: "/",
    secure: isSecure(c),
    ...(domain ? { domain } : {}),
  });
}
