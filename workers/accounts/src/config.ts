// Tunable constants for the account service. Durations are in milliseconds.

export const SESSION_COOKIE = "rxid";

export const SESSION_TTL_MS = 30 * 24 * 60 * 60 * 1000; // 30 days
export const VERIFY_TTL_MS = 24 * 60 * 60 * 1000; // 24 hours
export const RESET_TTL_MS = 60 * 60 * 1000; // 1 hour

// Device-authorization flow (CLI/desktop sign-in).
export const DEVICE_CODE_TTL_MS = 10 * 60 * 1000; // 10 minutes to approve
export const DEVICE_POLL_INTERVAL_S = 5; // client poll cadence; faster polls get slow_down

// PBKDF2-HMAC-SHA256 work factor. Cloudflare Workers hard-caps PBKDF2 at 100k
// iterations (it throws NotSupportedError above that), so this is the platform
// ceiling. The value is embedded in each stored hash, so it can be raised later
// (e.g. if the cap is lifted) without breaking existing logins.
export const PBKDF2_ITERATIONS = 100_000;

export const MIN_PASSWORD = 8;
export const MAX_PASSWORD = 200;
