export interface RateLimiter {
  limit(opts: { key: string }): Promise<{ success: boolean }>;
}

export interface Env {
  DB: D1Database;
  RATE_LIMITER: RateLimiter;
  PING_LIMITER: RateLimiter;
  METRICS_LIMITER: RateLimiter;
  ADMIN_EMAILS?: string;
}
