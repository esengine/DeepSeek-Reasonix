import type { AccountUser } from "./types";
import type { SessionRef } from "./db/sessions";

// Cloudflare's native rate-limit binding (configured under [[unsafe.bindings]]).
export interface RateLimiter {
  limit(opts: { key: string }): Promise<{ success: boolean }>;
}

export interface Bindings {
  DB: D1Database;
  AUTH_LIMITER?: RateLimiter;
  // Encrypted configuration backups. Absent until the bucket is bound; the
  // backup routes answer 503 rather than failing on a missing binding.
  BACKUPS?: R2Bucket;

  // Plain vars (wrangler.toml [vars]).
  APP_ORIGIN: string;
  ACCOUNT_ORIGIN: string;
  REMOTE_GATEWAY_ORIGIN: string;
  // Feedback worker origin for the account-deleted call; defaults to crash.reasonix.io.
  FEEDBACK_ORIGIN?: string;
  ALLOWED_ORIGINS: string;
  COOKIE_DOMAIN: string;
  EMAIL_PROVIDER: string;
  MAIL_FROM: string;
  ADMIN_EMAILS?: string;

  // Secrets (wrangler secret put ...).
  SESSION_PEPPER?: string;
  RESEND_API_KEY?: string;
  REMOTE_GATEWAY_TOKEN?: string;
  // Shared with the feedback worker: signs feedback assertions and erase calls.
  FEEDBACK_ACCOUNT_SECRET?: string;
}

// Per-request values set by middleware. `user` is null until a valid session is
// resolved; requireAuth guarantees it is non-null for protected routes.
export interface Variables {
  user: AccountUser | null;
  session: SessionRef | null;
}

export type AppEnv = { Bindings: Bindings; Variables: Variables };
