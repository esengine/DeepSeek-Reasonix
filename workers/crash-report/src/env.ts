export interface RateLimiter {
  limit(opts: { key: string }): Promise<{ success: boolean }>;
}

export interface Env {
  DB: D1Database;
  RATE_LIMITER: RateLimiter;
  PING_LIMITER: RateLimiter;
  METRICS_LIMITER: RateLimiter;
  ADMIN_EMAILS?: string;
  // Shared identity service (id.reasonix.io) and the site that hosts its login
  // page (reasonix.io). Overridable for local dev.
  ID_ORIGIN?: string;
  APP_ORIGIN?: string;
}
