export interface RateLimiter {
  limit(opts: { key: string }): Promise<{ success: boolean }>;
}

export interface Env {
  DB: D1Database;
  // Skill/MCP registry database — the folded registry API + moderation console
  // read and write it; the crash tables stay in DB.
  REGISTRY_DB: D1Database;
  RATE_LIMITER: RateLimiter;
  PING_LIMITER: RateLimiter;
  METRICS_LIMITER: RateLimiter;
  WRITE_LIMITER?: RateLimiter;
  ADMIN_EMAILS?: string;
  // Shared identity service (id.reasonix.io) and the site that hosts its login
  // page (reasonix.io). Overridable for local dev.
  ID_ORIGIN?: string;
  APP_ORIGIN?: string;
  // Browsers allowed to call the registry API with credentials (comma-separated).
  ALLOWED_ORIGINS?: string;
}
