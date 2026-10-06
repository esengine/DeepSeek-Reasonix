import type { Env } from "./env";
import { feedbackEnabled } from "./feedback_auth";
import { accountHash, constantTimeEqual, sign } from "./feedback_crypto";
import { refuse } from "./feedback_http";

const AUDIENCE = "feedback";
const MAX_LIFETIME_SECONDS = 360;
const SUBJECT = /^[1-9][0-9]{0,14}$/;

export const ASSERTION_LABEL = "feedback-assertion";
export const ERASE_LABEL = "feedback-account-erase";
export const USER_ID = SUBJECT;

// The flag, the shared secret and the feedback secrets must all be present, so a
// half-configured deploy leaves the whole feature absent rather than partly live.
export function accountLinkEnabled(env: Env): boolean {
  return env.FEEDBACK_ACCOUNT_LINK === "true" && !!env.FEEDBACK_ACCOUNT_SECRET && feedbackEnabled(env);
}

function decodePayload(part: string): { aud?: unknown; exp?: unknown; sub?: unknown } | null {
  try {
    const bytes = Uint8Array.from(atob(part.replace(/-/g, "+").replace(/_/g, "/")), (c) => c.charCodeAt(0));
    const parsed: unknown = JSON.parse(new TextDecoder().decode(bytes));
    return typeof parsed === "object" && parsed !== null ? parsed : null;
  } catch {
    return null;
  }
}

// An assertion is `v1.<payload>.<signature>` minted by the accounts worker: audience
// "feedback", a canonical numeric subject and an expiry at most six minutes ahead (five minted, one of skew).
export async function verifyAssertion(env: Env, header: string, now: Date): Promise<{ accountHash: string } | Response> {
  const denied = () => refuse("feedback.account_required", "account sign-in required");
  const secret = env.FEEDBACK_ACCOUNT_SECRET;
  const parts = header.split(".");
  if (!secret || !env.FEEDBACK_TOKEN_SECRET || parts.length !== 3 || parts[0] !== "v1") return denied();
  if (!(await constantTimeEqual(parts[2], await sign(secret, ASSERTION_LABEL, parts[1])))) return denied();
  const payload = decodePayload(parts[1]);
  const seconds = Math.floor(now.getTime() / 1000);
  if (!payload || payload.aud !== AUDIENCE || typeof payload.sub !== "string" || !SUBJECT.test(payload.sub)) return denied();
  if (typeof payload.exp !== "number" || !(payload.exp > seconds) || payload.exp - seconds > MAX_LIFETIME_SECONDS) return denied();
  return { accountHash: await accountHash(env.FEEDBACK_TOKEN_SECRET, payload.sub) };
}
