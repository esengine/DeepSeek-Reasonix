import { randomSuffix } from "../auth/crypto";

// Names that must never become a public handle (routes, infra, impersonation).
const RESERVED = new Set([
  "admin", "administrator", "root", "system", "support", "help", "about", "api",
  "auth", "login", "logout", "register", "signup", "signin", "reset", "verify",
  "forgot", "account", "settings", "me", "u", "user", "users", "profile",
  "reasonix", "www", "mail", "no-reply", "noreply", "null", "undefined", "anonymous",
]);

// Lowercase, collapse runs of disallowed characters to single underscores, and
// trim underscores from the ends.
export function normalizeHandle(raw: string): string {
  return raw
    .toLowerCase()
    .replace(/[^a-z0-9_]+/g, "_")
    .replace(/_+/g, "_")
    .replace(/^_+|_+$/g, "");
}

// 3–30 chars, [a-z0-9_], must start and end with an alphanumeric, not reserved.
export function isValidHandle(handle: string): boolean {
  if (handle.length < 3 || handle.length > 30) return false;
  if (!/^[a-z0-9](?:[a-z0-9_]*[a-z0-9])?$/.test(handle)) return false;
  return !RESERVED.has(handle);
}

// A normalized, valid starting handle derived from the email local-part. Falls
// back to a random "user…" handle when the local-part can't yield a valid one.
export function deriveHandleBase(email: string): string {
  let base = normalizeHandle(email.split("@")[0] ?? "");
  if (base.length > 24) base = base.slice(0, 24).replace(/_+$/g, "");
  if (base.length < 3 || RESERVED.has(base)) base = `user${randomSuffix(4)}`;
  return base;
}
