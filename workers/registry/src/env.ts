import type { RegistryUser } from "./types";

// Cloudflare's native rate-limit binding (configured under [[unsafe.bindings]]).
export interface RateLimiter {
  limit(opts: { key: string }): Promise<{ success: boolean }>;
}

export interface Bindings {
  DB: D1Database;
  WRITE_LIMITER?: RateLimiter;

  // Plain vars (wrangler.toml [vars]).
  ACCOUNTS_ORIGIN: string;
  APP_ORIGIN: string;
  ALLOWED_ORIGINS: string;
}

// Set by requireAuth on protected routes; absent on public reads.
export interface Variables {
  user: RegistryUser;
}

export type AppEnv = { Bindings: Bindings; Variables: Variables };
