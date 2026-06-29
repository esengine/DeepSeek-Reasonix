import type { AccountUser } from "./types";

// Cloudflare's native rate-limit binding (configured under [[unsafe.bindings]]).
export interface RateLimiter {
  limit(opts: { key: string }): Promise<{ success: boolean }>;
}

export interface Bindings {
  DB: D1Database;
  AUTH_LIMITER?: RateLimiter;

  // Plain vars (wrangler.toml [vars]).
  APP_ORIGIN: string;
  ALLOWED_ORIGINS: string;
  COOKIE_DOMAIN: string;
  EMAIL_PROVIDER: string;
  MAIL_FROM: string;
  ADMIN_EMAILS?: string;

  // Secrets (wrangler secret put ...).
  SESSION_PEPPER?: string;
  RESEND_API_KEY?: string;
}

// Per-request values set by middleware. `user` is null until a valid session is
// resolved; requireAuth guarantees it is non-null for protected routes.
export interface Variables {
  user: AccountUser | null;
}

export type AppEnv = { Bindings: Bindings; Variables: Variables };
