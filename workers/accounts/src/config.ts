// Tunable constants for the account service. Durations are in milliseconds.

export const SESSION_COOKIE = "rxid";

export const SESSION_TTL_MS = 30 * 24 * 60 * 60 * 1000; // 30 days
export const VERIFY_TTL_MS = 24 * 60 * 60 * 1000; // 24 hours
export const RESET_TTL_MS = 60 * 60 * 1000; // 1 hour

// Device-authorization flow (CLI/desktop sign-in).
export const DEVICE_CODE_TTL_MS = 10 * 60 * 1000; // 10 minutes to approve
export const DEVICE_POLL_INTERVAL_S = 5; // client poll cadence; faster polls get slow_down
export const REMOTE_GRANT_TTL_MS = 60 * 1000; // one-time gateway admission window
// Remote control needs a sign-in no older than this; a live connection ends when
// its signing-in session reaches the same age.
export const REMOTE_REAUTH_MS = 24 * 60 * 60 * 1000;
export const REMOTE_ATTACHMENT_TTL_MS = 15 * 60 * 1000;
export const REMOTE_ATTACHMENT_MAX_BYTES = 20 * 1024 * 1024;

// PBKDF2-HMAC-SHA256 work factor. Cloudflare Workers hard-caps PBKDF2 at 100k
// iterations (it throws NotSupportedError above that), so this is the platform
// ceiling. The value is embedded in each stored hash, so it can be raised later
// (e.g. if the cap is lifted) without breaking existing logins.
export const PBKDF2_ITERATIONS = 100_000;

export const MIN_PASSWORD = 8;
export const MAX_PASSWORD = 200;

// Encrypted configuration backups, per account.
export const CONFIG_BACKUP_MAX_BYTES = 4 * 1024 * 1024;
export const CONFIG_BACKUP_MAX_COUNT = 10;

// Controller enrollment bookkeeping.
export const REMOTE_CHALLENGE_TTL_MS = 60 * 1000;
export const REMOTE_PENDING_TTL_MS = 10 * 60 * 1000;
export const REMOTE_CONTROLLER_ACTIVE_CAP = 8;
export const REMOTE_CONTROLLER_PENDING_CAP = 3;
// An active controller unseen for this long stops counting toward the cap; it
// stays active and keeps its enrollment.
export const REMOTE_CONTROLLER_IDLE_MS = 30 * 24 * 60 * 60 * 1000;
export const REMOTE_PENDING_REJECT_LIMIT = 3;
export const REMOTE_PENDING_LOCK_MS = 60 * 60 * 1000;

// Per-route limits for controller enrollment, counted in D1 (see
// http/d1RateLimit.ts): the per-IP binding cannot express hour windows and is
// skipped when absent.
export const REMOTE_RATE_RULES = {
  challenge: { name: "challenge", limit: 30, windowMs: 60 * 1000 },
  enroll: { name: "enroll", limit: 5, windowMs: 60 * 60 * 1000 },
  revoke: { name: "revoke", limit: 30, windowMs: 60 * 1000 },
  hostControllers: { name: "host-controllers", limit: 60, windowMs: 60 * 1000 },
  pendingRejects: { name: "pending-rejects", limit: REMOTE_PENDING_REJECT_LIMIT, windowMs: REMOTE_PENDING_LOCK_MS },
} as const;
