export interface RateLimiter {
  limit(opts: { key: string }): Promise<{ success: boolean }>;
}

export interface Env {
  DB: D1Database;
  // CLI and Studio telemetry are isolated from crash diagnostics. Optional
  // only for local tests and rolling back to a pre-split deployment.
  TELEMETRY_DB?: D1Database;
  TELEMETRY_DB_MODE?: string;
  DESKTOP_TELEMETRY_DB_MODE?: string;
  // Skill/MCP registry database — the folded registry API + moderation console
  // read and write it; the crash tables stay in DB.
  REGISTRY_DB: D1Database;
  RATE_LIMITER: RateLimiter;
  PING_LIMITER: RateLimiter;
  METRICS_LIMITER: RateLimiter;
  TELEMETRY_BUDGET_LIMITER?: RateLimiter;
  TELEMETRY_QUEUE?: Queue<import("./telemetry_queue").TelemetryEnvelope>;
  TELEMETRY_RAW?: R2Bucket;
  TELEMETRY_QUEUE_ENABLED?: string;
  WRITE_LIMITER?: RateLimiter;
  // In-app feedback loop. Endpoints answer feedback.disabled unless both secrets
  // are set and FEEDBACK_ENABLED is not "false". Screenshots live in TELEMETRY_RAW
  // under the feedback/ prefix.
  FEEDBACK_LIMITER?: RateLimiter;
  FEEDBACK_BUDGET_LIMITER?: RateLimiter;
  FEEDBACK_ENABLED?: string;
  FEEDBACK_TOKEN_SECRET?: string;
  FEEDBACK_ADMIN_TOKEN?: string;
  // Account link kill switch: only "true" enables the link routes, the union read
  // and sibling replies. The shared secret verifies account assertions and erasure calls.
  FEEDBACK_ACCOUNT_LINK?: string;
  FEEDBACK_ACCOUNT_SECRET?: string;
  // Ops-events relay; feedback emits compact events only when both are set.
  OPS_EVENTS_URL?: string;
  OPS_EMIT_TOKEN?: string;
  // When set, submissions must carry a valid Turnstile token (feedback.challenge_required).
  FEEDBACK_TURNSTILE_SECRET?: string;
  // Comma-separated hostnames a Turnstile token may come from; unset skips the hostname check.
  FEEDBACK_TURNSTILE_HOSTNAMES?: string;
  ADMIN_EMAILS?: string;
  // Shared identity service (id.reasonix.io) and the site that hosts its login
  // page (reasonix.io). Overridable for local dev.
  ID_ORIGIN?: string;
  APP_ORIGIN?: string;
  // Browsers allowed to call the registry API with credentials (comma-separated).
  ALLOWED_ORIGINS?: string;
  // Optional incident webhook for the ingest sentinel, mirrored from the
  // GitHub repo secret of the same name by deploy-crash-worker.yml. Feishu/
  // Lark bot URLs get their native payload shape; any other receiver gets
  // Slack-style {"text": ...}. Unset = log-only.
  ALERT_WEBHOOK?: string;
  // Crash sample storage rollout. `d1` is the fail-safe default; `dual`
  // preserves D1 samples while mirroring Firebase; `firebase` keeps only the
  // D1 query projection and the bounded retry outbox.
  CRASH_STORAGE_MODE?: string;
  FIREBASE_DATABASE_URL?: string;
  FIREBASE_CLIENT_EMAIL?: string;
  FIREBASE_PRIVATE_KEY?: string;
}
